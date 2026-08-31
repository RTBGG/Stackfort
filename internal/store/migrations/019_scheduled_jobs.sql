-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Scheduled jobs persist only closed intent. Executable paths, systemd unit
-- names, and OnCalendar source are derived by the privileged agent.
CREATE TABLE scheduled_jobs (
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
    name TEXT NOT NULL
        CHECK (length(name) BETWEEN 1 AND 80 AND name = trim(name)),
    runtime TEXT NOT NULL CHECK (runtime IN ('shell', 'php')),
    script_path TEXT NOT NULL
        CHECK (
            length(script_path) BETWEEN 1 AND 255
            AND script_path NOT GLOB '*[^A-Za-z0-9._/-]*'
            AND substr(script_path, 1, 1) <> '/'
            AND substr(script_path, -1, 1) <> '/'
            AND instr(script_path, '//') = 0
            AND instr('/' || script_path || '/', '/../') = 0
            AND instr('/' || script_path || '/', '/./') = 0
        ),
    php_version TEXT,
    schedule_kind TEXT NOT NULL CHECK (schedule_kind IN ('interval', 'hourly', 'daily', 'weekly')),
    interval_minutes INTEGER NOT NULL CHECK (interval_minutes IN (0, 5, 15, 30)),
    hour_utc INTEGER NOT NULL CHECK (hour_utc BETWEEN 0 AND 23),
    minute_utc INTEGER NOT NULL CHECK (minute_utc BETWEEN 0 AND 59),
    weekday TEXT NOT NULL CHECK (weekday IN ('', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun')),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'disabled', 'deleting', 'error', 'deleted')),
    revision INTEGER NOT NULL CHECK (revision > 0),
    applied_revision INTEGER CHECK (applied_revision > 0 AND applied_revision <= revision),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    removed_at TEXT,
    UNIQUE (account_id, id),
    CHECK (
        (runtime = 'shell' AND php_version IS NULL AND substr(script_path, -3) = '.sh')
        OR (runtime = 'php' AND php_version IN ('8.3', '8.4', '8.5') AND substr(script_path, -4) = '.php')
    ),
    CHECK (
        (schedule_kind = 'interval' AND interval_minutes IN (5, 15, 30) AND hour_utc = 0 AND minute_utc = 0 AND weekday = '')
        OR (schedule_kind = 'hourly' AND interval_minutes = 0 AND hour_utc = 0 AND weekday = '')
        OR (schedule_kind = 'daily' AND interval_minutes = 0 AND weekday = '')
        OR (schedule_kind = 'weekly' AND interval_minutes = 0 AND weekday <> '')
    ),
    CHECK (
        (status = 'deleted' AND removed_at IS NOT NULL)
        OR (status <> 'deleted' AND removed_at IS NULL)
    )
) STRICT;

CREATE INDEX scheduled_jobs_account_idx
    ON scheduled_jobs(account_id, created_at, id)
    WHERE removed_at IS NULL;

CREATE TABLE scheduled_job_mutations (
    operation_id TEXT PRIMARY KEY NOT NULL REFERENCES operations(id) ON DELETE RESTRICT,
    account_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('create', 'update', 'delete')),
    desired_revision INTEGER NOT NULL CHECK (desired_revision > 0),
    applied_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (account_id, operation_id),
    FOREIGN KEY (account_id, operation_id)
        REFERENCES operations(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, job_id)
        REFERENCES scheduled_jobs(account_id, id) ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

CREATE UNIQUE INDEX scheduled_job_mutations_pending_job_idx
    ON scheduled_job_mutations(account_id, job_id)
    WHERE applied_at IS NULL;

CREATE TRIGGER scheduled_job_mutations_restrict_update
BEFORE UPDATE ON scheduled_job_mutations
WHEN OLD.applied_at IS NOT NULL
    OR NEW.operation_id IS NOT OLD.operation_id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.job_id IS NOT OLD.job_id
    OR NEW.action IS NOT OLD.action
    OR NEW.desired_revision IS NOT OLD.desired_revision
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.applied_at IS NULL
BEGIN
    SELECT RAISE(ABORT, 'scheduled job mutation transition is invalid');
END;

CREATE TRIGGER scheduled_job_mutations_no_delete
BEFORE DELETE ON scheduled_job_mutations
BEGIN
    SELECT RAISE(ABORT, 'scheduled job mutations are retained');
END;
