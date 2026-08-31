-- SPDX-License-Identifier: AGPL-3.0-or-later

-- A durable operation may be replayed after the API loses its lease or
-- restarts after the host mutation completed. Bind that operation to exactly
-- one immutable applied-state revision so replay cannot create false history.
CREATE UNIQUE INDEX applied_state_revisions_account_operation_idx
    ON applied_state_revisions(account_id, operation_id)
    WHERE operation_id IS NOT NULL;
