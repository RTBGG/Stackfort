-- SPDX-License-Identifier: AGPL-3.0-or-later

-- F-005 makes the exact redirect source-host scope explicit. Existing
-- revisions keep their previous base-plus-www behaviour.
ALTER TABLE domain_redirects
ADD COLUMN host_mode TEXT NOT NULL DEFAULT 'both'
    CHECK (host_mode IN ('apex_only', 'www_only', 'both'));

