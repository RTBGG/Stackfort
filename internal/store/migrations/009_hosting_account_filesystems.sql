-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE UNIQUE INDEX hosting_account_unix_identity_account_uid
ON hosting_account_unix_identities (account_id, uid);

CREATE TABLE hosting_account_filesystems (
    account_id TEXT PRIMARY KEY NOT NULL,
    project_id INTEGER NOT NULL UNIQUE
        CHECK (project_id BETWEEN 200000 AND 599999),
    desired_storage_bytes INTEGER
        CHECK (desired_storage_bytes IS NULL OR desired_storage_bytes > 0),
    desired_storage_inodes INTEGER
        CHECK (desired_storage_inodes IS NULL OR desired_storage_inodes > 0),
    applied_storage_bytes INTEGER
        CHECK (applied_storage_bytes IS NULL OR applied_storage_bytes > 0),
    applied_storage_inodes INTEGER
        CHECK (applied_storage_inodes IS NULL OR applied_storage_inodes > 0),
    revision INTEGER NOT NULL CHECK (revision > 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'applied', 'blocked')),
    capability_status TEXT NOT NULL
        CHECK (capability_status IN ('pending', 'available', 'unavailable', 'unsupported', 'unknown')),
    reason_code TEXT CHECK (
        reason_code IS NULL OR (
            length(reason_code) BETWEEN 1 AND 64
            AND reason_code NOT GLOB '*[^a-z0-9-]*'
        )
    ),
    updated_at TEXT NOT NULL,
    applied_at TEXT,
    last_operation_id TEXT REFERENCES operations(id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, project_id)
        REFERENCES hosting_account_unix_identities(account_id, uid) ON DELETE RESTRICT,
    CHECK (
        (status = 'pending'
            AND capability_status = 'pending'
            AND reason_code IS NULL)
        OR (status = 'applied'
            AND capability_status = 'available'
            AND reason_code IS NULL
            AND applied_storage_bytes IS desired_storage_bytes
            AND applied_storage_inodes IS desired_storage_inodes
            AND applied_at IS NOT NULL
            AND last_operation_id IS NOT NULL)
        OR (status = 'blocked'
            AND capability_status IN ('unavailable', 'unsupported', 'unknown')
            AND reason_code IS NOT NULL
            AND last_operation_id IS NOT NULL)
    )
) STRICT;

INSERT INTO hosting_account_filesystems (
    account_id, project_id, desired_storage_bytes, desired_storage_inodes,
    revision, status, capability_status, updated_at
)
SELECT
    u.account_id,
    u.uid,
    json_extract(a.effective_limits_json, '$.storageBytes'),
    json_extract(a.effective_limits_json, '$.storageInodes'),
    1,
    'pending',
    'pending',
    h.updated_at
FROM hosting_account_unix_identities AS u
JOIN hosting_accounts AS h ON h.id = u.account_id
JOIN account_package_assignments AS a
  ON a.account_id = h.id AND a.id = h.current_package_assignment_id;

CREATE TRIGGER hosting_account_filesystem_identity_immutable
BEFORE UPDATE ON hosting_account_filesystems
FOR EACH ROW
WHEN NEW.account_id <> OLD.account_id OR NEW.project_id <> OLD.project_id
BEGIN
    SELECT RAISE(ABORT, 'hosting account filesystem identity is immutable');
END;

CREATE TRIGGER hosting_account_filesystem_no_delete
BEFORE DELETE ON hosting_account_filesystems
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'hosting account filesystem state is retained');
END;
