-- SPDX-License-Identifier: AGPL-3.0-or-later

-- OCI application records store only Stackfort's closed intent. There are no
-- host ports, host mounts, namespace modes, capabilities, devices, commands,
-- or engine-socket fields for a caller to populate.
CREATE TABLE oci_applications (
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
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 80 AND name = trim(name)),
    slug TEXT NOT NULL
        CHECK (
            length(slug) BETWEEN 1 AND 63
            AND slug = lower(slug)
            AND substr(slug, 1, 1) GLOB '[a-z]'
            AND substr(slug, -1, 1) <> '-'
            AND slug NOT GLOB '*[^a-z0-9-]*'
        ),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('image_digest', 'containerfile')),
    image_reference TEXT,
    build_context TEXT,
    containerfile_path TEXT,
    internal_port INTEGER NOT NULL CHECK (internal_port BETWEEN 1 AND 65535),
    health_kind TEXT NOT NULL CHECK (health_kind IN ('http', 'tcp')),
    health_path TEXT,
    health_interval_seconds INTEGER NOT NULL CHECK (health_interval_seconds IN (10, 30, 60)),
    health_timeout_seconds INTEGER NOT NULL
        CHECK (health_timeout_seconds BETWEEN 1 AND 10 AND health_timeout_seconds < health_interval_seconds),
    health_retries INTEGER NOT NULL CHECK (health_retries BETWEEN 1 AND 10),
    status TEXT NOT NULL CHECK (status IN ('draft', 'pending', 'active', 'suspended', 'error', 'deleting', 'deleted')),
    revision INTEGER NOT NULL CHECK (revision > 0),
    applied_revision INTEGER CHECK (applied_revision > 0 AND applied_revision <= revision),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    UNIQUE (account_id, id),
    CHECK (
        (source_kind = 'image_digest'
            AND image_reference IS NOT NULL
            AND length(image_reference) BETWEEN 1 AND 255
            AND image_reference = lower(image_reference)
            AND instr(image_reference, '@sha256:') > 1
            AND length(substr(image_reference, instr(image_reference, '@sha256:') + 8)) = 64
            AND build_context IS NULL AND containerfile_path IS NULL)
        OR
        (source_kind = 'containerfile'
            AND image_reference IS NULL
            AND build_context IS NOT NULL AND length(build_context) BETWEEN 1 AND 255
            AND containerfile_path IS NOT NULL AND length(containerfile_path) BETWEEN 1 AND 255)
    ),
    CHECK (
        (health_kind = 'http' AND health_path IS NOT NULL
            AND length(health_path) BETWEEN 1 AND 200 AND substr(health_path, 1, 1) = '/')
        OR (health_kind = 'tcp' AND health_path IS NULL)
    ),
    CHECK (
        (status = 'deleted' AND removed_at IS NOT NULL)
        OR (status <> 'deleted' AND removed_at IS NULL)
    )
) STRICT;

CREATE UNIQUE INDEX oci_applications_live_slug_idx
    ON oci_applications(account_id, slug)
    WHERE removed_at IS NULL;

CREATE INDEX oci_applications_account_idx
    ON oci_applications(account_id, created_at, id)
    WHERE removed_at IS NULL;

CREATE TRIGGER oci_applications_no_delete
BEFORE DELETE ON oci_applications
BEGIN
    SELECT RAISE(ABORT, 'OCI applications are removed logically and retained');
END;

CREATE TRIGGER oci_applications_identity_immutable
BEFORE UPDATE ON oci_applications
WHEN NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.created_at IS NOT OLD.created_at
    OR OLD.removed_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'OCI application identity or terminal state is immutable');
END;

-- Existing domain_targets cannot gain a composite foreign key without a table
-- rebuild. The trigger provides the same account-parent and live-state check.
CREATE TRIGGER domain_targets_require_active_oci_application
BEFORE INSERT ON domain_targets
WHEN NEW.target_type = 'oci_application'
    AND NOT EXISTS (
        SELECT 1 FROM oci_applications AS application
        WHERE application.account_id = NEW.account_id
          AND application.id = NEW.application_id
          AND application.status = 'active'
          AND application.applied_revision = application.revision
          AND application.removed_at IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'OCI domain target requires an active account-owned application');
END;

CREATE TRIGGER oci_applications_restrict_referenced_removal
BEFORE UPDATE OF status, removed_at ON oci_applications
WHEN NEW.status = 'deleted'
    AND EXISTS (
        SELECT 1 FROM domain_targets AS target
        WHERE target.account_id = OLD.account_id
          AND target.application_id = OLD.id
          AND target.superseded_at IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'referenced OCI application cannot be removed');
END;
