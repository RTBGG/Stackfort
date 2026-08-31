-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Certificate keys are retrievable credentials. They use the same per-record
-- envelope encryption as ACME account keys and never appear in an operation
-- payload, audit record, or HTTP response.
CREATE TABLE tls_certificates (
    id TEXT PRIMARY KEY NOT NULL,
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    acme_account_id TEXT NOT NULL REFERENCES acme_accounts(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('ordering', 'staged', 'active', 'retired')),
    names_json TEXT NOT NULL
        CHECK (json_valid(names_json) AND json_type(names_json) = 'array'
            AND json_array_length(names_json) BETWEEN 1 AND 100),
    full_chain_pem TEXT,
    certificate_url TEXT,
    fingerprint_sha256 TEXT,
    issuer TEXT,
    serial_hex TEXT,
    not_before TEXT,
    expires_at TEXT,
    next_renewal_at TEXT,
    key_ciphertext BLOB NOT NULL,
    key_nonce BLOB NOT NULL,
    key_wrapped_key BLOB NOT NULL,
    key_wrap_nonce BLOB NOT NULL,
    key_version INTEGER NOT NULL CHECK (key_version > 0),
    created_at TEXT NOT NULL,
    issued_at TEXT,
    activated_at TEXT,
    retired_at TEXT,
    UNIQUE (account_id, domain_id, id),
    FOREIGN KEY (account_id, domain_id)
        REFERENCES domains(account_id, id) ON DELETE RESTRICT,
    CHECK (
        (status = 'ordering' AND full_chain_pem IS NULL AND issued_at IS NULL
            AND activated_at IS NULL AND retired_at IS NULL)
        OR
        (status = 'staged' AND full_chain_pem IS NOT NULL AND issued_at IS NOT NULL
            AND activated_at IS NULL AND retired_at IS NULL)
        OR
        (status = 'active' AND full_chain_pem IS NOT NULL AND issued_at IS NOT NULL
            AND activated_at IS NOT NULL AND retired_at IS NULL)
        OR
        (status = 'retired' AND full_chain_pem IS NOT NULL AND issued_at IS NOT NULL
            AND activated_at IS NOT NULL AND retired_at IS NOT NULL)
    )
) STRICT;

CREATE UNIQUE INDEX tls_certificates_active_domain_idx
    ON tls_certificates(account_id, domain_id)
    WHERE status = 'active';

CREATE INDEX tls_certificates_renewal_idx
    ON tls_certificates(next_renewal_at)
    WHERE status = 'active';

CREATE TRIGGER tls_certificates_immutable_identity_and_key
BEFORE UPDATE ON tls_certificates
WHEN NEW.id <> OLD.id
    OR NEW.account_id <> OLD.account_id
    OR NEW.domain_id <> OLD.domain_id
    OR NEW.acme_account_id <> OLD.acme_account_id
    OR NEW.names_json <> OLD.names_json
    OR NEW.key_ciphertext <> OLD.key_ciphertext
    OR NEW.key_nonce <> OLD.key_nonce
    OR NEW.key_wrapped_key <> OLD.key_wrapped_key
    OR NEW.key_wrap_nonce <> OLD.key_wrap_nonce
    OR NEW.key_version <> OLD.key_version
    OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'TLS certificate identity, names, and private key are immutable');
END;

CREATE TRIGGER tls_certificates_no_delete
BEFORE DELETE ON tls_certificates
BEGIN
    SELECT RAISE(ABORT, 'TLS certificates are retained');
END;

-- One row captures the replay boundary for an ACME order. The operation and
-- certificate are one-to-one; an order URL may be filled exactly once.
CREATE TABLE tls_certificate_orders (
    operation_id TEXT PRIMARY KEY NOT NULL,
    account_id TEXT NOT NULL,
    domain_id TEXT NOT NULL,
    certificate_id TEXT NOT NULL UNIQUE,
    purpose TEXT NOT NULL CHECK (purpose IN ('issue', 'renew')),
    replaces_certificate_id TEXT,
    order_url TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (account_id, operation_id)
        REFERENCES operations(account_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, domain_id, certificate_id)
        REFERENCES tls_certificates(account_id, domain_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (account_id, domain_id, replaces_certificate_id)
        REFERENCES tls_certificates(account_id, domain_id, id) ON DELETE RESTRICT,
    CHECK (
        (purpose = 'issue' AND replaces_certificate_id IS NULL)
        OR (purpose = 'renew' AND replaces_certificate_id IS NOT NULL)
    )
) WITHOUT ROWID, STRICT;

CREATE TRIGGER tls_certificate_orders_restrict_update
BEFORE UPDATE ON tls_certificate_orders
WHEN OLD.order_url IS NOT NULL
    OR NEW.order_url IS NULL
    OR NEW.operation_id <> OLD.operation_id
    OR NEW.account_id <> OLD.account_id
    OR NEW.domain_id <> OLD.domain_id
    OR NEW.certificate_id <> OLD.certificate_id
    OR NEW.purpose <> OLD.purpose
    OR NEW.replaces_certificate_id IS NOT OLD.replaces_certificate_id
    OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'TLS certificate order identity is immutable');
END;

CREATE TRIGGER tls_certificate_orders_no_delete
BEFORE DELETE ON tls_certificate_orders
BEGIN
    SELECT RAISE(ABORT, 'TLS certificate orders are retained');
END;
