-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE TABLE bootstrap_capabilities (
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
    active_slot INTEGER NOT NULL CHECK (active_slot = 1),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    consumed_by_identity_id TEXT REFERENCES identities(id) ON DELETE RESTRICT,
    invalidated_at TEXT,
    invalidation_reason TEXT
        CHECK (invalidation_reason IS NULL OR invalidation_reason IN ('expired', 'replaced')),
    CHECK (
        (consumed_at IS NULL AND consumed_by_identity_id IS NULL)
        OR (consumed_at IS NOT NULL AND consumed_by_identity_id IS NOT NULL)
    ),
    CHECK (
        (invalidated_at IS NULL AND invalidation_reason IS NULL)
        OR (invalidated_at IS NOT NULL AND invalidation_reason IS NOT NULL)
    ),
    CHECK (consumed_at IS NULL OR invalidated_at IS NULL)
) STRICT;

CREATE UNIQUE INDEX bootstrap_capabilities_one_active_idx
    ON bootstrap_capabilities(active_slot)
    WHERE consumed_at IS NULL AND invalidated_at IS NULL;

CREATE TRIGGER bootstrap_capabilities_restrict_update
BEFORE UPDATE ON bootstrap_capabilities
WHEN
    OLD.consumed_at IS NOT NULL
    OR OLD.invalidated_at IS NOT NULL
    OR NEW.id IS NOT OLD.id
    OR NEW.active_slot IS NOT OLD.active_slot
    OR NEW.token_hash IS NOT OLD.token_hash
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.expires_at IS NOT OLD.expires_at
    OR (NEW.consumed_at IS NULL AND NEW.invalidated_at IS NULL)
    OR (NEW.consumed_at IS NOT NULL AND NEW.invalidated_at IS NOT NULL)
BEGIN
    SELECT RAISE(ABORT, 'bootstrap capabilities permit one terminal transition');
END;

CREATE TRIGGER bootstrap_capabilities_no_delete
BEFORE DELETE ON bootstrap_capabilities
BEGIN
    SELECT RAISE(ABORT, 'bootstrap capability history is immutable');
END;

CREATE TABLE bootstrap_rate_limits (
    scope TEXT NOT NULL CHECK (scope IN ('global', 'source')),
    rate_key TEXT NOT NULL CHECK (length(rate_key) BETWEEN 1 AND 64),
    window_started_at TEXT NOT NULL,
    attempt_count INTEGER NOT NULL CHECK (attempt_count BETWEEN 1 AND 1000000),
    blocked_until TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (scope, rate_key)
) WITHOUT ROWID, STRICT;

CREATE INDEX bootstrap_rate_limits_cleanup_idx
    ON bootstrap_rate_limits(updated_at)
    WHERE scope = 'source';
