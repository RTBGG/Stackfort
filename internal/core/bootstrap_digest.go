// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

// BootstrapCapabilityDigest is SHA-256 of the complete displayed token string
// ("sfb_" plus unpadded base64url encoding of 32 random bytes), not of its decoded
// randomness. Only a trusted local producer may register it. A digest cannot
// prove the entropy of its preimage; this is never an unauthenticated HTTP API.
type BootstrapCapabilityDigest [sha256.Size]byte

type RegisterBootstrapCapabilityDigestParams struct {
	Digest    BootstrapCapabilityDigest
	TTL       time.Duration
	RequestID string
}

// RegisteredBootstrapCapability contains no bearer token or digest. Exact
// still-active retries report the original lifetime and never extend it.
type RegisteredBootstrapCapability struct {
	ID                ID        `json:"id"`
	CreatedAt         time.Time `json:"createdAt"`
	ExpiresAt         time.Time `json:"expiresAt"`
	AlreadyRegistered bool      `json:"alreadyRegistered"`
}

func ParseBootstrapCapabilityDigest(value string) (BootstrapCapabilityDigest, error) {
	var digest BootstrapCapabilityDigest
	if len(value) != sha256.Size*2 {
		return digest, fmt.Errorf("%w: bootstrap digest must be canonical lowercase SHA-256", ErrInvalidInput)
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return digest, fmt.Errorf("%w: bootstrap digest must be canonical lowercase SHA-256", ErrInvalidInput)
		}
	}
	if _, err := hex.Decode(digest[:], []byte(value)); err != nil || digest == (BootstrapCapabilityDigest{}) {
		return BootstrapCapabilityDigest{}, fmt.Errorf("%w: bootstrap digest is invalid", ErrInvalidInput)
	}
	return digest, nil
}

// RegisterBootstrapCapabilityDigest is a local private-database operation for a
// capability generated/displayed before reboot. Its lifetime starts only when
// registration succeeds. There is no replacement, reactivation or TTL-renewal
// option. Installer operation/host/release binding is enforced by its own sealed
// receipt; this method deliberately does not add another operation journal.
func (r *Repository) RegisterBootstrapCapabilityDigest(ctx context.Context, params RegisterBootstrapCapabilityDigestParams) (RegisteredBootstrapCapability, error) {
	ttl, requestID, err := validateBootstrapRegistration(params.TTL, params.RequestID)
	if err != nil {
		return RegisteredBootstrapCapability{}, err
	}
	return r.registerBootstrapDigest(ctx, params.Digest, ttl, false, true, requestID)
}

func validateBootstrapRegistration(ttl time.Duration, requestID string) (time.Duration, string, error) {
	if ttl == 0 {
		ttl = bootstrapDefaultTTL
	}
	if ttl < bootstrapMinimumTTL || ttl > bootstrapMaximumTTL {
		return 0, "", fmt.Errorf("%w: bootstrap TTL must be between %s and %s", ErrInvalidInput, bootstrapMinimumTTL, bootstrapMaximumTTL)
	}
	id, err := validateOptionalText(requestID, "requestId", 128)
	return ttl, id, err
}

// Shared serialized creation transaction. The ordinary create command retains
// its explicit replacement option; digest import has only exact-active replay.
func (r *Repository) registerBootstrapDigest(ctx context.Context, digest BootstrapCapabilityDigest, ttl time.Duration, replace, replayActive bool, requestID string) (RegisteredBootstrapCapability, error) {
	if ctx == nil || digest == (BootstrapCapabilityDigest{}) || (replace && replayActive) {
		return RegisteredBootstrapCapability{}, fmt.Errorf("%w: invalid local bootstrap registration", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return RegisteredBootstrapCapability{}, err
	}
	var result RegisteredBootstrapCapability
	err := r.state.Write(ctx, func(executor store.Executor) error {
		administratorExists, err := platformAdministratorExistsTx(ctx, executor)
		if err != nil {
			return err
		}
		if administratorExists {
			return ErrBootstrapDisabled
		}
		// Time is sampled inside the serialized transaction, not when a caller
		// starts waiting for the database or originally generated the raw token.
		now := r.timestamp()
		var activeID, activeCreatedAt, activeExpiresAt string
		var activeDigest []byte
		replacedActive := false
		err = executor.QueryRowContext(ctx, `
			SELECT id, token_hash, created_at, expires_at FROM bootstrap_capabilities
			WHERE consumed_at IS NULL AND invalidated_at IS NULL`).Scan(&activeID, &activeDigest, &activeCreatedAt, &activeExpiresAt)
		switch {
		case err == nil:
			expiry, err := parseTime(activeExpiresAt)
			if err != nil {
				return err
			}
			sameDigest := len(activeDigest) == sha256.Size && subtle.ConstantTimeCompare(activeDigest, digest[:]) == 1
			if replayActive && sameDigest {
				if !expiry.After(now) {
					return ErrConflict // Never reactivate or extend an expired token.
				}
				id, err := ParseID(activeID)
				if err != nil {
					return err
				}
				created, err := parseTime(activeCreatedAt)
				if err != nil {
					return err
				}
				result = RegisteredBootstrapCapability{ID: id, CreatedAt: created, ExpiresAt: expiry, AlreadyRegistered: true}
				return nil // No new capability, audit event or expiry write.
			}
			reason := "expired"
			if expiry.After(now) {
				if !replace {
					return ErrConflict
				}
				reason, replacedActive = "replaced", true
			}
			if _, err := executor.ExecContext(ctx, `
				UPDATE bootstrap_capabilities SET invalidated_at = ?, invalidation_reason = ?
				WHERE id = ?`, formatTime(now), reason, activeID); err != nil {
				return err
			}
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		}
		id, err := r.newID()
		if err != nil {
			return err
		}
		result = RegisteredBootstrapCapability{ID: id, CreatedAt: now, ExpiresAt: now.Add(ttl)}
		// The existing global unique digest and immutable terminal history also
		// reject replay of a consumed, invalidated or previously expired digest.
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO bootstrap_capabilities (id, active_slot, token_hash, created_at, expires_at)
			VALUES (?, 1, ?, ?, ?)`, string(result.ID), digest[:], formatTime(result.CreatedAt), formatTime(result.ExpiresAt)); err != nil {
			return err
		}
		details := map[string]any{"expiresAt": formatTime(result.ExpiresAt), "replaced": replacedActive}
		if replayActive {
			details["delivery"] = "local-digest-import"
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			Action: "bootstrap.capability_created", TargetType: "bootstrap_capability", TargetID: string(result.ID),
			RequestID: requestID, Result: AuditSuccess, Details: details,
		}, now)
	})
	if err != nil {
		return RegisteredBootstrapCapability{}, classifyDatabaseError(err)
	}
	return result, nil
}
