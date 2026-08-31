-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE TABLE identities (
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
    email TEXT NOT NULL CHECK (length(email) BETWEEN 3 AND 254),
    normalized_email TEXT NOT NULL UNIQUE
        CHECK (length(normalized_email) BETWEEN 3 AND 254 AND normalized_email = lower(normalized_email)),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 120),
    locale TEXT NOT NULL CHECK (locale IN ('en', 'de')),
    status TEXT NOT NULL CHECK (status IN ('active', 'suspended', 'archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT
) STRICT;

CREATE TABLE password_credentials (
    identity_id TEXT PRIMARY KEY NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    algorithm TEXT NOT NULL CHECK (algorithm = 'argon2id'),
    password_hash BLOB NOT NULL CHECK (length(password_hash) BETWEEN 16 AND 128),
    salt BLOB NOT NULL CHECK (length(salt) BETWEEN 16 AND 64),
    memory_kib INTEGER NOT NULL CHECK (memory_kib BETWEEN 8192 AND 4194304),
    iterations INTEGER NOT NULL CHECK (iterations BETWEEN 1 AND 100),
    parallelism INTEGER NOT NULL CHECK (parallelism BETWEEN 1 AND 64),
    version INTEGER NOT NULL CHECK (version > 0),
    must_rotate INTEGER NOT NULL CHECK (must_rotate IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE sessions (
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
    csrf_secret_hash BLOB NOT NULL CHECK (length(csrf_secret_hash) = 32),
    created_at TEXT NOT NULL,
    authenticated_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    revocation_reason TEXT CHECK (revocation_reason IS NULL OR length(revocation_reason) BETWEEN 1 AND 80),
    source_address TEXT CHECK (source_address IS NULL OR length(source_address) <= 64),
    user_agent TEXT CHECK (user_agent IS NULL OR length(user_agent) <= 512),
    CHECK (
        (revoked_at IS NULL AND revocation_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revocation_reason IS NOT NULL)
    )
) STRICT;

CREATE INDEX sessions_identity_active_idx
    ON sessions(identity_id, expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE platform_role_assignments (
    identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    role TEXT NOT NULL CHECK (role IN ('platform_admin')),
    granted_at TEXT NOT NULL,
    granted_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    PRIMARY KEY (identity_id, role)
) WITHOUT ROWID, STRICT;

CREATE TABLE packages (
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
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    slug TEXT NOT NULL UNIQUE
        CHECK (
            length(slug) BETWEEN 1 AND 63
            AND slug = lower(slug)
            AND slug NOT GLOB '*[^a-z0-9-]*'
            AND substr(slug, 1, 1) GLOB '[a-z]'
            AND substr(slug, -1, 1) GLOB '[a-z0-9]'
        ),
    status TEXT NOT NULL CHECK (status IN ('active', 'archived')),
    current_revision INTEGER NOT NULL CHECK (current_revision > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    FOREIGN KEY (id, current_revision)
        REFERENCES package_revisions(package_id, revision)
        DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE TABLE package_revisions (
    package_id TEXT NOT NULL REFERENCES packages(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL CHECK (revision > 0),
    limits_json TEXT NOT NULL CHECK (json_valid(limits_json) AND json_type(limits_json) = 'object'),
    created_at TEXT NOT NULL,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    PRIMARY KEY (package_id, revision)
) WITHOUT ROWID, STRICT;

CREATE TRIGGER package_revisions_no_update
BEFORE UPDATE ON package_revisions
BEGIN
    SELECT RAISE(ABORT, 'package revisions are immutable');
END;

CREATE TRIGGER package_revisions_no_delete
BEFORE DELETE ON package_revisions
BEGIN
    SELECT RAISE(ABORT, 'package revisions are immutable');
END;

CREATE TABLE hosting_accounts (
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
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    slug TEXT NOT NULL UNIQUE
        CHECK (
            length(slug) BETWEEN 1 AND 63
            AND slug = lower(slug)
            AND slug NOT GLOB '*[^a-z0-9-]*'
            AND substr(slug, 1, 1) GLOB '[a-z]'
            AND substr(slug, -1, 1) GLOB '[a-z0-9]'
        ),
    status TEXT NOT NULL CHECK (status IN ('active', 'suspended', 'archived')),
    current_package_assignment_id TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    archived_at TEXT,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    FOREIGN KEY (id, current_package_assignment_id)
        REFERENCES account_package_assignments(account_id, id)
        DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE TABLE account_memberships (
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
    account_id TEXT NOT NULL REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    role TEXT NOT NULL CHECK (role IN ('owner', 'member', 'auditor')),
    granted_at TEXT NOT NULL,
    granted_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    revoked_at TEXT
) STRICT;

CREATE UNIQUE INDEX account_memberships_active_idx
    ON account_memberships(account_id, identity_id)
    WHERE revoked_at IS NULL;

CREATE INDEX account_memberships_identity_idx
    ON account_memberships(identity_id, account_id)
    WHERE revoked_at IS NULL;

CREATE TABLE account_package_assignments (
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
    account_id TEXT NOT NULL REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    package_id TEXT NOT NULL,
    package_revision INTEGER NOT NULL CHECK (package_revision > 0),
    effective_limits_json TEXT NOT NULL
        CHECK (json_valid(effective_limits_json) AND json_type(effective_limits_json) = 'object'),
    assigned_at TEXT NOT NULL,
    assigned_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    superseded_at TEXT,
    UNIQUE (account_id, id),
    FOREIGN KEY (package_id, package_revision)
        REFERENCES package_revisions(package_id, revision)
        ON DELETE RESTRICT
) STRICT;

CREATE UNIQUE INDEX account_package_assignments_current_idx
    ON account_package_assignments(account_id)
    WHERE superseded_at IS NULL;

CREATE TRIGGER hosting_accounts_validate_current_assignment
BEFORE UPDATE OF current_package_assignment_id ON hosting_accounts
WHEN NOT EXISTS (
    SELECT 1
    FROM account_package_assignments
    WHERE id = NEW.current_package_assignment_id
      AND account_id = NEW.id
      AND superseded_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'current package assignment must be active and belong to the account');
END;

CREATE TRIGGER account_package_assignments_restrict_update
BEFORE UPDATE ON account_package_assignments
WHEN
    OLD.superseded_at IS NOT NULL
    OR NEW.superseded_at IS NULL
    OR NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.package_id IS NOT OLD.package_id
    OR NEW.package_revision IS NOT OLD.package_revision
    OR NEW.effective_limits_json IS NOT OLD.effective_limits_json
    OR NEW.assigned_at IS NOT OLD.assigned_at
    OR NEW.assigned_by_identity_id IS NOT OLD.assigned_by_identity_id
BEGIN
    SELECT RAISE(ABORT, 'package assignment snapshots are immutable');
END;

CREATE TRIGGER account_package_assignments_no_delete
BEFORE DELETE ON account_package_assignments
BEGIN
    SELECT RAISE(ABORT, 'package assignment snapshots are immutable');
END;

CREATE TABLE desired_state_revisions (
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
    account_id TEXT NOT NULL REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    document_json TEXT NOT NULL CHECK (json_valid(document_json) AND json_type(document_json) = 'object'),
    reason TEXT CHECK (reason IS NULL OR length(reason) <= 500),
    created_at TEXT NOT NULL,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    UNIQUE (account_id, sequence)
) STRICT;

CREATE INDEX desired_state_revisions_account_idx
    ON desired_state_revisions(account_id, sequence DESC);

CREATE TRIGGER desired_state_revisions_no_update
BEFORE UPDATE ON desired_state_revisions
BEGIN
    SELECT RAISE(ABORT, 'desired-state revisions are immutable');
END;

CREATE TRIGGER desired_state_revisions_no_delete
BEFORE DELETE ON desired_state_revisions
BEGIN
    SELECT RAISE(ABORT, 'desired-state revisions are immutable');
END;

CREATE TABLE operations (
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
    account_id TEXT REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    actor_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 80),
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelling', 'cancelled')),
    stage TEXT NOT NULL CHECK (length(stage) BETWEEN 1 AND 80),
    progress_percent INTEGER NOT NULL CHECK (progress_percent BETWEEN 0 AND 100),
    retry_class TEXT NOT NULL CHECK (retry_class IN ('none', 'safe', 'manual')),
    request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    idempotency_key TEXT CHECK (idempotency_key IS NULL OR length(idempotency_key) BETWEEN 1 AND 128),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json) AND json_type(payload_json) = 'object'),
    result_json TEXT CHECK (result_json IS NULL OR (json_valid(result_json) AND json_type(result_json) = 'object')),
    error_code TEXT CHECK (error_code IS NULL OR length(error_code) <= 80),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT
) STRICT;

CREATE UNIQUE INDEX operations_account_idempotency_idx
    ON operations(account_id, idempotency_key)
    WHERE account_id IS NOT NULL AND idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX operations_global_idempotency_idx
    ON operations(idempotency_key)
    WHERE account_id IS NULL AND idempotency_key IS NOT NULL;

CREATE INDEX operations_status_idx ON operations(status, created_at);
CREATE INDEX operations_account_idx ON operations(account_id, created_at DESC);

CREATE TABLE audit_events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE
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
    occurred_at TEXT NOT NULL,
    actor_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    session_id TEXT REFERENCES sessions(id) ON DELETE RESTRICT,
    source_address TEXT CHECK (source_address IS NULL OR length(source_address) <= 64),
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 120),
    target_type TEXT NOT NULL CHECK (length(target_type) BETWEEN 1 AND 80),
    target_id TEXT CHECK (target_id IS NULL OR length(target_id) <= 128),
    account_id TEXT REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    request_id TEXT CHECK (request_id IS NULL OR length(request_id) <= 128),
    operation_id TEXT REFERENCES operations(id) ON DELETE RESTRICT,
    result TEXT NOT NULL CHECK (result IN ('success', 'failure', 'denied')),
    details_json TEXT NOT NULL CHECK (json_valid(details_json) AND json_type(details_json) = 'object'),
    previous_hash BLOB NOT NULL CHECK (length(previous_hash) = 32),
    event_hash BLOB NOT NULL UNIQUE CHECK (length(event_hash) = 32)
) STRICT;

CREATE INDEX audit_events_account_idx ON audit_events(account_id, sequence DESC);
CREATE INDEX audit_events_actor_idx ON audit_events(actor_identity_id, sequence DESC);
CREATE INDEX audit_events_operation_idx ON audit_events(operation_id);

CREATE TRIGGER audit_events_no_update
BEFORE UPDATE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TRIGGER audit_events_no_delete
BEFORE DELETE ON audit_events
BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;
