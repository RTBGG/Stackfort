-- SPDX-License-Identifier: AGPL-3.0-or-later

ALTER TABLE operations ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 1
    CHECK (max_attempts BETWEEN 1 AND 100);
ALTER TABLE operations ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0
    CHECK (attempt_count BETWEEN 0 AND 100 AND attempt_count <= max_attempts);
ALTER TABLE operations ADD COLUMN next_attempt_at TEXT;
ALTER TABLE operations ADD COLUMN current_attempt_id TEXT;
ALTER TABLE operations ADD COLUMN worker_instance_id TEXT;
ALTER TABLE operations ADD COLUMN lease_expires_at TEXT;
ALTER TABLE operations ADD COLUMN cancellation_requested_at TEXT;
ALTER TABLE operations ADD COLUMN cancellation_requested_by_identity_id TEXT
    REFERENCES identities(id) ON DELETE RESTRICT;

UPDATE operations
SET max_attempts = CASE WHEN retry_class = 'none' THEN 1 ELSE 3 END,
    next_attempt_at = CASE WHEN status = 'pending' THEN created_at ELSE NULL END;

CREATE TABLE operation_attempts (
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
    operation_id TEXT NOT NULL REFERENCES operations(id) ON DELETE RESTRICT,
    attempt_number INTEGER NOT NULL CHECK (attempt_number BETWEEN 1 AND 100),
    worker_instance_id TEXT NOT NULL
        CHECK (
            length(worker_instance_id) = 36
            AND worker_instance_id = lower(worker_instance_id)
            AND worker_instance_id NOT GLOB '*[^0-9a-f-]*'
            AND length(replace(worker_instance_id, '-', '')) = 32
            AND substr(worker_instance_id, 9, 1) = '-'
            AND substr(worker_instance_id, 14, 1) = '-'
            AND substr(worker_instance_id, 15, 1) = '7'
            AND substr(worker_instance_id, 19, 1) = '-'
            AND substr(worker_instance_id, 20, 1) IN ('8', '9', 'a', 'b')
            AND substr(worker_instance_id, 24, 1) = '-'
        ),
    claimed_at TEXT NOT NULL,
    heartbeat_at TEXT NOT NULL,
    lease_expires_at TEXT NOT NULL,
    completed_at TEXT,
    outcome TEXT NOT NULL
        CHECK (outcome IN ('running', 'succeeded', 'failed', 'cancelled', 'lease_expired')),
    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 80),
    UNIQUE (operation_id, id),
    UNIQUE (operation_id, attempt_number),
    CHECK (
        (outcome = 'running' AND completed_at IS NULL AND error_code IS NULL)
        OR (outcome = 'succeeded' AND completed_at IS NOT NULL AND error_code IS NULL)
        OR (outcome = 'cancelled' AND completed_at IS NOT NULL AND error_code IS NULL)
        OR (outcome IN ('failed', 'lease_expired') AND completed_at IS NOT NULL AND error_code IS NOT NULL)
    )
) STRICT;

CREATE INDEX operation_attempts_operation_idx
    ON operation_attempts(operation_id, attempt_number DESC);

CREATE TABLE operation_events (
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
    operation_id TEXT NOT NULL REFERENCES operations(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    attempt_id TEXT,
    event_type TEXT NOT NULL
        CHECK (event_type IN (
            'created', 'claimed', 'progress', 'retry_scheduled',
            'cancellation_requested', 'succeeded', 'failed',
            'cancelled', 'lease_expired'
        )),
    stage TEXT NOT NULL CHECK (length(stage) BETWEEN 1 AND 80),
    progress_percent INTEGER NOT NULL CHECK (progress_percent BETWEEN 0 AND 100),
    message_code TEXT NOT NULL CHECK (length(message_code) BETWEEN 1 AND 80),
    details_json TEXT NOT NULL
        CHECK (
            length(details_json) <= 16384
            AND json_valid(details_json)
            AND json_type(details_json) = 'object'
        ),
    occurred_at TEXT NOT NULL,
    UNIQUE (operation_id, sequence),
    FOREIGN KEY (operation_id, attempt_id)
        REFERENCES operation_attempts(operation_id, id)
        ON DELETE RESTRICT
) STRICT;

CREATE INDEX operation_events_operation_idx
    ON operation_events(operation_id, sequence);

-- B-002 could create pending base operations before the durable worker schema
-- existed. Preserve them and give each one a deterministic initial event.
INSERT INTO operation_events (
    id, operation_id, sequence, event_type, stage, progress_percent,
    message_code, details_json, occurred_at
)
SELECT
    id, id, 1, 'created', stage, progress_percent,
    'operation.created', '{}', created_at
FROM operations;

CREATE INDEX operations_queue_idx
    ON operations(status, next_attempt_at, created_at)
    WHERE status = 'pending';

CREATE INDEX operations_expired_lease_idx
    ON operations(lease_expires_at)
    WHERE status IN ('running', 'cancelling');

CREATE TRIGGER operations_restrict_update
BEFORE UPDATE ON operations
WHEN
    NEW.id IS NOT OLD.id
    OR NEW.account_id IS NOT OLD.account_id
    OR NEW.actor_identity_id IS NOT OLD.actor_identity_id
    OR NEW.kind IS NOT OLD.kind
    OR NEW.retry_class IS NOT OLD.retry_class
    OR NEW.request_id IS NOT OLD.request_id
    OR NEW.idempotency_key IS NOT OLD.idempotency_key
    OR NEW.payload_json IS NOT OLD.payload_json
    OR NEW.created_at IS NOT OLD.created_at
    OR NEW.max_attempts IS NOT OLD.max_attempts
    OR NEW.progress_percent < OLD.progress_percent
    OR NEW.attempt_count < OLD.attempt_count
    OR NEW.attempt_count > NEW.max_attempts
    OR OLD.status IN ('succeeded', 'cancelled')
    OR NOT (
        (OLD.status = 'pending' AND NEW.status IN ('running', 'cancelled'))
        OR (OLD.status = 'running' AND NEW.status IN ('running', 'pending', 'cancelling', 'succeeded', 'failed'))
        OR (OLD.status = 'cancelling' AND NEW.status IN ('cancelling', 'cancelled', 'failed'))
        OR (OLD.status = 'failed' AND NEW.status = 'pending')
    )
    OR NOT (
        (
            NEW.status = 'pending'
            AND NEW.current_attempt_id IS NULL
            AND NEW.worker_instance_id IS NULL
            AND NEW.lease_expires_at IS NULL
            AND NEW.next_attempt_at IS NOT NULL
            AND NEW.result_json IS NULL
            AND NEW.error_code IS NULL
            AND NEW.completed_at IS NULL
        )
        OR (
            NEW.status IN ('running', 'cancelling')
            AND NEW.current_attempt_id IS NOT NULL
            AND NEW.worker_instance_id IS NOT NULL
            AND NEW.lease_expires_at IS NOT NULL
            AND NEW.next_attempt_at IS NULL
            AND NEW.result_json IS NULL
            AND NEW.error_code IS NULL
            AND NEW.completed_at IS NULL
        )
        OR (
            NEW.status = 'succeeded'
            AND NEW.current_attempt_id IS NULL
            AND NEW.worker_instance_id IS NULL
            AND NEW.lease_expires_at IS NULL
            AND NEW.next_attempt_at IS NULL
            AND NEW.error_code IS NULL
            AND NEW.completed_at IS NOT NULL
            AND NEW.progress_percent = 100
        )
        OR (
            NEW.status = 'failed'
            AND NEW.current_attempt_id IS NULL
            AND NEW.worker_instance_id IS NULL
            AND NEW.lease_expires_at IS NULL
            AND NEW.next_attempt_at IS NULL
            AND NEW.error_code IS NOT NULL
            AND NEW.completed_at IS NOT NULL
        )
        OR (
            NEW.status = 'cancelled'
            AND NEW.current_attempt_id IS NULL
            AND NEW.worker_instance_id IS NULL
            AND NEW.lease_expires_at IS NULL
            AND NEW.next_attempt_at IS NULL
            AND NEW.result_json IS NULL
            AND NEW.error_code IS NULL
            AND NEW.completed_at IS NOT NULL
        )
    )
BEGIN
    SELECT RAISE(ABORT, 'invalid operation mutation or state transition');
END;

CREATE TRIGGER operations_validate_current_attempt
BEFORE UPDATE ON operations
WHEN
    NEW.current_attempt_id IS NOT NULL
    AND NOT EXISTS (
        SELECT 1
        FROM operation_attempts
        WHERE id = NEW.current_attempt_id
          AND operation_id = NEW.id
          AND worker_instance_id = NEW.worker_instance_id
          AND outcome = 'running'
    )
BEGIN
    SELECT RAISE(ABORT, 'current attempt must be running and belong to the worker and operation');
END;

CREATE TRIGGER operations_validate_released_attempt
BEFORE UPDATE ON operations
WHEN
    OLD.current_attempt_id IS NOT NULL
    AND NEW.current_attempt_id IS NULL
    AND EXISTS (
        SELECT 1
        FROM operation_attempts
        WHERE id = OLD.current_attempt_id
          AND operation_id = OLD.id
          AND outcome = 'running'
    )
BEGIN
    SELECT RAISE(ABORT, 'current attempt must be terminal before its lease is released');
END;

CREATE TRIGGER operation_attempts_validate_insert
BEFORE INSERT ON operation_attempts
WHEN NOT EXISTS (
    SELECT 1
    FROM operations
    WHERE id = NEW.operation_id
      AND status = 'pending'
      AND NEW.attempt_number = attempt_count + 1
      AND NEW.attempt_number <= max_attempts
)
BEGIN
    SELECT RAISE(ABORT, 'attempt must be the next bounded claim of a pending operation');
END;

CREATE TRIGGER operation_attempts_restrict_update
BEFORE UPDATE ON operation_attempts
WHEN
    OLD.outcome <> 'running'
    OR NEW.id IS NOT OLD.id
    OR NEW.operation_id IS NOT OLD.operation_id
    OR NEW.attempt_number IS NOT OLD.attempt_number
    OR NEW.worker_instance_id IS NOT OLD.worker_instance_id
    OR NEW.claimed_at IS NOT OLD.claimed_at
    OR NOT (
        (
            NEW.outcome = 'running'
            AND NEW.completed_at IS NULL
            AND NEW.error_code IS NULL
            AND NEW.heartbeat_at >= OLD.heartbeat_at
            AND NEW.lease_expires_at >= OLD.lease_expires_at
        )
        OR (
            NEW.outcome IN ('succeeded', 'cancelled')
            AND NEW.completed_at IS NOT NULL
            AND NEW.error_code IS NULL
            AND NEW.heartbeat_at IS OLD.heartbeat_at
            AND NEW.lease_expires_at IS OLD.lease_expires_at
        )
        OR (
            NEW.outcome IN ('failed', 'lease_expired')
            AND NEW.completed_at IS NOT NULL
            AND NEW.error_code IS NOT NULL
            AND NEW.heartbeat_at IS OLD.heartbeat_at
            AND NEW.lease_expires_at IS OLD.lease_expires_at
        )
    )
BEGIN
    SELECT RAISE(ABORT, 'invalid operation attempt mutation');
END;

CREATE TRIGGER operation_attempts_no_delete
BEFORE DELETE ON operation_attempts
BEGIN
    SELECT RAISE(ABORT, 'operation attempts are retained for history');
END;

CREATE TRIGGER operations_no_delete
BEFORE DELETE ON operations
BEGIN
    SELECT RAISE(ABORT, 'operations are retained for history');
END;

CREATE TRIGGER operation_events_no_update
BEFORE UPDATE ON operation_events
BEGIN
    SELECT RAISE(ABORT, 'operation events are append-only');
END;

CREATE TRIGGER operation_events_no_delete
BEFORE DELETE ON operation_events
BEGIN
    SELECT RAISE(ABORT, 'operation events are append-only');
END;
