-- SPDX-License-Identifier: AGPL-3.0-or-later

-- A handoff stores only a digest of the browser-carried bearer. Credentials
-- remain in the managed database-user envelope and are decrypted only while
-- an authenticated, audience-bound redemption transaction is being consumed.
CREATE TABLE phpmyadmin_handoffs (
    id TEXT PRIMARY KEY NOT NULL
        CHECK (
            length(id) = 36 AND id = lower(id)
            AND id NOT GLOB '*[^0-9a-f-]*'
            AND length(replace(id, '-', '')) = 32
            AND substr(id, 9, 1) = '-' AND substr(id, 14, 1) = '-'
            AND substr(id, 15, 1) = '7' AND substr(id, 19, 1) = '-'
            AND substr(id, 20, 1) IN ('8', '9', 'a', 'b')
            AND substr(id, 24, 1) = '-'
        ),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    account_id TEXT NOT NULL REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    database_user_id TEXT NOT NULL,
    identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    audience TEXT NOT NULL CHECK (audience = 'stackfort-phpmyadmin-v1'),
    state TEXT NOT NULL CHECK (state IN ('issued', 'consumed', 'revoked')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    consumed_at TEXT,
    revoked_at TEXT,
    FOREIGN KEY (account_id, database_user_id)
        REFERENCES managed_database_users(account_id, id) ON DELETE RESTRICT,
    CHECK (
        (state = 'issued' AND consumed_at IS NULL AND revoked_at IS NULL)
        OR (state = 'consumed' AND consumed_at IS NOT NULL AND revoked_at IS NULL)
        OR (state = 'revoked' AND consumed_at IS NULL AND revoked_at IS NOT NULL)
    )
) STRICT;

CREATE INDEX phpmyadmin_handoffs_subject_idx
    ON phpmyadmin_handoffs(session_id, database_user_id, state, created_at);

CREATE TRIGGER phpmyadmin_handoffs_restrict_update
BEFORE UPDATE ON phpmyadmin_handoffs
WHEN NEW.id IS NOT OLD.id
    OR NEW.token_hash IS NOT OLD.token_hash
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.database_user_id IS NOT OLD.database_user_id
    OR NEW.identity_id IS NOT OLD.identity_id
    OR NEW.session_id IS NOT OLD.session_id
    OR NEW.audience IS NOT OLD.audience
    OR NEW.expires_at IS NOT OLD.expires_at
    OR NEW.created_at IS NOT OLD.created_at
    OR OLD.state <> 'issued'
    OR NEW.state NOT IN ('consumed', 'revoked')
BEGIN
    SELECT RAISE(ABORT, 'phpMyAdmin handoff transition is invalid');
END;

