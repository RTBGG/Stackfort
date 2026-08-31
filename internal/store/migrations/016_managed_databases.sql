-- SPDX-License-Identifier: AGPL-3.0-or-later

-- MariaDB objects use a full account UUID prefix. The user-visible alias is
-- intentionally limited to 28 characters so the derived physical identifier
-- is at most MariaDB's 64-character database identifier limit:
-- sf_<32 UUID hex characters>_<28 alias characters>.
CREATE TABLE managed_databases (
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
    account_id TEXT NOT NULL REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    alias TEXT NOT NULL
        CHECK (
            length(alias) BETWEEN 1 AND 28
            AND alias = lower(alias)
            AND alias NOT GLOB '*[^a-z0-9_]*'
            AND substr(alias, 1, 1) GLOB '[a-z]'
        ),
    physical_name TEXT NOT NULL UNIQUE
        CHECK (
            physical_name = 'sf_' || replace(account_id, '-', '') || '_' || alias
            AND length(physical_name) <= 64
        ),
    status TEXT NOT NULL
        CHECK (status IN ('pending', 'active', 'deleting', 'error', 'deleted')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    UNIQUE (account_id, id),
    CHECK (
        (status = 'deleted' AND removed_at IS NOT NULL)
        OR (status <> 'deleted' AND removed_at IS NULL)
    )
) STRICT;

CREATE UNIQUE INDEX managed_databases_live_alias_idx
    ON managed_databases(account_id, alias)
    WHERE removed_at IS NULL;

CREATE INDEX managed_databases_account_idx
    ON managed_databases(account_id, created_at, id)
    WHERE removed_at IS NULL;

CREATE TABLE managed_database_users (
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
    account_id TEXT NOT NULL REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    alias TEXT NOT NULL
        CHECK (
            length(alias) BETWEEN 1 AND 28
            AND alias = lower(alias)
            AND alias NOT GLOB '*[^a-z0-9_]*'
            AND substr(alias, 1, 1) GLOB '[a-z]'
        ),
    physical_name TEXT NOT NULL UNIQUE
        CHECK (
            physical_name = 'sf_' || replace(account_id, '-', '') || '_' || alias
            AND length(physical_name) <= 64
        ),
    host TEXT NOT NULL CHECK (host = 'localhost'),
    status TEXT NOT NULL
        CHECK (status IN ('pending', 'active', 'deleting', 'error', 'deleted')),
    password_ciphertext BLOB NOT NULL,
    password_nonce BLOB NOT NULL CHECK (length(password_nonce) = 12),
    password_wrapped_key BLOB NOT NULL CHECK (length(password_wrapped_key) = 48),
    password_wrap_nonce BLOB NOT NULL CHECK (length(password_wrap_nonce) = 12),
    password_key_version INTEGER NOT NULL CHECK (password_key_version > 0),
    password_revealed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    UNIQUE (account_id, id),
    CHECK (
        (status = 'deleted' AND removed_at IS NOT NULL)
        OR (status <> 'deleted' AND removed_at IS NULL)
    )
) STRICT;

CREATE UNIQUE INDEX managed_database_users_live_alias_idx
    ON managed_database_users(account_id, alias)
    WHERE removed_at IS NULL;

CREATE INDEX managed_database_users_account_idx
    ON managed_database_users(account_id, created_at, id)
    WHERE removed_at IS NULL;

CREATE TABLE managed_database_grants (
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
    account_id TEXT NOT NULL REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    database_id TEXT NOT NULL,
    database_user_id TEXT NOT NULL,
    preset TEXT NOT NULL CHECK (preset IN ('read_only', 'read_write')),
    status TEXT NOT NULL
        CHECK (status IN ('pending', 'active', 'revoking', 'error', 'revoked')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revoked_at TEXT,
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, database_id)
        REFERENCES managed_databases(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, database_user_id)
        REFERENCES managed_database_users(account_id, id) ON DELETE RESTRICT,
    CHECK (
        (status = 'revoked' AND revoked_at IS NOT NULL)
        OR (status <> 'revoked' AND revoked_at IS NULL)
    )
) STRICT;

CREATE UNIQUE INDEX managed_database_grants_live_pair_idx
    ON managed_database_grants(account_id, database_id, database_user_id)
    WHERE revoked_at IS NULL;

CREATE INDEX managed_database_grants_account_idx
    ON managed_database_grants(account_id, database_id, database_user_id)
    WHERE revoked_at IS NULL;

-- One lifecycle mutation is paired with one durable operation. The target
-- union is intentionally closed so a worker cannot receive an ambiguous drop.
CREATE TABLE managed_database_mutations (
    operation_id TEXT PRIMARY KEY NOT NULL REFERENCES operations(id) ON DELETE RESTRICT,
    account_id TEXT NOT NULL,
    action TEXT NOT NULL
        CHECK (action IN ('provision', 'drop_database', 'drop_user')),
    database_id TEXT,
    database_user_id TEXT,
    grant_id TEXT,
    applied_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (account_id, operation_id),
    FOREIGN KEY (account_id, operation_id)
        REFERENCES operations(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, database_id)
        REFERENCES managed_databases(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, database_user_id)
        REFERENCES managed_database_users(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, grant_id)
        REFERENCES managed_database_grants(account_id, id) ON DELETE RESTRICT,
    CHECK (
        (action = 'provision'
            AND database_id IS NOT NULL
            AND database_user_id IS NOT NULL
            AND grant_id IS NOT NULL)
        OR
        (action = 'drop_database'
            AND database_id IS NOT NULL
            AND database_user_id IS NULL
            AND grant_id IS NULL)
        OR
        (action = 'drop_user'
            AND database_id IS NULL
            AND database_user_id IS NOT NULL
            AND grant_id IS NULL)
    )
) WITHOUT ROWID, STRICT;

CREATE TRIGGER managed_database_mutations_restrict_update
BEFORE UPDATE ON managed_database_mutations
WHEN OLD.applied_at IS NOT NULL
    OR NEW.operation_id IS NOT OLD.operation_id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.action IS NOT OLD.action
    OR NEW.database_id IS NOT OLD.database_id
    OR NEW.database_user_id IS NOT OLD.database_user_id
    OR NEW.grant_id IS NOT OLD.grant_id
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.applied_at IS NULL
BEGIN
    SELECT RAISE(ABORT, 'managed database mutation is immutable after application');
END;

CREATE TRIGGER managed_database_mutations_no_delete
BEFORE DELETE ON managed_database_mutations
BEGIN
    SELECT RAISE(ABORT, 'managed database mutations are retained');
END;
