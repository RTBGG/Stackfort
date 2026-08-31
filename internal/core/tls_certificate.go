// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

const tlsCertificateKeyEnvelopeKind = "tls-certificate-key"

type tlsCertificateSecret struct {
	certificate TLSCertificate
	envelope    encryptedSecretEnvelope
}

func (r *Repository) PrepareTLSCertificateOrder(
	ctx context.Context,
	params PrepareTLSCertificateOrderParams,
) (TLSCertificateOrder, error) {
	for field, value := range map[string]ID{
		"accountId": params.AccountID, "domainId": params.DomainID, "operationId": params.OperationID,
	} {
		if err := validateID(value, field); err != nil {
			return TLSCertificateOrder{}, err
		}
	}
	if err := validateOptionalID(params.ReplacesCertificateID, "replacesCertificateId"); err != nil {
		return TLSCertificateOrder{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return TLSCertificateOrder{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	if _, err := ACMEDirectoryURL(params.Environment); err != nil {
		return TLSCertificateOrder{}, err
	}
	if existing, err := r.TLSCertificateOrderForOperation(ctx, params.OperationID); err == nil {
		if existing.AccountID != params.AccountID || existing.DomainID != params.DomainID ||
			existing.ACMEAccount.Environment != params.Environment ||
			!optionalIDEqual(existing.ReplacesCertificateID, params.ReplacesCertificateID) {
			return TLSCertificateOrder{}, fmt.Errorf("%w: TLS order replay differs from retained state", ErrConflict)
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return TLSCertificateOrder{}, err
	}

	domain, err := r.GetDomain(ctx, params.AccountID, params.DomainID)
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	if domain.Status != DomainActive || !domain.TLS.Enabled || domain.TLS.Mode != TLSModeACME ||
		domain.TLS.ChallengeType != TLSChallengeHTTP01 || len(domain.TLS.Names) == 0 {
		return TLSCertificateOrder{}, fmt.Errorf("%w: domain is not eligible for HTTP-01 issuance", ErrConflict)
	}
	names := slices.Clone(domain.TLS.Names)
	slices.Sort(names)
	if slices.ContainsFunc(names, func(name string) bool { return strings.HasPrefix(name, "*.") }) {
		return TLSCertificateOrder{}, fmt.Errorf("%w: wildcard names require DNS-01", ErrConflict)
	}
	account, err := r.ACMEAccountByEnvironment(ctx, params.Environment)
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	if account.Status != ACMEAccountValid {
		return TLSCertificateOrder{}, fmt.Errorf("%w: ACME account is not valid", ErrConflict)
	}

	certificateID, err := r.newID()
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), r.random)
	if err != nil {
		return TLSCertificateOrder{}, fmt.Errorf("generate certificate key: %w", err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return TLSCertificateOrder{}, fmt.Errorf("encode certificate key: %w", err)
	}
	defer clear(encodedKey)
	envelope, err := r.encryptSecret(
		tlsCertificateKeyEnvelopeKind, certificateID, params.DomainID, encodedKey,
	)
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	namesJSON, err := json.Marshal(names)
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	now := r.timestamp()
	purpose := TLSCertificateIssue
	if params.ReplacesCertificateID != nil {
		purpose = TLSCertificateRenew
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		if existing, findErr := tlsCertificateOrderForOperationTx(ctx, executor, params.OperationID); findErr == nil {
			if existing.AccountID != params.AccountID || existing.DomainID != params.DomainID ||
				!optionalIDEqual(existing.ReplacesCertificateID, params.ReplacesCertificateID) {
				return fmt.Errorf("%w: TLS order replay differs from retained state", ErrConflict)
			}
			return nil
		} else if !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		var currentNames, currentStatus, mode, challenge, activeRef string
		var enabled int64
		if err := executor.QueryRowContext(ctx, `
			SELECT tls.names_json, tls.issuance_status, tls.mode, tls.challenge_type,
			       tls.enabled, COALESCE(tls.active_certificate_ref, '')
			FROM domain_tls_states tls
			JOIN domains d ON d.account_id = tls.account_id AND d.id = tls.domain_id
			WHERE tls.account_id = ? AND tls.domain_id = ? AND d.status = 'active'`,
			string(params.AccountID), string(params.DomainID),
		).Scan(&currentNames, &currentStatus, &mode, &challenge, &enabled, &activeRef); err != nil {
			return err
		}
		if currentNames != string(namesJSON) || enabled != 1 || TLSMode(mode) != TLSModeACME ||
			TLSChallengeType(challenge) != TLSChallengeHTTP01 {
			return fmt.Errorf("%w: TLS intent changed while preparing the order", ErrConflict)
		}
		if params.ReplacesCertificateID == nil {
			if activeRef != "" {
				return fmt.Errorf("%w: initial issue cannot replace an active certificate", ErrConflict)
			}
			if TLSIssuanceStatus(currentStatus) != TLSPending && TLSIssuanceStatus(currentStatus) != TLSFailed {
				return fmt.Errorf("%w: initial issuance is already in progress", ErrConflict)
			}
		} else if activeRef != string(*params.ReplacesCertificateID) {
			return fmt.Errorf("%w: renewal does not replace the active certificate", ErrConflict)
		} else if TLSIssuanceStatus(currentStatus) != TLSActive && TLSIssuanceStatus(currentStatus) != TLSFailed {
			return fmt.Errorf("%w: renewal is already in progress", ErrConflict)
		}
		var acmeStatus string
		if err := executor.QueryRowContext(ctx, `
			SELECT status FROM acme_accounts WHERE id = ? AND environment = ?`,
			string(account.ID), string(params.Environment),
		).Scan(&acmeStatus); err != nil {
			return err
		}
		if ACMEAccountStatus(acmeStatus) != ACMEAccountValid {
			return fmt.Errorf("%w: ACME account is not valid", ErrConflict)
		}
		_, err := executor.ExecContext(ctx, `
			INSERT INTO tls_certificates (
				id, account_id, domain_id, acme_account_id, status, names_json,
				key_ciphertext, key_nonce, key_wrapped_key, key_wrap_nonce, key_version, created_at
			) VALUES (?, ?, ?, ?, 'ordering', ?, ?, ?, ?, ?, ?, ?)`,
			string(certificateID), string(params.AccountID), string(params.DomainID), string(account.ID),
			string(namesJSON), envelope.Ciphertext, envelope.Nonce, envelope.WrappedKey,
			envelope.WrapNonce, envelope.KeyVersion, formatTime(now),
		)
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO tls_certificate_orders (
				operation_id, account_id, domain_id, certificate_id, purpose,
				replaces_certificate_id, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(params.OperationID), string(params.AccountID), string(params.DomainID),
			string(certificateID), string(purpose), nullableID(params.ReplacesCertificateID), formatTime(now),
		)
		if err != nil {
			return err
		}
		issuanceStatus := TLSIssuing
		if purpose == TLSCertificateRenew {
			issuanceStatus = TLSRenewing
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE domain_tls_states
			SET issuance_status = ?, last_error_code = NULL, last_error_at = NULL, updated_at = ?
			WHERE account_id = ? AND domain_id = ?`,
			string(issuanceStatus), formatTime(now), string(params.AccountID), string(params.DomainID),
		); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "tls.certificate_order_prepared",
			TargetType: "tls_certificate", TargetID: string(certificateID),
			AccountID: &params.AccountID, RequestID: requestID, OperationID: &params.OperationID,
			Result: AuditSuccess, Details: map[string]any{"purpose": purpose, "names": len(names)},
		}, now)
	})
	if err != nil {
		return TLSCertificateOrder{}, classifyDatabaseError(err)
	}
	return r.TLSCertificateOrderForOperation(ctx, params.OperationID)
}

func (r *Repository) ACMEAccountByEnvironment(
	ctx context.Context,
	environment ACMEEnvironment,
) (ACMEAccount, error) {
	if _, err := ACMEDirectoryURL(environment); err != nil {
		return ACMEAccount{}, err
	}
	var account ACMEAccount
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var readErr error
		account, readErr = findACMEAccountByEnvironmentTx(ctx, reader, environment)
		return readErr
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ACMEAccount{}, ErrNotFound
	}
	if err != nil {
		return ACMEAccount{}, classifyDatabaseError(err)
	}
	return account, nil
}

func (r *Repository) TLSCertificateOrderForOperation(
	ctx context.Context,
	operationID ID,
) (TLSCertificateOrder, error) {
	if err := validateID(operationID, "operationId"); err != nil {
		return TLSCertificateOrder{}, err
	}
	var order TLSCertificateOrder
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var readErr error
		order, readErr = tlsCertificateOrderForOperationTx(ctx, reader, operationID)
		return readErr
	})
	if errors.Is(err, sql.ErrNoRows) {
		return TLSCertificateOrder{}, ErrNotFound
	}
	if err != nil {
		return TLSCertificateOrder{}, classifyDatabaseError(err)
	}
	return order, nil
}

func tlsCertificateOrderForOperationTx(
	ctx context.Context,
	reader store.Reader,
	operationID ID,
) (TLSCertificateOrder, error) {
	var order TLSCertificateOrder
	var purpose, createdAt string
	var replaces, orderURL sql.NullString
	err := reader.QueryRowContext(ctx, `
		SELECT operation_id, account_id, domain_id, certificate_id, purpose,
		       replaces_certificate_id, order_url, created_at
		FROM tls_certificate_orders WHERE operation_id = ?`, string(operationID),
	).Scan(&order.OperationID, &order.AccountID, &order.DomainID, &order.CertificateID,
		&purpose, &replaces, &orderURL, &createdAt)
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	order.Purpose = TLSCertificatePurpose(purpose)
	order.OrderURL = orderURL.String
	if replaces.Valid {
		value := ID(replaces.String)
		order.ReplacesCertificateID = &value
	}
	if order.CreatedAt, err = parseTime(createdAt); err != nil {
		return TLSCertificateOrder{}, err
	}
	order.Certificate, err = tlsCertificateByIDTx(ctx, reader, order.CertificateID)
	if err != nil {
		return TLSCertificateOrder{}, err
	}
	order.ACMEAccount, err = findACMEAccountByIDTx(ctx, reader, order.Certificate.ACMEAccountID)
	return order, err
}

func (r *Repository) LoadTLSCertificateSigner(ctx context.Context, certificateID ID) (crypto.Signer, error) {
	if err := validateID(certificateID, "certificateId"); err != nil {
		return nil, err
	}
	var envelope encryptedSecretEnvelope
	var domainID ID
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT domain_id, key_ciphertext, key_nonce, key_wrapped_key, key_wrap_nonce, key_version
			FROM tls_certificates WHERE id = ?`, string(certificateID),
		).Scan(&domainID, &envelope.Ciphertext, &envelope.Nonce, &envelope.WrappedKey,
			&envelope.WrapNonce, &envelope.KeyVersion)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	encoded, err := r.decryptSecret(tlsCertificateKeyEnvelopeKind, certificateID, domainID, envelope)
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	parsed, err := x509.ParsePKCS8PrivateKey(encoded)
	if err != nil {
		return nil, errors.New("decode encrypted TLS certificate key")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("encrypted TLS certificate key has an unsupported type")
	}
	return key, nil
}

func (r *Repository) RecordTLSCertificateOrderURL(
	ctx context.Context,
	operationID ID,
	orderURL string,
) error {
	if err := validateID(operationID, "operationId"); err != nil {
		return err
	}
	normalized, err := validateACMEURL(orderURL, "orderUrl", false)
	if err != nil {
		return err
	}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var existing sql.NullString
		if err := executor.QueryRowContext(ctx, `
			SELECT order_url FROM tls_certificate_orders WHERE operation_id = ?`, string(operationID),
		).Scan(&existing); err != nil {
			return err
		}
		if existing.Valid {
			if existing.String != normalized {
				return fmt.Errorf("%w: ACME order URL differs from retained state", ErrConflict)
			}
			return nil
		}
		_, err := executor.ExecContext(ctx, `
			UPDATE tls_certificate_orders SET order_url = ?
			WHERE operation_id = ? AND order_url IS NULL`, normalized, string(operationID))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return classifyDatabaseError(err)
}

func (r *Repository) StageTLSCertificate(
	ctx context.Context,
	params StageTLSCertificateParams,
) (TLSCertificate, error) {
	if err := validateID(params.OperationID, "operationId"); err != nil {
		return TLSCertificate{}, err
	}
	if err := validateID(params.CertificateID, "certificateId"); err != nil {
		return TLSCertificate{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return TLSCertificate{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return TLSCertificate{}, err
	}
	if len(params.FullChainPEM) == 0 || len(params.FullChainPEM) > 64<<10 ||
		len(params.CertificateURL) > 2048 || len(params.FingerprintSHA256) != 64 ||
		len(params.Issuer) == 0 || len(params.Issuer) > 253 || len(params.SerialHex) == 0 ||
		len(params.SerialHex) > 128 || !params.NotBefore.Before(params.ExpiresAt) ||
		params.NextRenewalAt.Before(params.NotBefore) || !params.NextRenewalAt.Before(params.ExpiresAt) {
		return TLSCertificate{}, fmt.Errorf("%w: issued certificate metadata is invalid", ErrInvalidInput)
	}
	certificateURL := ""
	if params.CertificateURL != "" {
		certificateURL, err = validateACMEURL(params.CertificateURL, "certificateUrl", false)
		if err != nil {
			return TLSCertificate{}, err
		}
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		order, err := tlsCertificateOrderForOperationTx(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		if order.CertificateID != params.CertificateID {
			return fmt.Errorf("%w: certificate does not belong to operation", ErrConflict)
		}
		if order.Certificate.Status != TLSCertificateOrdering {
			if order.Certificate.Status == TLSCertificateStaged || order.Certificate.Status == TLSCertificateActive ||
				order.Certificate.Status == TLSCertificateRetired {
				if order.Certificate.FullChainPEM == params.FullChainPEM &&
					order.Certificate.FingerprintSHA256 == params.FingerprintSHA256 {
					return nil
				}
			}
			return fmt.Errorf("%w: certificate staging conflicts with retained state", ErrConflict)
		}
		_, err = executor.ExecContext(ctx, `
			UPDATE tls_certificates
			SET status = 'staged', full_chain_pem = ?, certificate_url = ?,
			    fingerprint_sha256 = ?, issuer = ?, serial_hex = ?,
			    not_before = ?, expires_at = ?, next_renewal_at = ?, issued_at = ?
			WHERE id = ? AND status = 'ordering'`,
			params.FullChainPEM, nullableString(certificateURL), params.FingerprintSHA256,
			params.Issuer, params.SerialHex, formatTime(params.NotBefore), formatTime(params.ExpiresAt),
			formatTime(params.NextRenewalAt), formatTime(now), string(params.CertificateID),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "tls.certificate_staged", TargetType: "tls_certificate",
			TargetID: string(params.CertificateID), AccountID: &order.AccountID,
			RequestID: requestID, OperationID: &params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"expiresAt": formatTime(params.ExpiresAt), "names": len(order.Certificate.Names)},
		}, now)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return TLSCertificate{}, ErrNotFound
	}
	if err != nil {
		return TLSCertificate{}, classifyDatabaseError(err)
	}
	return r.GetTLSCertificate(ctx, params.CertificateID)
}

func (r *Repository) ActivateTLSCertificate(
	ctx context.Context,
	params ActivateTLSCertificateParams,
) (TLSCertificate, error) {
	for field, value := range map[string]ID{
		"accountId": params.AccountID, "domainId": params.DomainID,
		"certificateId": params.CertificateID, "desiredStateRevisionId": params.DesiredStateRevisionID,
		"operationId": params.OperationID,
	} {
		if err := validateID(value, field); err != nil {
			return TLSCertificate{}, err
		}
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return TLSCertificate{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return TLSCertificate{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		order, err := tlsCertificateOrderForOperationTx(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		if order.AccountID != params.AccountID || order.DomainID != params.DomainID ||
			order.CertificateID != params.CertificateID {
			return fmt.Errorf("%w: certificate activation operation differs", ErrConflict)
		}
		certificate, err := tlsCertificateByIDTx(ctx, executor, params.CertificateID)
		if err != nil {
			return err
		}
		if certificate.AccountID != params.AccountID || certificate.DomainID != params.DomainID {
			return fmt.Errorf("%w: certificate scope differs", ErrConflict)
		}
		if certificate.Status == TLSCertificateActive {
			return nil
		}
		if certificate.Status != TLSCertificateStaged || certificate.ExpiresAt == nil ||
			!certificate.ExpiresAt.After(now) {
			return fmt.Errorf("%w: staged certificate is not activatable", ErrConflict)
		}
		var applied bool
		if err := executor.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM applied_state_revisions
				WHERE account_id = ? AND desired_state_revision_id = ?
				  AND operation_id = ? AND status = 'active'
			)`, string(params.AccountID), string(params.DesiredStateRevisionID), string(params.OperationID),
		).Scan(&applied); err != nil {
			return err
		}
		if !applied {
			return fmt.Errorf("%w: TLS NGINX revision is not active", ErrConflict)
		}
		var namesJSON, activeRef string
		if err := executor.QueryRowContext(ctx, `
			SELECT names_json, COALESCE(active_certificate_ref, '')
			FROM domain_tls_states WHERE account_id = ? AND domain_id = ?`,
			string(params.AccountID), string(params.DomainID),
		).Scan(&namesJSON, &activeRef); err != nil {
			return err
		}
		expectedNames, _ := json.Marshal(certificate.Names)
		if namesJSON != string(expectedNames) {
			return fmt.Errorf("%w: certificate names no longer match domain intent", ErrConflict)
		}
		if activeRef != "" && activeRef != string(params.CertificateID) {
			result, err := executor.ExecContext(ctx, `
				UPDATE tls_certificates
				SET status = 'retired', retired_at = ?
				WHERE id = ? AND account_id = ? AND domain_id = ? AND status = 'active'`,
				formatTime(now), activeRef, string(params.AccountID), string(params.DomainID),
			)
			if err != nil {
				return err
			}
			if err := expectAffected(result); err != nil {
				return fmt.Errorf("%w: active predecessor is unavailable", ErrConflict)
			}
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE tls_certificates SET status = 'active', activated_at = ?
			WHERE id = ? AND status = 'staged'`, formatTime(now), string(params.CertificateID))
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			UPDATE domain_tls_states
			SET issuance_status = 'active', active_certificate_ref = ?, issuer = ?,
			    not_before = ?, expires_at = ?, next_renewal_at = ?,
			    last_error_code = NULL, last_error_at = NULL, updated_at = ?
			WHERE account_id = ? AND domain_id = ?`,
			string(params.CertificateID), certificate.Issuer, formatTime(*certificate.NotBefore),
			formatTime(*certificate.ExpiresAt), formatTime(*certificate.NextRenewalAt), formatTime(now),
			string(params.AccountID), string(params.DomainID),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "tls.certificate_activated", TargetType: "tls_certificate",
			TargetID: string(params.CertificateID), AccountID: &params.AccountID,
			RequestID: requestID, OperationID: &params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"expiresAt": formatTime(*certificate.ExpiresAt)},
		}, now)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return TLSCertificate{}, ErrNotFound
	}
	if err != nil {
		return TLSCertificate{}, classifyDatabaseError(err)
	}
	return r.GetTLSCertificate(ctx, params.CertificateID)
}

func (r *Repository) FailTLSCertificateOrder(
	ctx context.Context,
	params FailTLSCertificateOrderParams,
) error {
	if err := validateID(params.OperationID, "operationId"); err != nil {
		return err
	}
	code, err := validateAction(params.ErrorCode, "errorCode", 80)
	if err != nil {
		return err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return err
	}
	if params.RetryAt != nil && !params.Final {
		return fmt.Errorf("%w: retryAt requires a final operation attempt", ErrInvalidInput)
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		order, err := tlsCertificateOrderForOperationTx(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		var retryAt any
		if params.RetryAt != nil && order.ReplacesCertificateID != nil {
			retryAt = formatTime(*params.RetryAt)
		}
		_, err = executor.ExecContext(ctx, `
			UPDATE domain_tls_states
			SET issuance_status = 'failed', last_error_code = ?, last_error_at = ?,
			    next_renewal_at = CASE WHEN ? IS NULL THEN next_renewal_at ELSE ? END,
			    updated_at = ?
			WHERE account_id = ? AND domain_id = ?`,
			code, formatTime(now), retryAt, retryAt, formatTime(now),
			string(order.AccountID), string(order.DomainID),
		)
		if err != nil {
			return err
		}
		if !params.Final {
			return nil
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "tls.certificate_order_failed", TargetType: "tls_certificate",
			TargetID: string(order.CertificateID), AccountID: &order.AccountID,
			RequestID: requestID, OperationID: &params.OperationID, Result: AuditFailure,
			Details: map[string]any{"errorCode": code, "validPredecessorRetained": order.ReplacesCertificateID != nil},
		}, now)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return classifyDatabaseError(err)
}

func (r *Repository) GetTLSCertificate(ctx context.Context, certificateID ID) (TLSCertificate, error) {
	if err := validateID(certificateID, "certificateId"); err != nil {
		return TLSCertificate{}, err
	}
	var certificate TLSCertificate
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var readErr error
		certificate, readErr = tlsCertificateByIDTx(ctx, reader, certificateID)
		return readErr
	})
	if errors.Is(err, sql.ErrNoRows) {
		return TLSCertificate{}, ErrNotFound
	}
	if err != nil {
		return TLSCertificate{}, classifyDatabaseError(err)
	}
	return certificate, nil
}

func (r *Repository) ListTLSCertificates(
	ctx context.Context,
	accountID ID,
	domainID ID,
) ([]TLSCertificate, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return nil, err
	}
	if err := validateID(domainID, "domainId"); err != nil {
		return nil, err
	}
	result := []TLSCertificate{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, tlsCertificateSelect+`
			WHERE account_id = ? AND domain_id = ? ORDER BY created_at DESC`,
			string(accountID), string(domainID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			certificate, err := scanTLSCertificate(rows)
			if err != nil {
				return err
			}
			result = append(result, certificate)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) ListPendingTLSCertificateIssuances(
	ctx context.Context,
	limit int,
) ([]PendingTLSCertificateIssuance, error) {
	if limit < 1 || limit > 1_000 {
		return nil, fmt.Errorf("%w: issuance limit is invalid", ErrInvalidInput)
	}
	result := []PendingTLSCertificateIssuance{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT tls.account_id, tls.domain_id, a.environment
			FROM domain_tls_states tls
			JOIN domains d ON d.account_id = tls.account_id AND d.id = tls.domain_id
			JOIN acme_accounts a ON a.environment = 'letsencrypt-production' AND a.status = 'valid'
			WHERE d.status = 'active' AND tls.enabled = 1 AND tls.mode = 'acme'
			  AND tls.challenge_type = 'http-01' AND tls.issuance_status = 'pending'
			  AND tls.active_certificate_ref IS NULL
			  AND NOT EXISTS (
			      SELECT 1 FROM tls_certificate_orders o
			      JOIN operations op ON op.id = o.operation_id
			      WHERE o.account_id = tls.account_id AND o.domain_id = tls.domain_id
			        AND op.status IN ('pending', 'running', 'cancelling')
			  )
			ORDER BY tls.updated_at LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item PendingTLSCertificateIssuance
			if err := rows.Scan(&item.AccountID, &item.DomainID, &item.Environment); err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) ListDueTLSCertificateRenewals(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]DueTLSCertificateRenewal, error) {
	if limit < 1 || limit > 1_000 {
		return nil, fmt.Errorf("%w: renewal limit is invalid", ErrInvalidInput)
	}
	result := []DueTLSCertificateRenewal{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT c.account_id, c.domain_id, c.id, a.environment, c.next_renewal_at
			FROM tls_certificates c
			JOIN acme_accounts a ON a.id = c.acme_account_id AND a.status = 'valid'
			JOIN domains d ON d.account_id = c.account_id AND d.id = c.domain_id
			JOIN domain_tls_states tls ON tls.account_id = c.account_id AND tls.domain_id = c.domain_id
			WHERE c.status = 'active' AND c.next_renewal_at <= ?
			  AND d.status = 'active' AND tls.enabled = 1 AND tls.mode = 'acme'
			  AND tls.challenge_type = 'http-01' AND tls.active_certificate_ref = c.id
			  AND NOT EXISTS (
			      SELECT 1 FROM tls_certificate_orders o
			      JOIN operations op ON op.id = o.operation_id
			      WHERE o.replaces_certificate_id = c.id
			        AND op.status IN ('pending', 'running', 'cancelling')
			  )
			ORDER BY c.next_renewal_at LIMIT ?`, formatTime(now), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item DueTLSCertificateRenewal
			var renewalAt string
			if err := rows.Scan(&item.AccountID, &item.DomainID, &item.CertificateID,
				&item.Environment, &renewalAt); err != nil {
				return err
			}
			parsed, err := parseTime(renewalAt)
			if err != nil {
				return err
			}
			item.NextRenewalAt = parsed
			result = append(result, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return result, nil
}

const tlsCertificateSelect = `
	SELECT id, account_id, domain_id, acme_account_id, status, names_json,
	       full_chain_pem, certificate_url, fingerprint_sha256, issuer, serial_hex,
	       not_before, expires_at, next_renewal_at, created_at, issued_at,
	       activated_at, retired_at
	FROM tls_certificates `

func tlsCertificateByIDTx(ctx context.Context, reader store.Reader, certificateID ID) (TLSCertificate, error) {
	return scanTLSCertificate(reader.QueryRowContext(ctx, tlsCertificateSelect+` WHERE id = ?`, string(certificateID)))
}

func scanTLSCertificate(scanner rowScanner) (TLSCertificate, error) {
	var certificate TLSCertificate
	var status, namesJSON, createdAt string
	var fullChain, certificateURL, fingerprint, issuer, serial sql.NullString
	var notBefore, expiresAt, nextRenewalAt, issuedAt, activatedAt, retiredAt sql.NullString
	if err := scanner.Scan(
		&certificate.ID, &certificate.AccountID, &certificate.DomainID, &certificate.ACMEAccountID,
		&status, &namesJSON, &fullChain, &certificateURL, &fingerprint, &issuer, &serial,
		&notBefore, &expiresAt, &nextRenewalAt, &createdAt, &issuedAt, &activatedAt, &retiredAt,
	); err != nil {
		return TLSCertificate{}, err
	}
	certificate.Status = TLSCertificateStatus(status)
	if certificate.Status != TLSCertificateOrdering && certificate.Status != TLSCertificateStaged &&
		certificate.Status != TLSCertificateActive && certificate.Status != TLSCertificateRetired {
		return TLSCertificate{}, errors.New("stored TLS certificate status is invalid")
	}
	if err := json.Unmarshal([]byte(namesJSON), &certificate.Names); err != nil || len(certificate.Names) == 0 ||
		!slices.IsSorted(certificate.Names) {
		return TLSCertificate{}, errors.New("stored TLS certificate names are invalid")
	}
	certificate.FullChainPEM, certificate.CertificateURL = fullChain.String, certificateURL.String
	certificate.FingerprintSHA256, certificate.Issuer, certificate.SerialHex = fingerprint.String, issuer.String, serial.String
	var err error
	if certificate.CreatedAt, err = parseTime(createdAt); err != nil {
		return TLSCertificate{}, err
	}
	for _, item := range []struct {
		value       sql.NullString
		destination **time.Time
	}{
		{notBefore, &certificate.NotBefore}, {expiresAt, &certificate.ExpiresAt},
		{nextRenewalAt, &certificate.NextRenewalAt}, {issuedAt, &certificate.IssuedAt},
		{activatedAt, &certificate.ActivatedAt}, {retiredAt, &certificate.RetiredAt},
	} {
		value, destination := item.value, item.destination
		if value.Valid {
			parsed, parseErr := parseTime(value.String)
			if parseErr != nil {
				return TLSCertificate{}, parseErr
			}
			*destination = &parsed
		}
	}
	return certificate, nil
}
