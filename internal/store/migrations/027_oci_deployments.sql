-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Host ports are allocated by the control plane from a loopback-only range.
-- They are never supplied by a tenant and remain stable for the application.
CREATE TABLE oci_deployment_allocations (
    application_id TEXT PRIMARY KEY NOT NULL,
    account_id TEXT NOT NULL,
    loopback_port INTEGER NOT NULL UNIQUE CHECK (loopback_port BETWEEN 20000 AND 29999),
    allocated_at TEXT NOT NULL,
    FOREIGN KEY (account_id, application_id)
        REFERENCES oci_applications(account_id, id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX oci_deployment_allocations_account_idx
    ON oci_deployment_allocations(account_id, application_id);

CREATE TRIGGER oci_deployment_allocations_no_update
BEFORE UPDATE ON oci_deployment_allocations
BEGIN
    SELECT RAISE(ABORT, 'OCI deployment allocation is immutable');
END;

CREATE TRIGGER oci_deployment_allocations_no_delete
BEFORE DELETE ON oci_deployment_allocations
BEGIN
    SELECT RAISE(ABORT, 'OCI deployment allocation is retained');
END;

-- Successful host evidence is append-only and contains no secret plaintext or
-- caller-controlled host paths. It binds the exact image/resource/application
-- intent to the generated Quadlet digest and derived loopback endpoint.
CREATE TABLE oci_deployment_artifacts (
    application_id TEXT NOT NULL,
    account_id TEXT NOT NULL,
    application_revision INTEGER NOT NULL CHECK (application_revision > 0),
    deployment_digest TEXT NOT NULL
        CHECK (length(deployment_digest) = 71 AND substr(deployment_digest, 1, 7) = 'sha256:'
            AND substr(deployment_digest, 8) NOT GLOB '*[^0-9a-f]*'),
    quadlet_digest TEXT NOT NULL
        CHECK (length(quadlet_digest) = 71 AND substr(quadlet_digest, 1, 7) = 'sha256:'
            AND substr(quadlet_digest, 8) NOT GLOB '*[^0-9a-f]*'),
    policy_version TEXT NOT NULL CHECK (policy_version = 'stackfort-oci-deployment-v1'),
    unit_name TEXT NOT NULL
        CHECK (length(unit_name) = 50 AND substr(unit_name, 1, 10) = 'stackfort-'
            AND substr(unit_name, -8) = '.service'),
    loopback_port INTEGER NOT NULL CHECK (loopback_port BETWEEN 20000 AND 29999),
    healthy INTEGER NOT NULL CHECK (healthy = 1),
    active INTEGER NOT NULL CHECK (active = 1),
    deployed_at TEXT NOT NULL,
    deployed_by_identity_id TEXT NOT NULL REFERENCES identities(id) ON DELETE RESTRICT,
    PRIMARY KEY (application_id, application_revision, deployment_digest),
    FOREIGN KEY (account_id, application_id)
        REFERENCES oci_applications(account_id, id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX oci_deployment_artifacts_account_idx
    ON oci_deployment_artifacts(account_id, deployed_at, application_id);

CREATE TRIGGER oci_deployment_artifacts_no_update
BEFORE UPDATE ON oci_deployment_artifacts
BEGIN
    SELECT RAISE(ABORT, 'OCI deployment evidence is immutable');
END;

CREATE TRIGGER oci_deployment_artifacts_no_delete
BEFORE DELETE ON oci_deployment_artifacts
BEGIN
    SELECT RAISE(ABORT, 'OCI deployment evidence is retained');
END;
