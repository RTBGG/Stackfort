-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Environment values remain envelope-encrypted in control-plane state. Only
-- metadata references are attached to an application revision.
CREATE TABLE oci_environment_secrets (
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
    generation INTEGER NOT NULL CHECK (generation > 0),
    value_ciphertext BLOB NOT NULL,
    value_nonce BLOB NOT NULL,
    value_wrapped_key BLOB NOT NULL,
    value_wrap_nonce BLOB NOT NULL,
    value_key_version INTEGER NOT NULL CHECK (value_key_version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    UNIQUE (account_id, id),
    CHECK (
        (removed_at IS NULL
            AND length(value_ciphertext) BETWEEN 17 AND 32784
            AND length(value_nonce) = 12
            AND length(value_wrapped_key) = 48
            AND length(value_wrap_nonce) = 12)
        OR
        (removed_at IS NOT NULL
            AND length(value_ciphertext) = 0
            AND length(value_nonce) = 12
            AND length(value_wrapped_key) = 48
            AND length(value_wrap_nonce) = 12)
    )
) STRICT;

CREATE UNIQUE INDEX oci_environment_secrets_live_slug_idx
    ON oci_environment_secrets(account_id, slug)
    WHERE removed_at IS NULL;

CREATE INDEX oci_environment_secrets_account_idx
    ON oci_environment_secrets(account_id, created_at, id)
    WHERE removed_at IS NULL;

CREATE TRIGGER oci_environment_secrets_no_delete
BEFORE DELETE ON oci_environment_secrets
BEGIN
    SELECT RAISE(ABORT, 'OCI environment values are removed logically and retained');
END;

CREATE TRIGGER oci_environment_secrets_identity_immutable
BEFORE UPDATE ON oci_environment_secrets
WHEN NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.created_at IS NOT OLD.created_at
    OR OLD.removed_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'OCI environment value identity or terminal state is immutable');
END;

-- Volumes have metadata only. Their host path is derived from account identity
-- plus this UUID and can never be supplied by a caller.
CREATE TABLE oci_volumes (
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
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    UNIQUE (account_id, id)
) STRICT;

CREATE UNIQUE INDEX oci_volumes_live_slug_idx
    ON oci_volumes(account_id, slug)
    WHERE removed_at IS NULL;

CREATE INDEX oci_volumes_account_idx
    ON oci_volumes(account_id, created_at, id)
    WHERE removed_at IS NULL;

CREATE TRIGGER oci_volumes_no_delete
BEFORE DELETE ON oci_volumes
BEGIN
    SELECT RAISE(ABORT, 'OCI volumes are removed logically and retained');
END;

CREATE TRIGGER oci_volumes_identity_immutable
BEFORE UPDATE ON oci_volumes
WHEN NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.created_at IS NOT OLD.created_at
    OR OLD.removed_at IS NOT NULL
BEGIN
    SELECT RAISE(ABORT, 'OCI volume identity or terminal state is immutable');
END;

CREATE TABLE oci_application_secret_references (
    application_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    environment_name TEXT NOT NULL
        CHECK (
            length(environment_name) BETWEEN 1 AND 64
            AND environment_name NOT GLOB '*[^A-Z0-9_]*'
            AND substr(environment_name, 1, 1) NOT GLOB '[0-9]'
        ),
    secret_id TEXT NOT NULL,
    PRIMARY KEY (application_id, environment_name),
    UNIQUE (application_id, secret_id),
    FOREIGN KEY (account_id, application_id)
        REFERENCES oci_applications(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, secret_id)
        REFERENCES oci_environment_secrets(account_id, id) ON DELETE RESTRICT
) STRICT;

CREATE TRIGGER oci_application_secret_references_closed_insert
BEFORE INSERT ON oci_application_secret_references
WHEN NOT EXISTS (
        SELECT 1 FROM oci_applications AS application
        WHERE application.account_id = NEW.account_id
          AND application.id = NEW.application_id
          AND application.status = 'draft'
          AND application.removed_at IS NULL
    )
    OR NOT EXISTS (
        SELECT 1 FROM oci_environment_secrets AS value
        WHERE value.account_id = NEW.account_id
          AND value.id = NEW.secret_id
          AND value.removed_at IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'OCI environment reference requires live same-account draft state');
END;

CREATE TRIGGER oci_application_secret_references_closed_update
BEFORE UPDATE ON oci_application_secret_references
BEGIN
    SELECT RAISE(ABORT, 'OCI environment references are replaced atomically');
END;

CREATE TRIGGER oci_application_secret_references_closed_delete
BEFORE DELETE ON oci_application_secret_references
WHEN NOT EXISTS (
    SELECT 1 FROM oci_applications AS application
    WHERE application.account_id = OLD.account_id
      AND application.id = OLD.application_id
      AND application.status = 'draft'
      AND application.removed_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'only OCI draft environment references may be removed');
END;

CREATE TRIGGER oci_environment_secrets_restrict_referenced_removal
BEFORE UPDATE OF removed_at ON oci_environment_secrets
WHEN NEW.removed_at IS NOT NULL
    AND EXISTS (
        SELECT 1 FROM oci_application_secret_references AS reference
        WHERE reference.account_id = OLD.account_id AND reference.secret_id = OLD.id
    )
BEGIN
    SELECT RAISE(ABORT, 'referenced OCI environment value cannot be removed');
END;

CREATE TABLE oci_application_volume_mounts (
    application_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    container_path TEXT NOT NULL
        CHECK (
            length(container_path) BETWEEN 2 AND 255
            AND substr(container_path, 1, 1) = '/'
            AND instr(container_path, '//') = 0
            AND instr(container_path, '\\') = 0
        ),
    volume_id TEXT NOT NULL,
    read_only INTEGER NOT NULL CHECK (read_only IN (0, 1)),
    PRIMARY KEY (application_id, container_path),
    UNIQUE (application_id, volume_id),
    FOREIGN KEY (account_id, application_id)
        REFERENCES oci_applications(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, volume_id)
        REFERENCES oci_volumes(account_id, id) ON DELETE RESTRICT
) STRICT;

CREATE TRIGGER oci_application_volume_mounts_closed_insert
BEFORE INSERT ON oci_application_volume_mounts
WHEN NOT EXISTS (
        SELECT 1 FROM oci_applications AS application
        WHERE application.account_id = NEW.account_id
          AND application.id = NEW.application_id
          AND application.status = 'draft'
          AND application.removed_at IS NULL
    )
    OR NOT EXISTS (
        SELECT 1 FROM oci_volumes AS volume
        WHERE volume.account_id = NEW.account_id
          AND volume.id = NEW.volume_id
          AND volume.removed_at IS NULL
    )
BEGIN
    SELECT RAISE(ABORT, 'OCI volume mount requires live same-account draft state');
END;

CREATE TRIGGER oci_application_volume_mounts_closed_update
BEFORE UPDATE ON oci_application_volume_mounts
BEGIN
    SELECT RAISE(ABORT, 'OCI volume mounts are replaced atomically');
END;

CREATE TRIGGER oci_application_volume_mounts_closed_delete
BEFORE DELETE ON oci_application_volume_mounts
WHEN NOT EXISTS (
    SELECT 1 FROM oci_applications AS application
    WHERE application.account_id = OLD.account_id
      AND application.id = OLD.application_id
      AND application.status = 'draft'
      AND application.removed_at IS NULL
)
BEGIN
    SELECT RAISE(ABORT, 'only OCI draft volume mounts may be removed');
END;

CREATE TRIGGER oci_volumes_restrict_referenced_removal
BEFORE UPDATE OF removed_at ON oci_volumes
WHEN NEW.removed_at IS NOT NULL
    AND EXISTS (
        SELECT 1 FROM oci_application_volume_mounts AS mount
        WHERE mount.account_id = OLD.account_id AND mount.volume_id = OLD.id
    )
BEGIN
    SELECT RAISE(ABORT, 'referenced OCI volume cannot be removed');
END;

-- Reconciliation evidence is append-only and bound to the exact image-approved
-- application revision. It records metadata only; environment plaintext and
-- derived host paths are deliberately absent.
CREATE TABLE oci_resource_artifacts (
    application_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    application_revision INTEGER NOT NULL CHECK (application_revision > 0),
    resource_digest TEXT NOT NULL
        CHECK (length(resource_digest) = 71 AND substr(resource_digest, 1, 7) = 'sha256:'
            AND substr(resource_digest, 8) NOT GLOB '*[^0-9a-f]*'),
    policy_version TEXT NOT NULL CHECK (policy_version = 'stackfort-oci-resources-v1'),
    network_name TEXT NOT NULL CHECK (network_name = 'stackfort-private'),
    secret_count INTEGER NOT NULL CHECK (secret_count BETWEEN 0 AND 32),
    volume_count INTEGER NOT NULL CHECK (volume_count BETWEEN 0 AND 16),
    prepared_at TEXT NOT NULL,
    prepared_by_identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    PRIMARY KEY (application_id, application_revision, resource_digest),
    FOREIGN KEY (account_id, application_id)
        REFERENCES oci_applications(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (application_id, application_revision)
        REFERENCES oci_image_artifacts(application_id, application_revision) ON DELETE RESTRICT
) STRICT;

CREATE INDEX oci_resource_artifacts_account_idx
    ON oci_resource_artifacts(account_id, prepared_at, application_id);

CREATE TRIGGER oci_resource_artifacts_no_update
BEFORE UPDATE ON oci_resource_artifacts
BEGIN
    SELECT RAISE(ABORT, 'OCI private-resource evidence is immutable');
END;

CREATE TRIGGER oci_resource_artifacts_no_delete
BEFORE DELETE ON oci_resource_artifacts
BEGIN
    SELECT RAISE(ABORT, 'OCI private-resource evidence is retained');
END;
