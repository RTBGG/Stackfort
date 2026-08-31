// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/store"
	"golang.org/x/crypto/argon2"
)

const (
	bootstrapTokenPrefix   = "sfb_"
	bootstrapTokenBytes    = 32
	bootstrapDefaultTTL    = 15 * time.Minute
	bootstrapMinimumTTL    = time.Minute
	bootstrapMaximumTTL    = time.Hour
	bootstrapMinimumRunes  = 15
	bootstrapMaximumRunes  = 128
	bootstrapMaximumBytes  = 1024
	bootstrapSaltBytes     = 16
	bootstrapArgonMemory   = 64 * 1024
	bootstrapArgonTime     = 3
	bootstrapArgonThreads  = 4
	bootstrapArgonKeyBytes = 32

	bootstrapGlobalWindow = time.Minute
	bootstrapGlobalLimit  = 30
	bootstrapGlobalBlock  = time.Minute
	bootstrapSourceWindow = time.Minute
	bootstrapSourceLimit  = 5
	bootstrapSourceBlock  = 5 * time.Minute
)

type passwordDeriver func(password, salt []byte, iterations, memory uint32, parallelism uint8, keyLength uint32) []byte

type bootstrapAuthorization struct {
	capabilityID ID
	tokenHash    [sha256.Size]byte
}

type rateLimitPolicy struct {
	scope  string
	key    string
	window time.Duration
	limit  int64
	block  time.Duration
}

func deriveArgon2id(password, salt []byte, iterations, memory uint32, parallelism uint8, keyLength uint32) []byte {
	return argon2.IDKey(password, salt, iterations, memory, parallelism, keyLength)
}

// CreateBootstrapCapability creates a short-lived capability whose raw token
// exists only in this return value. SQLite receives only its SHA-256 digest.
func (r *Repository) CreateBootstrapCapability(ctx context.Context, params CreateBootstrapCapabilityParams) (BootstrapCapability, error) {
	ttl := params.TTL
	if ttl == 0 {
		ttl = bootstrapDefaultTTL
	}
	if ttl < bootstrapMinimumTTL || ttl > bootstrapMaximumTTL {
		return BootstrapCapability{}, fmt.Errorf("%w: bootstrap TTL must be between %s and %s", ErrInvalidInput, bootstrapMinimumTTL, bootstrapMaximumTTL)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return BootstrapCapability{}, err
	}

	raw := make([]byte, bootstrapTokenBytes)
	if _, err := io.ReadFull(r.random, raw); err != nil {
		return BootstrapCapability{}, fmt.Errorf("generate bootstrap capability: %w", err)
	}
	token := bootstrapTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	id, err := r.newID()
	if err != nil {
		return BootstrapCapability{}, err
	}
	now := r.timestamp()
	capability := BootstrapCapability{ID: id, Token: token, CreatedAt: now, ExpiresAt: now.Add(ttl)}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		administratorExists, err := platformAdministratorExistsTx(ctx, executor)
		if err != nil {
			return err
		}
		if administratorExists {
			return ErrBootstrapDisabled
		}

		var activeID, activeExpiresAt string
		replacedActive := false
		err = executor.QueryRowContext(ctx, `
			SELECT id, expires_at FROM bootstrap_capabilities
			WHERE consumed_at IS NULL AND invalidated_at IS NULL`).Scan(&activeID, &activeExpiresAt)
		switch {
		case err == nil:
			expiry, err := parseTime(activeExpiresAt)
			if err != nil {
				return err
			}
			reason := "expired"
			if expiry.After(now) {
				if !params.Replace {
					return ErrConflict
				}
				reason = "replaced"
				replacedActive = true
			}
			if _, err := executor.ExecContext(ctx, `
				UPDATE bootstrap_capabilities
				SET invalidated_at = ?, invalidation_reason = ?
				WHERE id = ?`, formatTime(now), reason, activeID); err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		}

		if _, err := executor.ExecContext(ctx, `
			INSERT INTO bootstrap_capabilities (
				id, active_slot, token_hash, created_at, expires_at
			) VALUES (?, 1, ?, ?, ?)`,
			string(capability.ID), tokenHash[:], formatTime(capability.CreatedAt), formatTime(capability.ExpiresAt)); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			Action:     "bootstrap.capability_created",
			TargetType: "bootstrap_capability",
			TargetID:   string(capability.ID),
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"expiresAt": formatTime(capability.ExpiresAt),
				"replaced":  replacedActive,
			},
		}, now)
	})
	if err != nil {
		return BootstrapCapability{}, classifyDatabaseError(err)
	}
	return capability, nil
}

// AdministratorBootstrapStatus exposes no capability material.
func (r *Repository) AdministratorBootstrapStatus(ctx context.Context) (BootstrapStatus, error) {
	now := r.timestamp()
	var status BootstrapStatus
	err := r.state.Read(ctx, func(reader store.Reader) error {
		administratorExists, err := platformAdministratorExistsTx(ctx, reader)
		if err != nil {
			return err
		}
		status.Required = !administratorExists
		if administratorExists {
			return nil
		}

		var expiresAt string
		err = reader.QueryRowContext(ctx, `
			SELECT expires_at FROM bootstrap_capabilities
			WHERE consumed_at IS NULL AND invalidated_at IS NULL`).Scan(&expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		expiry, err := parseTime(expiresAt)
		if err != nil {
			return err
		}
		if expiry.After(now) {
			status.CapabilityActive = true
			status.ExpiresAt = &expiry
		}
		return nil
	})
	return status, err
}

// BootstrapAdministrator validates and consumes a capability while creating
// the first administrator, credential, role, and audit event atomically.
func (r *Repository) BootstrapAdministrator(ctx context.Context, params BootstrapAdministratorParams) (Identity, error) {
	email, normalizedEmail, err := normalizeEmail(params.Email)
	if err != nil {
		return Identity{}, err
	}
	displayName, err := validateText(params.DisplayName, "displayName", 1, 120)
	if err != nil {
		return Identity{}, err
	}
	if params.Locale != LocaleEnglish && params.Locale != LocaleGerman {
		return Identity{}, fmt.Errorf("%w: locale must be en or de", ErrInvalidInput)
	}
	if err := validateBootstrapPassword(params.Password); err != nil {
		return Identity{}, err
	}
	if len(params.Token) == 0 || len(params.Token) > 128 {
		return Identity{}, ErrBootstrapDenied
	}
	sourceAddress, err := normalizeSourceAddress(params.SourceAddress)
	if err != nil {
		return Identity{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Identity{}, err
	}

	authorization, err := r.authorizeBootstrapAttempt(ctx, params.Token, sourceAddress)
	if err != nil {
		return Identity{}, err
	}
	select {
	case r.passwordDerivationSlots <- struct{}{}:
		defer func() { <-r.passwordDerivationSlots }()
	case <-ctx.Done():
		return Identity{}, ctx.Err()
	}
	if err := r.validateBootstrapAuthorization(ctx, authorization); err != nil {
		return Identity{}, err
	}
	salt := make([]byte, bootstrapSaltBytes)
	if _, err := io.ReadFull(r.random, salt); err != nil {
		return Identity{}, fmt.Errorf("generate password salt: %w", err)
	}
	passwordHash := r.derivePassword(
		[]byte(params.Password), salt, bootstrapArgonTime, bootstrapArgonMemory,
		bootstrapArgonThreads, bootstrapArgonKeyBytes,
	)
	if len(passwordHash) != bootstrapArgonKeyBytes {
		return Identity{}, errors.New("password derivation returned an invalid key length")
	}

	id, err := r.newID()
	if err != nil {
		return Identity{}, err
	}
	now := r.timestamp()
	identity := Identity{
		ID:              id,
		Email:           email,
		NormalizedEmail: normalizedEmail,
		DisplayName:     displayName,
		Locale:          params.Locale,
		Status:          IdentityActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		administratorExists, err := platformAdministratorExistsTx(ctx, executor)
		if err != nil {
			return err
		}
		if administratorExists {
			return ErrBootstrapDisabled
		}

		var storedHash []byte
		var expiresAt string
		err = executor.QueryRowContext(ctx, `
			SELECT token_hash, expires_at
			FROM bootstrap_capabilities
			WHERE id = ? AND consumed_at IS NULL AND invalidated_at IS NULL`,
			string(authorization.capabilityID)).Scan(&storedHash, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBootstrapDenied
		}
		if err != nil {
			return err
		}
		expiry, err := parseTime(expiresAt)
		if err != nil {
			return err
		}
		if !expiry.After(now) || len(storedHash) != sha256.Size || subtle.ConstantTimeCompare(storedHash, authorization.tokenHash[:]) != 1 {
			return ErrBootstrapDenied
		}

		if _, err := executor.ExecContext(ctx, `
			INSERT INTO identities (
				id, email, normalized_email, display_name, locale, status, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`,
			string(identity.ID), identity.Email, identity.NormalizedEmail, identity.DisplayName,
			string(identity.Locale), formatTime(now), formatTime(now)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO password_credentials (
				identity_id, algorithm, password_hash, salt, memory_kib, iterations,
				parallelism, version, must_rotate, created_at, updated_at
			) VALUES (?, 'argon2id', ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			string(identity.ID), passwordHash, salt, bootstrapArgonMemory, bootstrapArgonTime,
			bootstrapArgonThreads, argon2.Version, formatTime(now), formatTime(now)); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO platform_role_assignments (
				identity_id, role, granted_at, granted_by_identity_id
			) VALUES (?, 'platform_admin', ?, ?)`,
			string(identity.ID), formatTime(now), string(identity.ID)); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE bootstrap_capabilities
			SET consumed_at = ?, consumed_by_identity_id = ?
			WHERE id = ? AND consumed_at IS NULL AND invalidated_at IS NULL`,
			formatTime(now), string(identity.ID), string(authorization.capabilityID))
		if err != nil {
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rowsAffected != 1 {
			return ErrBootstrapDenied
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:       &identity.ID,
			SourceAddress: sourceAddress,
			Action:        "bootstrap.administrator_created",
			TargetType:    "identity",
			TargetID:      string(identity.ID),
			RequestID:     requestID,
			Result:        AuditSuccess,
			Details: map[string]any{
				"locale": identity.Locale,
			},
		}, now)
	})
	if err != nil {
		return Identity{}, classifyDatabaseError(err)
	}
	return identity, nil
}

func (r *Repository) authorizeBootstrapAttempt(ctx context.Context, token, sourceAddress string) (bootstrapAuthorization, error) {
	now := r.timestamp()
	suppliedHash := sha256.Sum256([]byte(token))
	authorization := bootstrapAuthorization{tokenHash: suppliedHash}
	denied := false
	var retryAfter time.Duration

	err := r.state.Write(ctx, func(executor store.Executor) error {
		administratorExists, err := platformAdministratorExistsTx(ctx, executor)
		if err != nil {
			return err
		}
		if administratorExists {
			return ErrBootstrapDisabled
		}

		policies := []rateLimitPolicy{{
			scope: "global", key: "*", window: bootstrapGlobalWindow,
			limit: bootstrapGlobalLimit, block: bootstrapGlobalBlock,
		}, {
			scope: "source", key: sourceAddress, window: bootstrapSourceWindow,
			limit: bootstrapSourceLimit, block: bootstrapSourceBlock,
		}}
		for _, policy := range policies {
			limitedFor, err := applyBootstrapRateLimitTx(ctx, executor, policy, now)
			if err != nil {
				return err
			}
			if limitedFor > 0 {
				retryAfter = limitedFor
				return nil
			}
		}
		if _, err := executor.ExecContext(ctx, `
			DELETE FROM bootstrap_rate_limits
			WHERE scope = 'source' AND updated_at < ?`, formatTime(now.Add(-24*time.Hour))); err != nil {
			return err
		}

		var capabilityID string
		var storedHash []byte
		var expiresAt string
		err = executor.QueryRowContext(ctx, `
			SELECT id, token_hash, expires_at
			FROM bootstrap_capabilities
			WHERE consumed_at IS NULL AND invalidated_at IS NULL`).Scan(&capabilityID, &storedHash, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			storedHash = make([]byte, sha256.Size)
			denied = true
		} else if err != nil {
			return err
		}

		if !denied {
			expiry, err := parseTime(expiresAt)
			if err != nil {
				return err
			}
			if !expiry.After(now) {
				if _, err := executor.ExecContext(ctx, `
					UPDATE bootstrap_capabilities
					SET invalidated_at = ?, invalidation_reason = 'expired'
					WHERE id = ?`, formatTime(now), capabilityID); err != nil {
					return err
				}
				storedHash = make([]byte, sha256.Size)
				denied = true
			}
		}
		if len(storedHash) != sha256.Size || subtle.ConstantTimeCompare(storedHash, suppliedHash[:]) != 1 {
			denied = true
		}
		if !denied {
			parsedID, err := ParseID(capabilityID)
			if err != nil {
				return err
			}
			authorization.capabilityID = parsedID
		}
		return nil
	})
	if err != nil {
		return bootstrapAuthorization{}, classifyDatabaseError(err)
	}
	if retryAfter > 0 {
		return bootstrapAuthorization{}, &BootstrapRateLimitError{RetryAfter: retryAfter}
	}
	if denied {
		return bootstrapAuthorization{}, ErrBootstrapDenied
	}
	return authorization, nil
}

func (r *Repository) validateBootstrapAuthorization(ctx context.Context, authorization bootstrapAuthorization) error {
	now := r.timestamp()
	err := r.state.Read(ctx, func(reader store.Reader) error {
		administratorExists, err := platformAdministratorExistsTx(ctx, reader)
		if err != nil {
			return err
		}
		if administratorExists {
			return ErrBootstrapDisabled
		}
		var storedHash []byte
		var expiresAt string
		err = reader.QueryRowContext(ctx, `
			SELECT token_hash, expires_at
			FROM bootstrap_capabilities
			WHERE id = ? AND consumed_at IS NULL AND invalidated_at IS NULL`,
			string(authorization.capabilityID)).Scan(&storedHash, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBootstrapDenied
		}
		if err != nil {
			return err
		}
		expiry, err := parseTime(expiresAt)
		if err != nil {
			return err
		}
		if !expiry.After(now) || len(storedHash) != sha256.Size || subtle.ConstantTimeCompare(storedHash, authorization.tokenHash[:]) != 1 {
			return ErrBootstrapDenied
		}
		return nil
	})
	return classifyDatabaseError(err)
}

func applyBootstrapRateLimitTx(ctx context.Context, executor store.Executor, policy rateLimitPolicy, now time.Time) (time.Duration, error) {
	var windowStartedAt, blockedUntil sql.NullString
	var attemptCount int64
	err := executor.QueryRowContext(ctx, `
		SELECT window_started_at, attempt_count, blocked_until
		FROM bootstrap_rate_limits
		WHERE scope = ? AND rate_key = ?`, policy.scope, policy.key).Scan(&windowStartedAt, &attemptCount, &blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = executor.ExecContext(ctx, `
			INSERT INTO bootstrap_rate_limits (
				scope, rate_key, window_started_at, attempt_count, updated_at
			) VALUES (?, ?, ?, 1, ?)`, policy.scope, policy.key, formatTime(now), formatTime(now))
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	if blockedUntil.Valid {
		blocked, err := parseTime(blockedUntil.String)
		if err != nil {
			return 0, err
		}
		if blocked.After(now) {
			return blocked.Sub(now), nil
		}
	}
	windowStart, err := parseTime(windowStartedAt.String)
	if err != nil {
		return 0, err
	}
	if !now.Before(windowStart.Add(policy.window)) {
		_, err = executor.ExecContext(ctx, `
			UPDATE bootstrap_rate_limits
			SET window_started_at = ?, attempt_count = 1, blocked_until = NULL, updated_at = ?
			WHERE scope = ? AND rate_key = ?`,
			formatTime(now), formatTime(now), policy.scope, policy.key)
		return 0, err
	}

	attemptCount++
	if attemptCount > policy.limit {
		blockUntil := now.Add(policy.block)
		_, err = executor.ExecContext(ctx, `
			UPDATE bootstrap_rate_limits
			SET attempt_count = ?, blocked_until = ?, updated_at = ?
			WHERE scope = ? AND rate_key = ?`,
			attemptCount, formatTime(blockUntil), formatTime(now), policy.scope, policy.key)
		return policy.block, err
	}
	_, err = executor.ExecContext(ctx, `
		UPDATE bootstrap_rate_limits
		SET attempt_count = ?, blocked_until = NULL, updated_at = ?
		WHERE scope = ? AND rate_key = ?`,
		attemptCount, formatTime(now), policy.scope, policy.key)
	return 0, err
}

func platformAdministratorExistsTx(ctx context.Context, reader store.Reader) (bool, error) {
	var exists bool
	err := reader.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM platform_role_assignments WHERE role = 'platform_admin'
		)`).Scan(&exists)
	return exists, err
}

func normalizeSourceAddress(value string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("%w: sourceAddress must be an IP address", ErrInvalidInput)
	}
	return address.Unmap().String(), nil
}

func validateBootstrapPassword(password string) error {
	if !utf8.ValidString(password) || len(password) > bootstrapMaximumBytes {
		return fmt.Errorf("%w: password must be valid UTF-8 and no more than %d bytes", ErrInvalidInput, bootstrapMaximumBytes)
	}
	length := utf8.RuneCountInString(password)
	if length < bootstrapMinimumRunes || length > bootstrapMaximumRunes {
		return fmt.Errorf("%w: password length must be between %d and %d characters", ErrInvalidInput, bootstrapMinimumRunes, bootstrapMaximumRunes)
	}
	return nil
}
