-- SPDX-License-Identifier: AGPL-3.0-or-later

CREATE TABLE domain_cache_policies (
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    preset TEXT NOT NULL CHECK (preset IN ('disabled', 'respect_origin', 'wordpress')),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (domain_id),
    UNIQUE (account_id, domain_id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id) ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

INSERT INTO domain_cache_policies (account_id, domain_id, preset, updated_at)
SELECT account_id, id, 'disabled', updated_at
FROM domains;

CREATE TRIGGER domain_cache_policies_no_delete
BEFORE DELETE ON domain_cache_policies
BEGIN
    SELECT RAISE(ABORT, 'domain cache policy is retained for history');
END;
