-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Domain mutations and desired-state snapshots are external-operation saga
-- steps. These immutable correlations make replay after a worker/API restart
-- distinguish an already committed step from work that still has to run.
CREATE TABLE domain_lifecycle_mutations (
    operation_id TEXT PRIMARY KEY NOT NULL,
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('create', 'edit', 'suspend', 'resume', 'remove')),
    applied_at TEXT NOT NULL,
    UNIQUE (account_id, operation_id),
    FOREIGN KEY (account_id, operation_id)
        REFERENCES operations(account_id, id)
        ON DELETE RESTRICT,
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id)
        ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

CREATE INDEX domain_lifecycle_mutations_domain_idx
    ON domain_lifecycle_mutations(account_id, domain_id, applied_at);

CREATE TRIGGER domain_lifecycle_mutations_no_update
BEFORE UPDATE ON domain_lifecycle_mutations
BEGIN
    SELECT RAISE(ABORT, 'domain lifecycle mutation correlations are immutable');
END;

CREATE TRIGGER domain_lifecycle_mutations_no_delete
BEFORE DELETE ON domain_lifecycle_mutations
BEGIN
    SELECT RAISE(ABORT, 'domain lifecycle mutation correlations are retained');
END;

CREATE TABLE operation_desired_state_revisions (
    operation_id TEXT PRIMARY KEY NOT NULL,
    account_id TEXT NOT NULL,
    desired_state_revision_id TEXT NOT NULL,
    linked_at TEXT NOT NULL,
    UNIQUE (account_id, operation_id),
    UNIQUE (account_id, desired_state_revision_id),
    FOREIGN KEY (account_id, operation_id)
        REFERENCES operations(account_id, id)
        ON DELETE RESTRICT,
    FOREIGN KEY (account_id, desired_state_revision_id)
        REFERENCES desired_state_revisions(account_id, id)
        ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

CREATE TRIGGER operation_desired_state_revisions_no_update
BEFORE UPDATE ON operation_desired_state_revisions
BEGIN
    SELECT RAISE(ABORT, 'operation desired-state correlations are immutable');
END;

CREATE TRIGGER operation_desired_state_revisions_no_delete
BEFORE DELETE ON operation_desired_state_revisions
BEGIN
    SELECT RAISE(ABORT, 'operation desired-state correlations are retained');
END;
