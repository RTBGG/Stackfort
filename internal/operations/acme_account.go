// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/core"
	"golang.org/x/crypto/acme"
)

const ACMEAccountRegistrationKind = "acme.account.register"

type ACMEAccountRegistrationPayload struct {
	Environment   core.ACMEEnvironment `json:"environment"`
	ContactEmail  string               `json:"contactEmail"`
	TermsAccepted bool                 `json:"termsAccepted"`
}

type ACMEAccountRepository interface {
	EnsureACMEAccount(context.Context, core.EnsureACMEAccountParams) (core.ACMEAccount, error)
	LoadACMEAccountSigner(context.Context, core.ID) (crypto.Signer, error)
	CompleteACMERegistration(context.Context, core.CompleteACMERegistrationParams) (core.ACMEAccount, error)
}

type ACMERegistrationRequest struct {
	DirectoryURL  string
	ContactEmail  string
	TermsAccepted bool
	Signer        crypto.Signer
}

type ACMERegistrationResult struct {
	AccountURI string
	OrdersURL  string
	TermsURL   string
	Status     core.ACMEAccountStatus
}

type ACMERegistrar interface {
	Register(context.Context, ACMERegistrationRequest) (ACMERegistrationResult, error)
}

type acmeRegistrationFailure struct {
	code      string
	retryable bool
}

func (failure *acmeRegistrationFailure) Error() string { return failure.code }

// RFC8555Registrar registers an already persisted account key against one
// caller-selected, server-approved directory URL. HTTPClient is nil in
// production and exists only for a private test CA trust root.
type RFC8555Registrar struct {
	HTTPClient *http.Client
}

func (registrar RFC8555Registrar) Register(
	ctx context.Context,
	request ACMERegistrationRequest,
) (ACMERegistrationResult, error) {
	if request.Signer == nil || request.DirectoryURL == "" || request.ContactEmail == "" || !request.TermsAccepted {
		return ACMERegistrationResult{}, &acmeRegistrationFailure{code: "acme.registration_input_invalid"}
	}
	client := &acme.Client{
		Key: request.Signer, DirectoryURL: request.DirectoryURL,
		HTTPClient: registrar.HTTPClient,
		UserAgent:  "Stackfort/" + buildinfo.Current().Version,
	}
	directory, err := client.Discover(ctx)
	if err != nil {
		return ACMERegistrationResult{}, classifyACMEProtocolError(err)
	}
	if directory.ExternalAccountRequired {
		return ACMERegistrationResult{}, &acmeRegistrationFailure{code: "acme.external_account_required"}
	}
	account, err := client.Register(ctx, &acme.Account{
		Contact: []string{"mailto:" + request.ContactEmail},
	}, func(string) bool { return request.TermsAccepted })
	if errors.Is(err, acme.ErrAccountAlreadyExists) {
		account, err = client.GetReg(ctx, "")
	}
	if err != nil {
		return ACMERegistrationResult{}, classifyACMEProtocolError(err)
	}
	status, err := acmeAccountStatus(account.Status)
	if err != nil {
		return ACMERegistrationResult{}, err
	}
	return ACMERegistrationResult{
		AccountURI: account.URI, OrdersURL: account.OrdersURL,
		TermsURL: directory.Terms, Status: status,
	}, nil
}

type ACMEAccountRegistrationHandler struct {
	repository ACMEAccountRepository
	registrar  ACMERegistrar
}

func NewACMEAccountRegistrationHandler(
	repository ACMEAccountRepository,
	registrar ACMERegistrar,
) (*ACMEAccountRegistrationHandler, error) {
	if repository == nil || registrar == nil {
		return nil, errors.New("ACME account registration handler requires a repository and registrar")
	}
	return &ACMEAccountRegistrationHandler{repository: repository, registrar: registrar}, nil
}

func NewACMEAccountRegistrationPayload(
	payload ACMEAccountRegistrationPayload,
) (map[string]any, error) {
	if _, err := core.ACMEDirectoryURL(payload.Environment); err != nil ||
		payload.ContactEmail == "" || !payload.TermsAccepted {
		return nil, core.ErrInvalidInput
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func (handler *ACMEAccountRegistrationHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != ACMEAccountRegistrationKind || operation.AccountID != nil ||
		operation.ActorID == nil || reporter == nil {
		return nil, &Failure{Code: "acme.registration_operation_invalid"}
	}
	payload, err := decodeACMEAccountRegistrationPayload(operation.Payload)
	if err != nil {
		return nil, &Failure{Code: "acme.registration_payload_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "securing-account-key", 10, "acme.account.securing_key", nil); err != nil {
		return nil, err
	}
	account, err := handler.repository.EnsureACMEAccount(ctx, core.EnsureACMEAccountParams{
		Environment: payload.Environment, ContactEmail: payload.ContactEmail,
		TermsAccepted: payload.TermsAccepted, ActorID: operation.ActorID,
		OperationID: &operation.ID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyACMERepositoryFailure(err)
	}
	if account.Status != core.ACMEAccountPending {
		return acmeRegistrationResultObject(account), nil
	}
	signer, err := handler.repository.LoadACMEAccountSigner(ctx, account.ID)
	if err != nil {
		return nil, classifyACMERepositoryFailure(err)
	}
	if err := reporter.Checkpoint(ctx, "registering", 35, "acme.account.registering", map[string]any{
		"environment": account.Environment,
	}); err != nil {
		return nil, err
	}
	registered, err := handler.registrar.Register(ctx, ACMERegistrationRequest{
		DirectoryURL: account.DirectoryURL, ContactEmail: account.ContactEmail,
		TermsAccepted: payload.TermsAccepted, Signer: signer,
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var failure *acmeRegistrationFailure
		if errors.As(err, &failure) {
			return nil, &Failure{Code: failure.code, Retryable: failure.retryable}
		}
		return nil, &Failure{Code: "acme.authority_unavailable", Retryable: true}
	}
	if err := reporter.Checkpoint(ctx, "recording", 85, "acme.account.recording", nil); err != nil {
		return nil, err
	}
	account, err = handler.repository.CompleteACMERegistration(ctx, core.CompleteACMERegistrationParams{
		AccountID: account.ID, AccountURI: registered.AccountURI, OrdersURL: registered.OrdersURL,
		TermsURL: registered.TermsURL, Status: registered.Status,
		ActorID: operation.ActorID, OperationID: &operation.ID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyACMERepositoryFailure(err)
	}
	return acmeRegistrationResultObject(account), nil
}

func decodeACMEAccountRegistrationPayload(value map[string]any) (ACMEAccountRegistrationPayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ACMEAccountRegistrationPayload{}, err
	}
	var payload ACMEAccountRegistrationPayload
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return ACMEAccountRegistrationPayload{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ACMEAccountRegistrationPayload{}, errors.New("payload contains trailing JSON")
	}
	if _, err := core.ACMEDirectoryURL(payload.Environment); err != nil ||
		payload.ContactEmail == "" || !payload.TermsAccepted {
		return ACMEAccountRegistrationPayload{}, errors.New("invalid ACME registration payload")
	}
	return payload, nil
}

func acmeRegistrationResultObject(account core.ACMEAccount) map[string]any {
	return map[string]any{
		"acmeAccountId": string(account.ID), "environment": account.Environment,
		"status": account.Status,
	}
}

func acmeAccountStatus(value string) (core.ACMEAccountStatus, error) {
	switch value {
	case acme.StatusValid:
		return core.ACMEAccountValid, nil
	case acme.StatusDeactivated:
		return core.ACMEAccountDeactivated, nil
	case acme.StatusRevoked:
		return core.ACMEAccountRevoked, nil
	default:
		return "", &acmeRegistrationFailure{code: "acme.account_status_invalid"}
	}
}

func classifyACMEProtocolError(err error) error {
	var protocolError *acme.Error
	if errors.As(err, &protocolError) {
		retryable := protocolError.StatusCode == http.StatusTooManyRequests || protocolError.StatusCode >= 500
		return &acmeRegistrationFailure{code: "acme.authority_rejected", retryable: retryable}
	}
	return &acmeRegistrationFailure{code: "acme.authority_unavailable", retryable: true}
}

func classifyACMERepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "acme.registration_state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "acme.registration_state_conflict"}
	case errors.Is(err, core.ErrSecretStorageUnavailable):
		return &Failure{Code: "acme.secret_storage_unavailable"}
	default:
		return &Failure{Code: "acme.registration_state_unavailable", Retryable: true}
	}
}
