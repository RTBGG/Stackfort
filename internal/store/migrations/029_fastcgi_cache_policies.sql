-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Keep the historical migration immutable. No tables reference this policy table.
CREATE TABLE domain_cache_policies_next (
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    preset TEXT NOT NULL CHECK (preset IN (
        'disabled', 'respect_origin', 'wordpress', 'fastcgi_respect_origin', 'fastcgi_wordpress'
    )),
    generation TEXT NOT NULL CHECK (length(generation) = 36),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (domain_id),
    UNIQUE (account_id, domain_id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id) ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

INSERT INTO domain_cache_policies_next (account_id, domain_id, preset, generation, updated_at)
SELECT account_id, domain_id, preset, domain_id, updated_at FROM domain_cache_policies;
DROP TRIGGER domain_cache_policies_no_delete;
DROP TABLE domain_cache_policies;
ALTER TABLE domain_cache_policies_next RENAME TO domain_cache_policies;

CREATE TRIGGER domain_cache_policies_no_delete
BEFORE DELETE ON domain_cache_policies
BEGIN
    SELECT RAISE(ABORT, 'domain cache policy is retained for history');
END;
