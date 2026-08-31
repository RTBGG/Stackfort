-- SPDX-License-Identifier: AGPL-3.0-or-later

-- WAF policy is a mutable, account/domain-scoped record rather than free-form
-- web-server configuration. Existing domains migrate to the explicit safe
-- default and can be enabled through a normal desired-state revision.
CREATE TABLE domain_waf_policies (
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('off', 'detection_only', 'blocking_pl1')),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (domain_id),
    UNIQUE (account_id, domain_id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id)
        ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

INSERT INTO domain_waf_policies (account_id, domain_id, mode, updated_at)
SELECT account_id, id, 'off', updated_at
FROM domains;

CREATE TRIGGER domain_waf_policies_no_delete
BEFORE DELETE ON domain_waf_policies
BEGIN
    SELECT RAISE(ABORT, 'domain WAF policy is retained for history');
END;
