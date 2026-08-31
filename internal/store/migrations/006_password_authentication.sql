-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE TABLE authentication_rate_limits (
    scope TEXT NOT NULL CHECK (scope IN ('global', 'source', 'identity')),
    rate_key TEXT NOT NULL CHECK (length(rate_key) BETWEEN 1 AND 64),
    window_started_at TEXT NOT NULL,
    attempt_count INTEGER NOT NULL CHECK (attempt_count BETWEEN 1 AND 1000000),
    blocked_until TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (scope, rate_key)
) WITHOUT ROWID, STRICT;

CREATE INDEX authentication_rate_limits_cleanup_idx
    ON authentication_rate_limits(updated_at)
    WHERE scope != 'global';
