// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/RTBGG/stackfort/internal/store"
	"golang.org/x/crypto/acme"
)

const acmeAccountKeyEnvelopeKind = "acme-account-key"

const (
	letsEncryptStagingDirectory    = "https://acme-staging-v02.api.letsencrypt.org/directory"
	letsEncryptProductionDirectory = "https://acme-v02.api.letsencrypt.org/directory"
)

type acmeAccountSecret struct {
	account  ACMEAccount
	envelope encryptedSecretEnvelope
}

// ACMEDirectoryURL resolves only the built-in authorities. A browser request
// can select an environment but cannot supply an arbitrary network endpoint.
func ACMEDirectoryURL(environment ACMEEnvironment) (string, error) {
	switch environment {
	case ACMELetsEncryptStaging:
		return letsEncryptStagingDirectory, nil
	case ACMELetsEncryptProduction:
		return letsEncryptProductionDirectory, nil
	default:
		return "", fmt.Errorf("%w: unsupported ACME environment", ErrInvalidInput)
	}
}

// EnsureACMEAccount creates one encrypted P-256 account credential per CA
// environment. Replays return the existing metadata only when the immutable
// contact and environment agree.
func (r *Repository) EnsureACMEAccount(
	ctx context.Context,
	params EnsureACMEAccountParams,
) (ACMEAccount, error) {
	directoryURL, err := ACMEDirectoryURL(params.Environment)
	if err != nil {
		return ACMEAccount{}, err
	}
	contactEmail, normalizedEmail, err := normalizeEmail(params.ContactEmail)
	if err != nil {
		return ACMEAccount{}, err
	}
	if contactEmail != normalizedEmail {
		contactEmail = normalizedEmail
	}
	if !params.TermsAccepted {
		return ACMEAccount{}, fmt.Errorf("%w: ACME terms must be accepted explicitly", ErrInvalidInput)
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return ACMEAccount{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return ACMEAccount{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return ACMEAccount{}, err
	}
	accountID, err := r.newID()
	if err != nil {
		return ACMEAccount{}, err
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), r.random)
	if err != nil {
		return ACMEAccount{}, fmt.Errorf("generate ACME account key: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return ACMEAccount{}, fmt.Errorf("encode ACME account key: %w", err)
	}
	defer clear(privateKeyDER)
	envelope, err := r.encryptSecret(acmeAccountKeyEnvelopeKind, accountID, accountID, privateKeyDER)
	if err != nil {
		return ACMEAccount{}, err
	}
	thumbprint, err := acme.JWKThumbprint(privateKey.Public())
	if err != nil {
		return ACMEAccount{}, fmt.Errorf("derive ACME account thumbprint: %w", err)
	}
	now := r.timestamp()
	account := ACMEAccount{
		ID: accountID, Environment: params.Environment, DirectoryURL: directoryURL,
		ContactEmail: contactEmail, Status: ACMEAccountPending,
		TermsAgreedAt: now, PublicKeyThumbprint: thumbprint, CreatedAt: now, UpdatedAt: now,
	}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		existing, findErr := findACMEAccountByEnvironmentTx(ctx, executor, params.Environment)
		switch {
		case findErr == nil:
			if existing.ContactEmail != contactEmail || existing.DirectoryURL != directoryURL {
				return fmt.Errorf("%w: ACME environment already has different account input", ErrConflict)
			}
			account = existing
			return nil
		case !errors.Is(findErr, sql.ErrNoRows):
			return findErr
		}
		_, err := executor.ExecContext(ctx, `
			INSERT INTO acme_accounts (
				id, environment, directory_url, contact_email, status,
				terms_agreed_at, public_key_thumbprint,
				key_ciphertext, key_nonce, key_wrapped_key, key_wrap_nonce, key_version,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(account.ID), string(account.Environment), account.DirectoryURL, account.ContactEmail,
			formatTime(account.TermsAgreedAt), account.PublicKeyThumbprint,
			envelope.Ciphertext, envelope.Nonce, envelope.WrappedKey, envelope.WrapNonce, envelope.KeyVersion,
			formatTime(account.CreatedAt), formatTime(account.UpdatedAt),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "acme.account_key_created",
			TargetType: "acme_account", TargetID: string(account.ID),
			RequestID: requestID, OperationID: params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"environment": account.Environment},
		}, now)
	})
	if err != nil {
		return ACMEAccount{}, classifyDatabaseError(err)
	}
	return account, nil
}

// LoadACMEAccountSigner decrypts a credential only for the internal ACME
// worker. No HTTP response model contains this value.
func (r *Repository) LoadACMEAccountSigner(ctx context.Context, accountID ID) (crypto.Signer, error) {
	if err := validateID(accountID, "acmeAccountId"); err != nil {
		return nil, err
	}
	var secret acmeAccountSecret
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var unusedID ID
		return reader.QueryRowContext(ctx, `
			SELECT id, key_ciphertext, key_nonce, key_wrapped_key, key_wrap_nonce, key_version
			FROM acme_accounts
			WHERE id = ?`, string(accountID)).Scan(
			&unusedID, &secret.envelope.Ciphertext, &secret.envelope.Nonce,
			&secret.envelope.WrappedKey, &secret.envelope.WrapNonce, &secret.envelope.KeyVersion,
		)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	encoded, err := r.decryptSecret(acmeAccountKeyEnvelopeKind, accountID, accountID, secret.envelope)
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	parsed, err := x509.ParsePKCS8PrivateKey(encoded)
	if err != nil {
		return nil, errors.New("decode encrypted ACME account key")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("encrypted ACME account key has an unsupported type")
	}
	return key, nil
}

func (r *Repository) CompleteACMERegistration(
	ctx context.Context,
	params CompleteACMERegistrationParams,
) (ACMEAccount, error) {
	if err := validateID(params.AccountID, "acmeAccountId"); err != nil {
		return ACMEAccount{}, err
	}
	if params.Status != ACMEAccountValid && params.Status != ACMEAccountDeactivated && params.Status != ACMEAccountRevoked {
		return ACMEAccount{}, fmt.Errorf("%w: unsupported registered ACME account status", ErrInvalidInput)
	}
	accountURI, err := validateACMEURL(params.AccountURI, "accountUri", false)
	if err != nil {
		return ACMEAccount{}, err
	}
	ordersURL, err := validateACMEURL(params.OrdersURL, "ordersUrl", true)
	if err != nil {
		return ACMEAccount{}, err
	}
	termsURL, err := validateACMEURL(params.TermsURL, "termsUrl", true)
	if err != nil {
		return ACMEAccount{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return ACMEAccount{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return ACMEAccount{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return ACMEAccount{}, err
	}
	now := r.timestamp()
	var account ACMEAccount
	err = r.state.Write(ctx, func(executor store.Executor) error {
		current, findErr := findACMEAccountByIDTx(ctx, executor, params.AccountID)
		if findErr != nil {
			return findErr
		}
		if current.Status != ACMEAccountPending {
			if current.Status != params.Status || current.AccountURI != accountURI ||
				current.OrdersURL != ordersURL || current.TermsURL != termsURL {
				return fmt.Errorf("%w: ACME registration result differs from retained state", ErrConflict)
			}
			account = current
			return nil
		}
		_, updateErr := executor.ExecContext(ctx, `
			UPDATE acme_accounts
			SET status = ?, account_uri = ?, orders_url = ?, terms_url = ?,
			    updated_at = ?, registered_at = ?
			WHERE id = ? AND status = 'pending'`,
			string(params.Status), accountURI, nullableString(ordersURL), nullableString(termsURL),
			formatTime(now), formatTime(now), string(params.AccountID),
		)
		if updateErr != nil {
			return updateErr
		}
		account, findErr = findACMEAccountByIDTx(ctx, executor, params.AccountID)
		if findErr != nil {
			return findErr
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "acme.account_registered",
			TargetType: "acme_account", TargetID: string(params.AccountID),
			RequestID: requestID, OperationID: params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"environment": account.Environment, "status": account.Status},
		}, now)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ACMEAccount{}, ErrNotFound
	}
	if err != nil {
		return ACMEAccount{}, classifyDatabaseError(err)
	}
	return account, nil
}

func (r *Repository) ListACMEAccounts(ctx context.Context) ([]ACMEAccount, error) {
	accounts := []ACMEAccount{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT id, environment, directory_url, contact_email, status,
			       account_uri, orders_url, terms_url, terms_agreed_at,
			       public_key_thumbprint, created_at, updated_at, registered_at
			FROM acme_accounts
			ORDER BY environment`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			account, scanErr := scanACMEAccount(rows)
			if scanErr != nil {
				return scanErr
			}
			accounts = append(accounts, account)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return accounts, nil
}

func findACMEAccountByEnvironmentTx(
	ctx context.Context,
	executor store.Reader,
	environment ACMEEnvironment,
) (ACMEAccount, error) {
	return scanACMEAccount(executor.QueryRowContext(ctx, `
		SELECT id, environment, directory_url, contact_email, status,
		       account_uri, orders_url, terms_url, terms_agreed_at,
		       public_key_thumbprint, created_at, updated_at, registered_at
		FROM acme_accounts
		WHERE environment = ?`, string(environment)))
}

func findACMEAccountByIDTx(ctx context.Context, executor store.Reader, accountID ID) (ACMEAccount, error) {
	return scanACMEAccount(executor.QueryRowContext(ctx, `
		SELECT id, environment, directory_url, contact_email, status,
		       account_uri, orders_url, terms_url, terms_agreed_at,
		       public_key_thumbprint, created_at, updated_at, registered_at
		FROM acme_accounts
		WHERE id = ?`, string(accountID)))
}

func scanACMEAccount(scanner rowScanner) (ACMEAccount, error) {
	var account ACMEAccount
	var environment, status, termsAgreedAt, createdAt, updatedAt string
	var accountURI, ordersURL, termsURL, registeredAt sql.NullString
	if err := scanner.Scan(
		&account.ID, &environment, &account.DirectoryURL, &account.ContactEmail, &status,
		&accountURI, &ordersURL, &termsURL, &termsAgreedAt,
		&account.PublicKeyThumbprint, &createdAt, &updatedAt, &registeredAt,
	); err != nil {
		return ACMEAccount{}, err
	}
	account.Environment = ACMEEnvironment(environment)
	account.Status = ACMEAccountStatus(status)
	expectedDirectory, err := ACMEDirectoryURL(account.Environment)
	if err != nil || account.DirectoryURL != expectedDirectory {
		return ACMEAccount{}, errors.New("stored ACME environment is invalid")
	}
	if account.Status != ACMEAccountPending && account.Status != ACMEAccountValid &&
		account.Status != ACMEAccountDeactivated && account.Status != ACMEAccountRevoked {
		return ACMEAccount{}, errors.New("stored ACME account status is invalid")
	}
	account.AccountURI, account.OrdersURL, account.TermsURL = accountURI.String, ordersURL.String, termsURL.String
	if account.TermsAgreedAt, err = parseTime(termsAgreedAt); err != nil {
		return ACMEAccount{}, err
	}
	if account.CreatedAt, err = parseTime(createdAt); err != nil {
		return ACMEAccount{}, err
	}
	if account.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ACMEAccount{}, err
	}
	if registeredAt.Valid {
		parsed, parseErr := parseTime(registeredAt.String)
		if parseErr != nil {
			return ACMEAccount{}, parseErr
		}
		account.RegisteredAt = &parsed
	}
	return account, nil
}

func validateACMEURL(value, field string, optional bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" && optional {
		return "", nil
	}
	if trimmed == "" || trimmed != value || len(trimmed) > 2048 {
		return "", fmt.Errorf("%w: %s is invalid", ErrInvalidInput, field)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: %s must be an absolute HTTPS URL", ErrInvalidInput, field)
	}
	return trimmed, nil
}
