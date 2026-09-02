-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Update discovery is intentionally separate from functional update
-- activation. The singleton policy defaults to safe, automatic stable-channel
-- checks while applied revisions remain future staged-updater state.
CREATE TABLE update_policy (
    singleton INTEGER PRIMARY KEY NOT NULL CHECK (singleton = 1),
    channel TEXT NOT NULL CHECK (channel IN ('stable', 'beta')),
    automatic_checks INTEGER NOT NULL CHECK (automatic_checks IN (0, 1)),
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO update_policy (singleton, channel, automatic_checks, updated_at)
VALUES (1, 'stable', 1, '1970-01-01T00:00:00Z');

-- Only data-minimized discovery state is retained. An ETag is an opaque
-- validator, not a credential. A candidate is stored only after its release
-- channel, immutability, asset inventory, and semantic version passed the
-- application-level verifier.
CREATE TABLE update_check_state (
    singleton INTEGER PRIMARY KEY NOT NULL CHECK (singleton = 1),
    etag TEXT NOT NULL DEFAULT '' CHECK (length(etag) <= 512),
    last_attempted_at TEXT,
    last_successful_at TEXT,
    latest_version TEXT CHECK (latest_version IS NULL OR length(latest_version) BETWEEN 5 AND 64),
    latest_tag TEXT CHECK (latest_tag IS NULL OR length(latest_tag) BETWEEN 6 AND 65),
    latest_url TEXT CHECK (latest_url IS NULL OR length(latest_url) <= 512),
    latest_published_at TEXT,
    latest_prerelease INTEGER CHECK (latest_prerelease IS NULL OR latest_prerelease IN (0, 1)),
    latest_immutable INTEGER CHECK (latest_immutable IS NULL OR latest_immutable = 1),
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 80),
    rate_limit_reset_at TEXT,
    CHECK (
        (latest_version IS NULL AND latest_tag IS NULL AND latest_url IS NULL AND
         latest_published_at IS NULL AND latest_prerelease IS NULL AND latest_immutable IS NULL)
        OR
        (latest_version IS NOT NULL AND latest_tag IS NOT NULL AND latest_url IS NOT NULL AND
         latest_published_at IS NOT NULL AND latest_prerelease IS NOT NULL AND latest_immutable = 1)
    )
) STRICT;

INSERT INTO update_check_state (singleton) VALUES (1);
