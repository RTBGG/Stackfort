-- SPDX-License-Identifier: AGPL-3.0-or-later

-- ACME account keys are retrievable credentials and therefore use the same
-- per-record envelope encryption as TOTP secrets. One independent account is
-- retained for each explicitly supported CA environment.
CREATE TABLE acme_accounts (
    id TEXT PRIMARY KEY NOT NULL,
    environment TEXT NOT NULL UNIQUE
        CHECK (environment IN ('letsencrypt-staging', 'letsencrypt-production')),
    directory_url TEXT NOT NULL,
    contact_email TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'valid', 'deactivated', 'revoked')),
    account_uri TEXT,
    orders_url TEXT,
    terms_url TEXT,
    terms_agreed_at TEXT NOT NULL,
    public_key_thumbprint TEXT NOT NULL,
    key_ciphertext BLOB NOT NULL,
    key_nonce BLOB NOT NULL,
    key_wrapped_key BLOB NOT NULL,
    key_wrap_nonce BLOB NOT NULL,
    key_version INTEGER NOT NULL CHECK (key_version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    registered_at TEXT,
    CHECK (
        (environment = 'letsencrypt-staging'
            AND directory_url = 'https://acme-staging-v02.api.letsencrypt.org/directory')
        OR
        (environment = 'letsencrypt-production'
            AND directory_url = 'https://acme-v02.api.letsencrypt.org/directory')
    ),
    CHECK (
        (status = 'pending' AND account_uri IS NULL AND registered_at IS NULL)
        OR
        (status <> 'pending' AND account_uri IS NOT NULL AND registered_at IS NOT NULL)
    )
) STRICT;

CREATE TRIGGER acme_accounts_immutable_key
BEFORE UPDATE ON acme_accounts
WHEN NEW.id <> OLD.id
    OR NEW.environment <> OLD.environment
    OR NEW.directory_url <> OLD.directory_url
    OR NEW.public_key_thumbprint <> OLD.public_key_thumbprint
    OR NEW.key_ciphertext <> OLD.key_ciphertext
    OR NEW.key_nonce <> OLD.key_nonce
    OR NEW.key_wrapped_key <> OLD.key_wrapped_key
    OR NEW.key_wrap_nonce <> OLD.key_wrap_nonce
    OR NEW.key_version <> OLD.key_version
    OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'ACME account identity and key are immutable');
END;

CREATE TRIGGER acme_accounts_no_delete
BEFORE DELETE ON acme_accounts
BEGIN
    SELECT RAISE(ABORT, 'ACME accounts are retained');
END;
