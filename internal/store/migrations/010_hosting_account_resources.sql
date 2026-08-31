-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE TABLE hosting_account_resources (
    account_id TEXT PRIMARY KEY NOT NULL
        REFERENCES hosting_account_unix_identities(account_id) ON DELETE RESTRICT,
    desired_cpu_quota_percent INTEGER
        CHECK (desired_cpu_quota_percent IS NULL OR desired_cpu_quota_percent BETWEEN 1 AND 100000),
    desired_cpu_weight INTEGER
        CHECK (desired_cpu_weight IS NULL OR desired_cpu_weight BETWEEN 1 AND 10000),
    desired_memory_bytes INTEGER
        CHECK (desired_memory_bytes IS NULL OR desired_memory_bytes > 0),
    desired_swap_bytes INTEGER
        CHECK (desired_swap_bytes IS NULL OR desired_swap_bytes >= 0),
    desired_process_limit INTEGER
        CHECK (desired_process_limit IS NULL OR desired_process_limit > 0),
    applied_cpu_quota_percent INTEGER
        CHECK (applied_cpu_quota_percent IS NULL OR applied_cpu_quota_percent BETWEEN 1 AND 100000),
    applied_cpu_weight INTEGER
        CHECK (applied_cpu_weight IS NULL OR applied_cpu_weight BETWEEN 1 AND 10000),
    applied_memory_bytes INTEGER
        CHECK (applied_memory_bytes IS NULL OR applied_memory_bytes > 0),
    applied_swap_bytes INTEGER
        CHECK (applied_swap_bytes IS NULL OR applied_swap_bytes >= 0),
    applied_process_limit INTEGER
        CHECK (applied_process_limit IS NULL OR applied_process_limit > 0),
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
    CHECK (
        (status = 'pending'
            AND capability_status = 'pending'
            AND reason_code IS NULL)
        OR (status = 'applied'
            AND capability_status = 'available'
            AND reason_code IS NULL
            AND applied_cpu_quota_percent IS desired_cpu_quota_percent
            AND applied_cpu_weight IS desired_cpu_weight
            AND applied_memory_bytes IS desired_memory_bytes
            AND applied_swap_bytes IS desired_swap_bytes
            AND applied_process_limit IS desired_process_limit
            AND applied_at IS NOT NULL
            AND last_operation_id IS NOT NULL)
        OR (status = 'blocked'
            AND capability_status IN ('unavailable', 'unsupported', 'unknown')
            AND reason_code IS NOT NULL
            AND last_operation_id IS NOT NULL)
    )
) STRICT;

INSERT INTO hosting_account_resources (
    account_id, desired_cpu_quota_percent, desired_cpu_weight,
    desired_memory_bytes, desired_swap_bytes, desired_process_limit,
    revision, status, capability_status, updated_at
)
SELECT
    h.id,
    json_extract(a.effective_limits_json, '$.cpuQuotaPercent'),
    json_extract(a.effective_limits_json, '$.cpuWeight'),
    json_extract(a.effective_limits_json, '$.memoryBytes'),
    json_extract(a.effective_limits_json, '$.swapBytes'),
    json_extract(a.effective_limits_json, '$.processLimit'),
    1,
    'pending',
    'pending',
    h.updated_at
FROM hosting_accounts AS h
JOIN account_package_assignments AS a
  ON a.account_id = h.id AND a.id = h.current_package_assignment_id;

CREATE TRIGGER hosting_account_resource_identity_immutable
BEFORE UPDATE ON hosting_account_resources
FOR EACH ROW
WHEN NEW.account_id <> OLD.account_id
BEGIN
    SELECT RAISE(ABORT, 'hosting account resource identity is immutable');
END;

CREATE TRIGGER hosting_account_resource_no_delete
BEFORE DELETE ON hosting_account_resources
FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'hosting account resource state is retained');
END;
