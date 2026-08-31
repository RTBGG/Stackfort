-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Composite parent keys keep every domain relation account-scoped. A caller
-- cannot attach a root, redirect, desired revision, or operation owned by a
-- different hosting account even if it learns an opaque object identifier.
CREATE UNIQUE INDEX desired_state_revisions_account_id_idx
    ON desired_state_revisions(account_id, id);

CREATE UNIQUE INDEX operations_account_id_idx
    ON operations(account_id, id);

CREATE TABLE document_roots (
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
    relative_path TEXT NOT NULL CHECK (length(relative_path) BETWEEN 1 AND 1024),
    created_at TEXT NOT NULL,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    UNIQUE (account_id, id),
    UNIQUE (account_id, relative_path)
) STRICT;

CREATE TRIGGER document_roots_no_update
BEFORE UPDATE ON document_roots
BEGIN
    SELECT RAISE(ABORT, 'document roots are immutable');
END;

CREATE TRIGGER document_roots_no_delete
BEFORE DELETE ON document_roots
BEGIN
    SELECT RAISE(ABORT, 'document roots are retained for domain history');
END;

CREATE TABLE domains (
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
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 3 AND 253),
    ascii_name TEXT NOT NULL
        CHECK (
            length(ascii_name) BETWEEN 3 AND 253
            AND ascii_name = lower(ascii_name)
            AND ascii_name NOT GLOB '*[^a-z0-9.-]*'
            AND substr(ascii_name, 1, 1) GLOB '[a-z0-9]'
            AND substr(ascii_name, -1, 1) GLOB '[a-z0-9]'
        ),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'suspended', 'removed')),
    canonical_mode TEXT NOT NULL
        CHECK (canonical_mode IN ('prefer_apex', 'prefer_www', 'serve_both')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    UNIQUE (account_id, id),
    CHECK (
        (status = 'removed' AND removed_at IS NOT NULL)
        OR (status <> 'removed' AND removed_at IS NULL)
    )
) STRICT;

-- ascii_name is the canonical base without a leading "www.". Consequently a
-- row reserves both the base host and its www alias across all accounts.
CREATE UNIQUE INDEX domains_live_ascii_name_idx
    ON domains(ascii_name)
    WHERE removed_at IS NULL;

CREATE INDEX domains_account_idx
    ON domains(account_id, created_at)
    WHERE removed_at IS NULL;

CREATE TRIGGER domains_restrict_update
BEFORE UPDATE ON domains
WHEN
    OLD.removed_at IS NOT NULL
    OR NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.display_name IS NOT OLD.display_name
    OR NEW.ascii_name IS NOT OLD.ascii_name
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.created_by_identity_id IS NOT OLD.created_by_identity_id
BEGIN
    SELECT RAISE(ABORT, 'domain identity and removal history are immutable');
END;

CREATE TRIGGER domains_no_delete
BEFORE DELETE ON domains
BEGIN
    SELECT RAISE(ABORT, 'domains are removed logically and retained for history');
END;

CREATE TABLE domain_redirects (
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
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    status_code INTEGER NOT NULL CHECK (status_code IN (301, 302)),
    target_url TEXT NOT NULL CHECK (length(target_url) BETWEEN 9 AND 2048),
    target_ascii_host TEXT NOT NULL
        CHECK (length(target_ascii_host) BETWEEN 1 AND 253 AND target_ascii_host = lower(target_ascii_host)),
    preserve_path INTEGER NOT NULL CHECK (preserve_path IN (0, 1)),
    preserve_query INTEGER NOT NULL CHECK (preserve_query IN (0, 1)),
    wildcard_subdomains INTEGER NOT NULL CHECK (wildcard_subdomains IN (0, 1)),
    created_at TEXT NOT NULL,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    UNIQUE (account_id, domain_id, id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id)
        ON DELETE RESTRICT
) STRICT;

CREATE TRIGGER domain_redirects_no_update
BEFORE UPDATE ON domain_redirects
BEGIN
    SELECT RAISE(ABORT, 'domain redirects are immutable target revisions');
END;

CREATE TRIGGER domain_redirects_no_delete
BEFORE DELETE ON domain_redirects
BEGIN
    SELECT RAISE(ABORT, 'domain redirects are retained for history');
END;

CREATE TABLE domain_targets (
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
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    target_type TEXT NOT NULL CHECK (target_type IN ('static', 'php', 'oci_application', 'redirect')),
    document_root_id TEXT,
    php_version TEXT CHECK (php_version IS NULL OR length(php_version) BETWEEN 3 AND 16),
    application_id TEXT
        CHECK (
            application_id IS NULL
            OR (
                length(application_id) = 36
                AND application_id = lower(application_id)
                AND application_id NOT GLOB '*[^0-9a-f-]*'
                AND length(replace(application_id, '-', '')) = 32
                AND substr(application_id, 9, 1) = '-'
                AND substr(application_id, 14, 1) = '-'
                AND substr(application_id, 15, 1) = '7'
                AND substr(application_id, 19, 1) = '-'
                AND substr(application_id, 20, 1) IN ('8', '9', 'a', 'b')
                AND substr(application_id, 24, 1) = '-'
            )
        ),
    redirect_id TEXT,
    created_at TEXT NOT NULL,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    superseded_at TEXT,
    UNIQUE (account_id, domain_id, id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id)
        ON DELETE RESTRICT,
    FOREIGN KEY (account_id, document_root_id)
        REFERENCES document_roots(account_id, id)
        ON DELETE RESTRICT,
    FOREIGN KEY (account_id, domain_id, redirect_id)
        REFERENCES domain_redirects(account_id, domain_id, id)
        ON DELETE RESTRICT,
    CHECK (
        (target_type = 'static' AND document_root_id IS NOT NULL AND php_version IS NULL AND application_id IS NULL AND redirect_id IS NULL)
        OR (target_type = 'php' AND document_root_id IS NOT NULL AND php_version IS NOT NULL AND application_id IS NULL AND redirect_id IS NULL)
        OR (target_type = 'oci_application' AND document_root_id IS NULL AND php_version IS NULL AND application_id IS NOT NULL AND redirect_id IS NULL)
        OR (target_type = 'redirect' AND document_root_id IS NULL AND php_version IS NULL AND application_id IS NULL AND redirect_id IS NOT NULL)
    )
) STRICT;

CREATE UNIQUE INDEX domain_targets_current_idx
    ON domain_targets(account_id, domain_id)
    WHERE superseded_at IS NULL;

CREATE INDEX domain_targets_history_idx
    ON domain_targets(account_id, domain_id, created_at DESC);

CREATE TRIGGER domain_targets_restrict_update
BEFORE UPDATE ON domain_targets
WHEN
    OLD.superseded_at IS NOT NULL
    OR NEW.superseded_at IS NULL
    OR NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.domain_id IS NOT OLD.domain_id
    OR NEW.target_type IS NOT OLD.target_type
    OR NEW.document_root_id IS NOT OLD.document_root_id
    OR NEW.php_version IS NOT OLD.php_version
    OR NEW.application_id IS NOT OLD.application_id
    OR NEW.redirect_id IS NOT OLD.redirect_id
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.created_by_identity_id IS NOT OLD.created_by_identity_id
BEGIN
    SELECT RAISE(ABORT, 'domain target revisions are immutable');
END;

CREATE TRIGGER domain_targets_no_delete
BEFORE DELETE ON domain_targets
BEGIN
    SELECT RAISE(ABORT, 'domain targets are retained for history');
END;

CREATE TABLE domain_tls_states (
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    mode TEXT NOT NULL CHECK (mode IN ('acme', 'imported')),
    challenge_type TEXT NOT NULL CHECK (challenge_type IN ('http-01', 'dns-01', 'imported')),
    issuance_status TEXT NOT NULL
        CHECK (issuance_status IN ('disabled', 'pending', 'issuing', 'active', 'renewing', 'failed')),
    names_json TEXT NOT NULL
        CHECK (
            json_valid(names_json)
            AND json_type(names_json) = 'array'
            AND json_array_length(names_json) BETWEEN 1 AND 100
        ),
    active_certificate_ref TEXT
        CHECK (active_certificate_ref IS NULL OR length(active_certificate_ref) BETWEEN 1 AND 512),
    issuer TEXT CHECK (issuer IS NULL OR length(issuer) <= 253),
    not_before TEXT,
    expires_at TEXT,
    next_renewal_at TEXT,
    last_error_code TEXT CHECK (last_error_code IS NULL OR length(last_error_code) <= 80),
    last_error_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (domain_id),
    UNIQUE (account_id, domain_id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id)
        ON DELETE RESTRICT,
    CHECK (
        (enabled = 0 AND issuance_status = 'disabled')
        OR enabled = 1
    ),
    CHECK (
        (mode = 'imported' AND challenge_type = 'imported')
        OR (mode = 'acme' AND challenge_type IN ('http-01', 'dns-01'))
    )
) WITHOUT ROWID, STRICT;

CREATE TRIGGER domain_tls_states_no_delete
BEFORE DELETE ON domain_tls_states
BEGIN
    SELECT RAISE(ABORT, 'domain TLS state is retained for history');
END;

CREATE TABLE applied_state_revisions (
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
    desired_state_revision_id TEXT NOT NULL,
    operation_id TEXT,
    config_digest BLOB NOT NULL CHECK (length(config_digest) = 32),
    status TEXT NOT NULL CHECK (status IN ('active', 'superseded', 'rolled_back')),
    applied_at TEXT NOT NULL,
    superseded_at TEXT,
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, desired_state_revision_id)
        REFERENCES desired_state_revisions(account_id, id)
        ON DELETE RESTRICT,
    FOREIGN KEY (account_id, operation_id)
        REFERENCES operations(account_id, id)
        ON DELETE RESTRICT,
    CHECK (
        (status = 'active' AND superseded_at IS NULL)
        OR (status <> 'active' AND superseded_at IS NOT NULL)
    )
) STRICT;

CREATE UNIQUE INDEX applied_state_revisions_current_idx
    ON applied_state_revisions(account_id)
    WHERE status = 'active';

CREATE INDEX applied_state_revisions_account_idx
    ON applied_state_revisions(account_id, applied_at DESC);

CREATE TRIGGER applied_state_revisions_restrict_update
BEFORE UPDATE ON applied_state_revisions
WHEN
    OLD.status <> 'active'
    OR NEW.status <> 'superseded'
    OR NEW.superseded_at IS NULL
    OR NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.desired_state_revision_id IS NOT OLD.desired_state_revision_id
    OR NEW.operation_id IS NOT OLD.operation_id
    OR NEW.config_digest IS NOT OLD.config_digest
    OR NEW.applied_at IS NOT OLD.applied_at
BEGIN
    SELECT RAISE(ABORT, 'applied-state revisions are immutable');
END;

CREATE TRIGGER applied_state_revisions_no_delete
BEFORE DELETE ON applied_state_revisions
BEGIN
    SELECT RAISE(ABORT, 'applied-state revisions are retained for history');
END;
