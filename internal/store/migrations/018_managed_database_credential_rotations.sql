-- SPDX-License-Identifier: AGPL-3.0-or-later

-- A rotation keeps its candidate envelope separate from the authoritative
-- managed_database_users envelope. The candidate is promoted only after the
-- host agent has successfully changed the MariaDB principal. Keeping one
-- unresolved rotation per principal also prevents an older retry from
-- overwriting a newer credential.
CREATE TABLE managed_database_credential_rotations (
    operation_id TEXT PRIMARY KEY NOT NULL REFERENCES operations(id) ON DELETE RESTRICT,
    account_id TEXT NOT NULL,
    database_user_id TEXT NOT NULL,
    password_ciphertext BLOB NOT NULL,
    password_nonce BLOB NOT NULL CHECK (length(password_nonce) = 12),
    password_wrapped_key BLOB NOT NULL CHECK (length(password_wrapped_key) = 48),
    password_wrap_nonce BLOB NOT NULL CHECK (length(password_wrap_nonce) = 12),
    password_key_version INTEGER NOT NULL CHECK (password_key_version > 0),
    applied_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (account_id, operation_id),
    FOREIGN KEY (account_id, operation_id)
        REFERENCES operations(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, database_user_id)
        REFERENCES managed_database_users(account_id, id) ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

CREATE UNIQUE INDEX managed_database_credential_rotations_pending_user_idx
    ON managed_database_credential_rotations(account_id, database_user_id)
    WHERE applied_at IS NULL;

CREATE INDEX managed_database_credential_rotations_user_idx
    ON managed_database_credential_rotations(account_id, database_user_id, created_at);

CREATE TRIGGER managed_database_credential_rotations_restrict_update
BEFORE UPDATE ON managed_database_credential_rotations
WHEN OLD.applied_at IS NOT NULL
    OR NEW.operation_id IS NOT OLD.operation_id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.database_user_id IS NOT OLD.database_user_id
    OR NEW.password_ciphertext IS NOT OLD.password_ciphertext
    OR NEW.password_nonce IS NOT OLD.password_nonce
    OR NEW.password_wrapped_key IS NOT OLD.password_wrapped_key
    OR NEW.password_wrap_nonce IS NOT OLD.password_wrap_nonce
    OR NEW.password_key_version IS NOT OLD.password_key_version
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.applied_at IS NULL
BEGIN
    SELECT RAISE(ABORT, 'managed database credential rotation transition is invalid');
END;

CREATE TRIGGER managed_database_credential_rotations_no_delete
BEFORE DELETE ON managed_database_credential_rotations
BEGIN
    SELECT RAISE(ABORT, 'managed database credential rotations are retained');
END;

-- This monotonic generation is safe to expose and lets later runtime work
-- fence phpMyAdmin sessions without exposing credential material.
ALTER TABLE managed_database_users
    ADD COLUMN credential_generation INTEGER NOT NULL DEFAULT 1
        CHECK (credential_generation > 0);
