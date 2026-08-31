// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/RTBGG/stackfort/internal/store"
)

func (r *Repository) CreateIdentity(ctx context.Context, params CreateIdentityParams) (Identity, error) {
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
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Identity{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Identity{}, err
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
		_, err := executor.ExecContext(ctx, `
			INSERT INTO identities (
				id, email, normalized_email, display_name, locale, status, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			string(identity.ID),
			identity.Email,
			identity.NormalizedEmail,
			identity.DisplayName,
			string(identity.Locale),
			string(identity.Status),
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "identity.created",
			TargetType: "identity",
			TargetID:   string(identity.ID),
			RequestID:  requestID,
			Result:     AuditSuccess,
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

func (r *Repository) GetIdentity(ctx context.Context, identityID ID) (Identity, error) {
	if err := validateID(identityID, "identityId"); err != nil {
		return Identity{}, err
	}

	var identity Identity
	var locale, status, createdAt, updatedAt string
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT id, email, normalized_email, display_name, locale, status, created_at, updated_at
			FROM identities
			WHERE id = ?`, string(identityID)).Scan(
			&identity.ID,
			&identity.Email,
			&identity.NormalizedEmail,
			&identity.DisplayName,
			&locale,
			&status,
			&createdAt,
			&updatedAt,
		)
	})
	if err != nil {
		return Identity{}, classifyDatabaseError(err)
	}
	identity.Locale = Locale(locale)
	identity.Status = IdentityStatus(status)
	identity.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Identity{}, err
	}
	identity.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (r *Repository) SetPasswordCredential(ctx context.Context, params SetPasswordCredentialParams) error {
	if err := validateID(params.IdentityID, "identityId"); err != nil {
		return err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return err
	}
	if len(params.Hash) < 16 || len(params.Hash) > 128 || len(params.Salt) < 16 || len(params.Salt) > 64 {
		return fmt.Errorf("%w: password hash or salt has an unsupported length", ErrInvalidInput)
	}
	if params.MemoryKiB < 8192 || params.MemoryKiB > 4_194_304 || params.Iterations < 1 || params.Iterations > 100 || params.Parallelism < 1 || params.Parallelism > 64 || params.Version < 1 {
		return fmt.Errorf("%w: Argon2id parameters are outside supported bounds", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	now := r.timestamp()

	err = r.state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			INSERT INTO password_credentials (
				identity_id, algorithm, password_hash, salt, memory_kib, iterations,
				parallelism, version, must_rotate, created_at, updated_at
			) VALUES (?, 'argon2id', ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(identity_id) DO UPDATE SET
				algorithm = excluded.algorithm,
				password_hash = excluded.password_hash,
				salt = excluded.salt,
				memory_kib = excluded.memory_kib,
				iterations = excluded.iterations,
				parallelism = excluded.parallelism,
				version = excluded.version,
				must_rotate = excluded.must_rotate,
				updated_at = excluded.updated_at`,
			string(params.IdentityID),
			params.Hash,
			params.Salt,
			params.MemoryKiB,
			params.Iterations,
			params.Parallelism,
			params.Version,
			params.MustRotate,
			formatTime(now),
			formatTime(now),
		)
		if err != nil {
			return err
		}
		revokedSessions, err := revokeIdentitySessionsTx(
			ctx, executor, params.IdentityID, nil, "credential_changed", now,
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "identity.password_credential_set",
			TargetType: "identity",
			TargetID:   string(params.IdentityID),
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"algorithm":       "argon2id",
				"mustRotate":      params.MustRotate,
				"revokedSessions": revokedSessions,
			},
		}, now)
	})
	return classifyDatabaseError(err)
}

func (r *Repository) CreateSession(ctx context.Context, params CreateSessionParams) (Session, error) {
	if err := validateID(params.IdentityID, "identityId"); err != nil {
		return Session{}, err
	}
	if len(params.TokenHash) != 32 || len(params.CSRFSecretHash) != 32 {
		return Session{}, fmt.Errorf("%w: session hashes must contain exactly 32 bytes", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Session{}, err
	}
	sourceAddress := strings.TrimSpace(params.SourceAddress)
	if sourceAddress != "" {
		address, err := netip.ParseAddr(sourceAddress)
		if err != nil {
			return Session{}, fmt.Errorf("%w: sourceAddress must be an IP address", ErrInvalidInput)
		}
		sourceAddress = address.String()
	}
	userAgent, err := validateOptionalText(params.UserAgent, "userAgent", 512)
	if err != nil {
		return Session{}, err
	}
	now := r.timestamp()
	expiresAt := params.ExpiresAt.UTC()
	if !expiresAt.After(now) {
		return Session{}, fmt.Errorf("%w: session expiry must be in the future", ErrInvalidInput)
	}
	id, err := r.newID()
	if err != nil {
		return Session{}, err
	}
	session := Session{
		ID: id, IdentityID: params.IdentityID,
		CreatedAt: now, AuthenticatedAt: now, LastSeenAt: now,
		ExpiresAt: expiresAt, SourceAddress: sourceAddress, UserAgent: userAgent,
		AuthenticationLevel: SessionAuthenticationPassword,
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			INSERT INTO sessions (
				id, identity_id, token_hash, csrf_secret_hash, created_at, authenticated_at,
				last_seen_at, expires_at, source_address, user_agent
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(session.ID),
			string(session.IdentityID),
			params.TokenHash,
			params.CSRFSecretHash,
			formatTime(now),
			formatTime(now),
			formatTime(now),
			formatTime(expiresAt),
			nullableString(sourceAddress),
			nullableString(userAgent),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:       &params.IdentityID,
			SessionID:     &session.ID,
			SourceAddress: sourceAddress,
			Action:        "session.created",
			TargetType:    "session",
			TargetID:      string(session.ID),
			RequestID:     requestID,
			Result:        AuditSuccess,
			Details: map[string]any{
				"expiresAt": formatTime(expiresAt),
			},
		}, now)
	})
	if err != nil {
		return Session{}, classifyDatabaseError(err)
	}
	return session, nil
}

func (r *Repository) GrantPlatformRole(ctx context.Context, params GrantPlatformRoleParams) error {
	if err := validateID(params.IdentityID, "identityId"); err != nil {
		return err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return err
	}
	if params.Role != PlatformAdministrator {
		return fmt.Errorf("%w: unsupported platform role", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	now := r.timestamp()

	err = r.state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			INSERT INTO platform_role_assignments (
				identity_id, role, granted_at, granted_by_identity_id
			) VALUES (?, ?, ?, ?)`,
			string(params.IdentityID),
			string(params.Role),
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "platform_role.granted",
			TargetType: "identity",
			TargetID:   string(params.IdentityID),
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"role": params.Role,
			},
		}, now)
	})
	return classifyDatabaseError(err)
}
