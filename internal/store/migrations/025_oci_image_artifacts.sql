-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Approved image artifacts are append-only evidence for one immutable
-- application revision. Workload activation remains a later lifecycle step.
CREATE TABLE oci_image_artifacts (
    application_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    application_revision INTEGER NOT NULL CHECK (application_revision > 0),
    image_digest TEXT NOT NULL
        CHECK (length(image_digest) = 71 AND substr(image_digest, 1, 7) = 'sha256:'
            AND substr(image_digest, 8) NOT GLOB '*[^0-9a-f]*'),
    source_digest TEXT NOT NULL
        CHECK (length(source_digest) = 71 AND substr(source_digest, 1, 7) = 'sha256:'
            AND substr(source_digest, 8) NOT GLOB '*[^0-9a-f]*'),
    policy_version TEXT NOT NULL CHECK (policy_version = 'stackfort-oci-image-v1'),
    scanner_provider TEXT NOT NULL CHECK (scanner_provider = 'trivy'),
    scanner_version TEXT NOT NULL CHECK (scanner_version = '0.74.0'),
    unknown_vulnerabilities INTEGER NOT NULL CHECK (unknown_vulnerabilities BETWEEN 0 AND 1000000),
    low_vulnerabilities INTEGER NOT NULL CHECK (low_vulnerabilities BETWEEN 0 AND 1000000),
    medium_vulnerabilities INTEGER NOT NULL CHECK (medium_vulnerabilities BETWEEN 0 AND 1000000),
    high_vulnerabilities INTEGER NOT NULL CHECK (high_vulnerabilities = 0),
    critical_vulnerabilities INTEGER NOT NULL CHECK (critical_vulnerabilities = 0),
    prepared_at TEXT NOT NULL,
    prepared_by_identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    CHECK (unknown_vulnerabilities + low_vulnerabilities + medium_vulnerabilities <= 1000000),
    PRIMARY KEY (application_id, application_revision),
    FOREIGN KEY (account_id, application_id)
        REFERENCES oci_applications(account_id, id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX oci_image_artifacts_account_idx
    ON oci_image_artifacts(account_id, prepared_at, application_id);

CREATE TRIGGER oci_image_artifacts_no_update
BEFORE UPDATE ON oci_image_artifacts
BEGIN
    SELECT RAISE(ABORT, 'OCI image artifact evidence is immutable');
END;

CREATE TRIGGER oci_image_artifacts_no_delete
BEFORE DELETE ON oci_image_artifacts
BEGIN
    SELECT RAISE(ABORT, 'OCI image artifact evidence is retained');
END;
