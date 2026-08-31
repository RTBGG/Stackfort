-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Administrator-created WAF exceptions are closed, account/domain-correlated,
-- operation-correlated, short-lived records. History is retained through soft
-- removal; an expiry embedded in the generated Coraza rule makes stale records
-- ineffective even if reconciliation is delayed.
CREATE TABLE domain_waf_exceptions (
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
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    rule_id INTEGER NOT NULL CHECK (rule_id BETWEEN 920000 AND 944999),
    request_path TEXT NOT NULL
        CHECK (length(request_path) <= 512 AND request_path = trim(request_path)),
    parameter_name TEXT NOT NULL
        CHECK (length(parameter_name) <= 128 AND parameter_name = trim(parameter_name)),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    created_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    creation_operation_id TEXT NOT NULL,
    removed_at TEXT,
    removed_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    removal_operation_id TEXT,
    UNIQUE (account_id, id),
    UNIQUE (account_id, creation_operation_id),
    UNIQUE (account_id, removal_operation_id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, creation_operation_id)
        REFERENCES operations(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, removal_operation_id)
        REFERENCES operations(account_id, id) ON DELETE RESTRICT,
    CHECK (request_path <> '' OR parameter_name <> ''),
    CHECK (
        (removed_at IS NULL AND removed_by_identity_id IS NULL AND removal_operation_id IS NULL)
        OR (removed_at IS NOT NULL AND removal_operation_id IS NOT NULL)
    )
) STRICT;

CREATE INDEX domain_waf_exceptions_active_idx
    ON domain_waf_exceptions(account_id, domain_id, expires_at, id)
    WHERE removed_at IS NULL;

CREATE TRIGGER domain_waf_exceptions_restrict_update
BEFORE UPDATE ON domain_waf_exceptions
WHEN OLD.id IS NOT NEW.id
    OR OLD.account_id IS NOT NEW.account_id
    OR OLD.domain_id IS NOT NEW.domain_id
    OR OLD.rule_id IS NOT NEW.rule_id
    OR OLD.request_path IS NOT NEW.request_path
    OR OLD.parameter_name IS NOT NEW.parameter_name
    OR OLD.expires_at IS NOT NEW.expires_at
    OR OLD.created_at IS NOT NEW.created_at
    OR OLD.created_by_identity_id IS NOT NEW.created_by_identity_id
    OR OLD.creation_operation_id IS NOT NEW.creation_operation_id
    OR OLD.removed_at IS NOT NULL
    OR NEW.removed_at IS NULL
    OR NEW.removal_operation_id IS NULL
BEGIN
    SELECT RAISE(ABORT, 'domain WAF exception transition is invalid');
END;

CREATE TRIGGER domain_waf_exceptions_no_delete
BEFORE DELETE ON domain_waf_exceptions
BEGIN
    SELECT RAISE(ABORT, 'domain WAF exceptions are retained');
END;
