-- SPDX-License-Identifier: AGPL-3.0-or-later

ALTER TABLE sessions ADD COLUMN authentication_level TEXT NOT NULL DEFAULT 'password'
    CHECK (authentication_level IN ('password', 'totp', 'recovery'));
ALTER TABLE sessions ADD COLUMN mfa_authenticated_at TEXT;

CREATE TRIGGER sessions_authentication_level_insert
BEFORE INSERT ON sessions
WHEN (NEW.authentication_level = 'password' AND NEW.mfa_authenticated_at IS NOT NULL)
  OR (NEW.authentication_level != 'password' AND NEW.mfa_authenticated_at IS NULL)
BEGIN
    SELECT RAISE(ABORT, 'session authentication level is inconsistent');
END;

CREATE TRIGGER sessions_authentication_level_update
BEFORE UPDATE OF authentication_level, mfa_authenticated_at ON sessions
WHEN (NEW.authentication_level = 'password' AND NEW.mfa_authenticated_at IS NOT NULL)
  OR (NEW.authentication_level != 'password' AND NEW.mfa_authenticated_at IS NULL)
BEGIN
    SELECT RAISE(ABORT, 'session authentication level is inconsistent');
END;

CREATE TABLE totp_factors (
    id TEXT PRIMARY KEY NOT NULL
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND id NOT GLOB '*[^0-9a-f-]*'
            AND length(replace(id, '-', '')) = 32
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 15, 1) = '7'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 20, 1) IN ('8', '9', 'a', 'b')
            AND substr(id, 24, 1) = '-'
        ),
    identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('active', 'replaced', 'removed')),
    algorithm TEXT NOT NULL CHECK (algorithm = 'SHA1'),
    digits INTEGER NOT NULL CHECK (digits = 6),
    period_seconds INTEGER NOT NULL CHECK (period_seconds = 30),
    secret_ciphertext BLOB NOT NULL CHECK (length(secret_ciphertext) BETWEEN 32 AND 128),
    secret_nonce BLOB NOT NULL CHECK (length(secret_nonce) = 12),
    wrapped_dek BLOB NOT NULL CHECK (length(wrapped_dek) = 48),
    wrap_nonce BLOB NOT NULL CHECK (length(wrap_nonce) = 12),
    key_version INTEGER NOT NULL CHECK (key_version > 0),
    last_used_counter INTEGER CHECK (last_used_counter IS NULL OR last_used_counter >= 0),
    created_at TEXT NOT NULL,
    activated_at TEXT NOT NULL,
    deactivated_at TEXT,
    CHECK (
        (status = 'active' AND deactivated_at IS NULL)
        OR (status != 'active' AND deactivated_at IS NOT NULL)
    ),
    UNIQUE (id, identity_id)
) STRICT;

CREATE UNIQUE INDEX totp_factors_identity_active_idx
    ON totp_factors(identity_id)
    WHERE status = 'active';

CREATE TABLE totp_setup_challenges (
    id TEXT PRIMARY KEY NOT NULL
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND id NOT GLOB '*[^0-9a-f-]*'
            AND length(replace(id, '-', '')) = 32
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 15, 1) = '7'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 20, 1) IN ('8', '9', 'a', 'b')
            AND substr(id, 24, 1) = '-'
        ),
    identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    replaces_factor_id TEXT REFERENCES totp_factors(id) ON DELETE RESTRICT,
    secret_ciphertext BLOB NOT NULL CHECK (length(secret_ciphertext) BETWEEN 32 AND 128),
    secret_nonce BLOB NOT NULL CHECK (length(secret_nonce) = 12),
    wrapped_dek BLOB NOT NULL CHECK (length(wrapped_dek) = 48),
    wrap_nonce BLOB NOT NULL CHECK (length(wrap_nonce) = 12),
    key_version INTEGER NOT NULL CHECK (key_version > 0),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    completion_reason TEXT CHECK (
        completion_reason IS NULL OR completion_reason IN ('activated', 'replaced', 'expired', 'attempts_exhausted')
    ),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 5),
    CHECK (
        (consumed_at IS NULL AND completion_reason IS NULL)
        OR (consumed_at IS NOT NULL AND completion_reason IS NOT NULL)
    )
) STRICT;

CREATE UNIQUE INDEX totp_setup_challenges_identity_active_idx
    ON totp_setup_challenges(identity_id)
    WHERE consumed_at IS NULL;

CREATE TABLE recovery_codes (
    id TEXT PRIMARY KEY NOT NULL
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND id NOT GLOB '*[^0-9a-f-]*'
            AND length(replace(id, '-', '')) = 32
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 15, 1) = '7'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 20, 1) IN ('8', '9', 'a', 'b')
            AND substr(id, 24, 1) = '-'
        ),
    factor_id TEXT NOT NULL,
    identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    code_hash BLOB NOT NULL UNIQUE CHECK (length(code_hash) = 32),
    created_at TEXT NOT NULL,
    used_at TEXT,
    FOREIGN KEY (factor_id, identity_id)
        REFERENCES totp_factors(id, identity_id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX recovery_codes_identity_unused_idx
    ON recovery_codes(identity_id, factor_id)
    WHERE used_at IS NULL;

CREATE TABLE mfa_login_challenges (
    id TEXT PRIMARY KEY NOT NULL
        CHECK (
            length(id) = 36
            AND id = lower(id)
            AND id NOT GLOB '*[^0-9a-f-]*'
            AND length(replace(id, '-', '')) = 32
            AND substr(id, 9, 1) = '-'
            AND substr(id, 14, 1) = '-'
            AND substr(id, 15, 1) = '7'
            AND substr(id, 19, 1) = '-'
            AND substr(id, 20, 1) IN ('8', '9', 'a', 'b')
            AND substr(id, 24, 1) = '-'
        ),
    identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    previous_session_id TEXT REFERENCES sessions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 5),
    source_address TEXT CHECK (source_address IS NULL OR length(source_address) <= 64),
    user_agent TEXT CHECK (user_agent IS NULL OR length(user_agent) <= 512)
) STRICT;

CREATE INDEX mfa_login_challenges_identity_active_idx
    ON mfa_login_challenges(identity_id, expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE mfa_attempt_limits (
    identity_id TEXT PRIMARY KEY NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    window_started_at TEXT NOT NULL,
    failure_count INTEGER NOT NULL CHECK (failure_count >= 0),
    blocked_until TEXT,
    updated_at TEXT NOT NULL
) WITHOUT ROWID, STRICT;
