-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE TABLE hosting_account_unix_identities (
    account_id TEXT PRIMARY KEY NOT NULL
        REFERENCES hosting_accounts(id) ON DELETE RESTRICT,
    username TEXT NOT NULL UNIQUE
        CHECK (
            length(username) = 29
            AND substr(username, 1, 3) = 'sf_'
            AND substr(username, 4) = lower(substr(username, 4))
            AND substr(username, 4) NOT GLOB '*[^0-9a-f]*'
        ),
    uid INTEGER NOT NULL UNIQUE CHECK (uid BETWEEN 200000 AND 599999),
    gid INTEGER NOT NULL UNIQUE CHECK (gid = uid),
    home_directory TEXT NOT NULL UNIQUE
        CHECK (home_directory = '/srv/hosting/accounts/' || account_id),
    lifecycle_state TEXT NOT NULL
        CHECK (lifecycle_state IN (
            'allocated', 'reconciled', 'archive_requested', 'archived',
            'deletion_requested', 'deleted'
        )),
    allocated_at TEXT NOT NULL,
    reconciled_at TEXT,
    archive_requested_at TEXT,
    archived_at TEXT,
    archive_reference TEXT CHECK (
        archive_reference IS NULL OR length(archive_reference) BETWEEN 1 AND 512
    ),
    deletion_requested_at TEXT,
    deleted_at TEXT,
    CHECK (
        (lifecycle_state = 'allocated'
            AND reconciled_at IS NULL
            AND archive_requested_at IS NULL
            AND archived_at IS NULL
            AND archive_reference IS NULL
            AND deletion_requested_at IS NULL
            AND deleted_at IS NULL)
        OR (lifecycle_state = 'reconciled'
            AND reconciled_at IS NOT NULL
            AND archive_requested_at IS NULL
            AND archived_at IS NULL
            AND archive_reference IS NULL
            AND deletion_requested_at IS NULL
            AND deleted_at IS NULL)
        OR (lifecycle_state = 'archive_requested'
            AND archive_requested_at IS NOT NULL
            AND archived_at IS NULL
            AND archive_reference IS NULL
            AND deletion_requested_at IS NULL
            AND deleted_at IS NULL)
        OR (lifecycle_state = 'archived'
            AND archive_requested_at IS NOT NULL
            AND archived_at IS NOT NULL
            AND archive_reference IS NOT NULL
            AND deletion_requested_at IS NULL
            AND deleted_at IS NULL)
        OR (lifecycle_state = 'deletion_requested'
            AND archive_requested_at IS NOT NULL
            AND archived_at IS NOT NULL
            AND archive_reference IS NOT NULL
            AND deletion_requested_at IS NOT NULL
            AND deleted_at IS NULL)
        OR (lifecycle_state = 'deleted'
            AND archive_requested_at IS NOT NULL
            AND archived_at IS NOT NULL
            AND archive_reference IS NOT NULL
            AND deletion_requested_at IS NOT NULL
            AND deleted_at IS NOT NULL)
    )
) STRICT;

INSERT INTO hosting_account_unix_identities (
    account_id, username, uid, gid, home_directory, lifecycle_state, allocated_at
)
SELECT
    id,
    'sf_' || substr(replace(id, '-', ''), -26),
    199999 + row_number() OVER (ORDER BY id),
    199999 + row_number() OVER (ORDER BY id),
    '/srv/hosting/accounts/' || id,
    'allocated',
    created_at
FROM hosting_accounts;

CREATE TRIGGER hosting_account_unix_identity_immutable
BEFORE UPDATE ON hosting_account_unix_identities
FOR EACH ROW
WHEN NEW.account_id <> OLD.account_id
    OR NEW.username <> OLD.username
    OR NEW.uid <> OLD.uid
    OR NEW.gid <> OLD.gid
    OR NEW.home_directory <> OLD.home_directory
    OR NEW.allocated_at <> OLD.allocated_at
BEGIN
    SELECT RAISE(ABORT, 'hosting account Unix identity is immutable');
END;

CREATE TRIGGER hosting_account_unix_identity_lifecycle
BEFORE UPDATE OF lifecycle_state ON hosting_account_unix_identities
FOR EACH ROW
WHEN NEW.lifecycle_state <> OLD.lifecycle_state
    AND NOT (
        (OLD.lifecycle_state = 'allocated' AND NEW.lifecycle_state IN ('reconciled', 'archive_requested'))
        OR (OLD.lifecycle_state = 'reconciled' AND NEW.lifecycle_state = 'archive_requested')
        OR (OLD.lifecycle_state = 'archive_requested' AND NEW.lifecycle_state = 'archived')
        OR (OLD.lifecycle_state = 'archived' AND NEW.lifecycle_state = 'deletion_requested')
        OR (OLD.lifecycle_state = 'deletion_requested' AND NEW.lifecycle_state = 'deleted')
    )
BEGIN
    SELECT RAISE(ABORT, 'invalid hosting account Unix identity lifecycle transition');
END;

CREATE TRIGGER hosting_account_unix_identity_history_immutable
BEFORE UPDATE ON hosting_account_unix_identities
FOR EACH ROW
WHEN (OLD.reconciled_at IS NOT NULL AND NEW.reconciled_at IS NOT OLD.reconciled_at)
    OR (OLD.archive_requested_at IS NOT NULL AND NEW.archive_requested_at IS NOT OLD.archive_requested_at)
    OR (OLD.archived_at IS NOT NULL AND NEW.archived_at IS NOT OLD.archived_at)
    OR (OLD.archive_reference IS NOT NULL AND NEW.archive_reference IS NOT OLD.archive_reference)
    OR (OLD.deletion_requested_at IS NOT NULL AND NEW.deletion_requested_at IS NOT OLD.deletion_requested_at)
    OR (OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS NOT OLD.deleted_at)
BEGIN
    SELECT RAISE(ABORT, 'hosting account Unix identity lifecycle history is immutable');
END;

CREATE TRIGGER hosting_account_unix_identity_no_delete
BEFORE DELETE ON hosting_account_unix_identities
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'hosting account Unix identity tombstones are retained');
END;
