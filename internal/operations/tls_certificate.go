// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/tlsartifact"
	"golang.org/x/crypto/acme"
)

const (
	TLSCertificateLifecycleKind = "tls.certificate.lifecycle"
	tlsActivationSchemaVersion  = 1
)

type TLSCertificateLifecyclePayload struct {
	DomainID              string               `json:"domainId"`
	Environment           core.ACMEEnvironment `json:"environment"`
	ReplacesCertificateID string               `json:"replacesCertificateId,omitempty"`
}

type TLSCertificateRepository interface {
	PrepareTLSCertificateOrder(context.Context, core.PrepareTLSCertificateOrderParams) (core.TLSCertificateOrder, error)
	LoadACMEAccountSigner(context.Context, core.ID) (crypto.Signer, error)
	LoadTLSCertificateSigner(context.Context, core.ID) (crypto.Signer, error)
	RecordTLSCertificateOrderURL(context.Context, core.ID, string) error
	StageTLSCertificate(context.Context, core.StageTLSCertificateParams) (core.TLSCertificate, error)
	ActivateTLSCertificate(context.Context, core.ActivateTLSCertificateParams) (core.TLSCertificate, error)
	FailTLSCertificateOrder(context.Context, core.FailTLSCertificateOrderParams) error
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
	ListDomains(context.Context, core.ID, bool) ([]core.Domain, error)
	DesiredStateRevisionForOperation(context.Context, core.ID, core.ID) (core.DesiredStateRevision, error)
	CreateDesiredStateRevision(context.Context, core.CreateDesiredStateRevisionParams) (core.DesiredStateRevision, error)
	RecordAppliedStateRevision(context.Context, core.RecordAppliedStateRevisionParams) (core.AppliedStateRevision, error)
}

type TLSCertificateAgent interface {
	ReconcileACMEHTTP01(
		context.Context, string, agentprotocol.AuditCorrelation, acmehttp01.Intent,
	) (agentprotocol.ACMEHTTP01Response, error)
	StageTLSCertificate(
		context.Context, string, agentprotocol.AuditCorrelation, tlsartifact.Bundle,
	) (agentprotocol.TLSCertificateStageResponse, error)
	NGINXActivationClient
}

type ACMEIssueRequest struct {
	DirectoryURL   string
	AccountURI     string
	AccountSigner  crypto.Signer
	CertificateKey crypto.Signer
	Names          []string
	OrderURL       string
}

type ACMEIssueCallbacks struct {
	RecordOrderURL func(context.Context, string) error
	PresentHTTP01  func(context.Context, string, string) error
	CleanupHTTP01  func(context.Context, string) error
}

type ACMEIssueResult struct {
	DERChain       [][]byte
	CertificateURL string
}

type ACMEIssuer interface {
	Issue(context.Context, ACMEIssueRequest, ACMEIssueCallbacks) (ACMEIssueResult, error)
}

type acmeIssueFailure struct {
	code      string
	retryable bool
}

func (failure *acmeIssueFailure) Error() string { return failure.code }

// RFC8555Issuer implements the order flow from RFC 8555. HTTPClient is nil in
// production and is injectable only for a private test CA trust root.
type RFC8555Issuer struct{ HTTPClient *http.Client }

func (issuer RFC8555Issuer) Issue(
	ctx context.Context,
	request ACMEIssueRequest,
	callbacks ACMEIssueCallbacks,
) (ACMEIssueResult, error) {
	if request.DirectoryURL == "" || request.AccountURI == "" || request.AccountSigner == nil || request.CertificateKey == nil ||
		len(request.Names) == 0 || callbacks.RecordOrderURL == nil ||
		callbacks.PresentHTTP01 == nil || callbacks.CleanupHTTP01 == nil {
		return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_input_invalid"}
	}
	client := &acme.Client{
		Key: request.AccountSigner, DirectoryURL: request.DirectoryURL,
		KID: acme.KeyID(request.AccountURI), HTTPClient: issuer.HTTPClient,
		UserAgent: "Stackfort/" + buildinfo.Current().Version,
	}
	var order *acme.Order
	var err error
	if request.OrderURL == "" {
		order, err = client.AuthorizeOrder(ctx, acme.DomainIDs(request.Names...))
		if err == nil {
			err = callbacks.RecordOrderURL(ctx, order.URI)
		}
	} else {
		order, err = client.GetOrder(ctx, request.OrderURL)
	}
	if err != nil {
		return ACMEIssueResult{}, classifyCertificateACMEError(err)
	}
	if !acmeOrderNamesMatch(order, request.Names) {
		return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_order_names_invalid"}
	}
	if order.Status == acme.StatusValid {
		if order.CertURL == "" {
			return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_order_invalid"}
		}
		chain, err := client.FetchCert(ctx, order.CertURL, true)
		if err != nil {
			return ACMEIssueResult{}, classifyCertificateACMEError(err)
		}
		return ACMEIssueResult{DERChain: chain, CertificateURL: order.CertURL}, nil
	}
	if order.Status == acme.StatusInvalid {
		return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_order_rejected"}
	}
	if order.Status == acme.StatusPending {
		for _, authorizationURL := range order.AuthzURLs {
			authorization, err := client.GetAuthorization(ctx, authorizationURL)
			if err != nil {
				return ACMEIssueResult{}, classifyCertificateACMEError(err)
			}
			if authorization.Status == acme.StatusValid {
				if challenge := selectHTTP01Challenge(authorization.Challenges); challenge != nil {
					cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
					cleanupErr := callbacks.CleanupHTTP01(cleanupContext, challenge.Token)
					cancelCleanup()
					if cleanupErr != nil {
						return ACMEIssueResult{}, cleanupErr
					}
				}
				continue
			}
			if authorization.Status != acme.StatusPending || authorization.Wildcard {
				return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_authorization_rejected"}
			}
			challenge := selectHTTP01Challenge(authorization.Challenges)
			if challenge == nil {
				return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_http01_unavailable"}
			}
			response, err := client.HTTP01ChallengeResponse(challenge.Token)
			if err != nil {
				return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_challenge_invalid"}
			}
			if err := callbacks.PresentHTTP01(ctx, challenge.Token, response); err != nil {
				return ACMEIssueResult{}, err
			}
			_, acceptErr := client.Accept(ctx, challenge)
			if acceptErr == nil {
				_, acceptErr = client.WaitAuthorization(ctx, authorizationURL)
			}
			cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			cleanupErr := callbacks.CleanupHTTP01(cleanupContext, challenge.Token)
			cancelCleanup()
			if acceptErr != nil {
				return ACMEIssueResult{}, classifyCertificateACMEError(acceptErr)
			}
			if cleanupErr != nil {
				return ACMEIssueResult{}, cleanupErr
			}
		}
	}
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return ACMEIssueResult{}, classifyCertificateACMEError(err)
	}
	if order.Status == acme.StatusValid {
		if order.CertURL == "" {
			return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_order_invalid"}
		}
		chain, err := client.FetchCert(ctx, order.CertURL, true)
		if err != nil {
			return ACMEIssueResult{}, classifyCertificateACMEError(err)
		}
		return ACMEIssueResult{DERChain: chain, CertificateURL: order.CertURL}, nil
	}
	if order.Status != acme.StatusReady || order.FinalizeURL == "" {
		return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.acme_order_not_ready", retryable: true}
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: request.Names}, request.CertificateKey)
	if err != nil {
		return ACMEIssueResult{}, &acmeIssueFailure{code: "tls.csr_failed"}
	}
	chain, certificateURL, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return ACMEIssueResult{}, classifyCertificateACMEError(err)
	}
	return ACMEIssueResult{DERChain: chain, CertificateURL: certificateURL}, nil
}

func acmeOrderNamesMatch(order *acme.Order, expected []string) bool {
	if order == nil {
		return false
	}
	names := make([]string, 0, len(order.Identifiers))
	for _, identifier := range order.Identifiers {
		if identifier.Type != "dns" {
			return false
		}
		names = append(names, identifier.Value)
	}
	slices.Sort(names)
	return slices.Equal(names, expected)
}

func selectHTTP01Challenge(challenges []*acme.Challenge) *acme.Challenge {
	for _, challenge := range challenges {
		if challenge != nil && challenge.Type == "http-01" {
			return challenge
		}
	}
	return nil
}

func classifyCertificateACMEError(err error) error {
	var protocolError *acme.Error
	if errors.As(err, &protocolError) {
		return &acmeIssueFailure{
			code:      "tls.acme_authority_rejected",
			retryable: protocolError.StatusCode == http.StatusTooManyRequests || protocolError.StatusCode >= 500,
		}
	}
	var authorizationError *acme.AuthorizationError
	if errors.As(err, &authorizationError) {
		return &acmeIssueFailure{code: "tls.acme_authorization_rejected"}
	}
	var orderError *acme.OrderError
	if errors.As(err, &orderError) {
		return &acmeIssueFailure{code: "tls.acme_order_rejected"}
	}
	return &acmeIssueFailure{code: "tls.acme_authority_unavailable", retryable: true}
}

type TLSCertificateLifecycleHandler struct {
	repository TLSCertificateRepository
	agent      TLSCertificateAgent
	issuer     ACMEIssuer
	nginx      *NGINXActivationHandler
	now        func() time.Time
	random     io.Reader
}

func NewTLSCertificateLifecycleHandler(
	repository TLSCertificateRepository,
	agent TLSCertificateAgent,
	issuer ACMEIssuer,
) (*TLSCertificateLifecycleHandler, error) {
	if repository == nil || agent == nil || issuer == nil {
		return nil, errors.New("TLS certificate lifecycle handler requires repository, agent, and issuer")
	}
	nginx, err := NewNGINXActivationHandler(repository, agent)
	if err != nil {
		return nil, err
	}
	return &TLSCertificateLifecycleHandler{
		repository: repository, agent: agent, issuer: issuer, nginx: nginx,
		now: time.Now, random: rand.Reader,
	}, nil
}

func NewTLSCertificateLifecyclePayload(
	payload TLSCertificateLifecyclePayload,
) (map[string]any, error) {
	if _, err := core.ParseID(payload.DomainID); err != nil {
		return nil, core.ErrInvalidInput
	}
	if _, err := core.ACMEDirectoryURL(payload.Environment); err != nil {
		return nil, core.ErrInvalidInput
	}
	if payload.ReplacesCertificateID != "" {
		if _, err := core.ParseID(payload.ReplacesCertificateID); err != nil {
			return nil, core.ErrInvalidInput
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	return object, nil
}

func (handler *TLSCertificateLifecycleHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (result map[string]any, returnErr error) {
	operation := claimed.Operation
	if operation.Kind != TLSCertificateLifecycleKind || operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "tls.lifecycle_operation_invalid"}
	}
	payload, err := decodeTLSCertificateLifecyclePayload(operation.Payload)
	if err != nil {
		return nil, &Failure{Code: "tls.lifecycle_payload_invalid"}
	}
	domainID, _ := core.ParseID(payload.DomainID)
	var replacement *core.ID
	if payload.ReplacesCertificateID != "" {
		value, _ := core.ParseID(payload.ReplacesCertificateID)
		replacement = &value
	}
	if err := reporter.Checkpoint(ctx, "preparing", 5, "tls.certificate.preparing", nil); err != nil {
		return nil, err
	}
	order, err := handler.repository.PrepareTLSCertificateOrder(ctx, core.PrepareTLSCertificateOrderParams{
		AccountID: *operation.AccountID, DomainID: domainID, OperationID: operation.ID,
		Environment: payload.Environment, ReplacesCertificateID: replacement,
		ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyTLSRepositoryFailure(err)
	}
	defer func() {
		if returnErr == nil {
			return
		}
		failure := &Failure{Code: "tls.lifecycle_failed"}
		_ = errors.As(returnErr, &failure)
		final := !failure.Retryable || operation.AttemptCount >= operation.MaxAttempts
		var retryAt *time.Time
		if final && order.ReplacesCertificateID != nil {
			value := handler.now().UTC().Add(6 * time.Hour)
			retryAt = &value
		}
		if recordErr := handler.repository.FailTLSCertificateOrder(ctx, core.FailTLSCertificateOrderParams{
			OperationID: operation.ID, ErrorCode: failure.Code, Final: final, RetryAt: retryAt,
			ActorID: operation.ActorID, RequestID: operation.RequestID,
		}); recordErr != nil {
			returnErr = &Failure{Code: "tls.lifecycle_state_unavailable", Retryable: true}
		}
	}()
	accountSigner, err := handler.repository.LoadACMEAccountSigner(ctx, order.ACMEAccount.ID)
	if err != nil {
		return nil, classifyTLSRepositoryFailure(err)
	}
	certificateSigner, err := handler.repository.LoadTLSCertificateSigner(ctx, order.CertificateID)
	if err != nil {
		return nil, classifyTLSRepositoryFailure(err)
	}
	certificate := order.Certificate
	if certificate.Status == core.TLSCertificateOrdering {
		if err := reporter.Checkpoint(ctx, "authorizing", 15, "tls.certificate.authorizing", map[string]any{
			"names": len(certificate.Names), "purpose": order.Purpose,
		}); err != nil {
			return nil, err
		}
		correlation := tlsLifecycleCorrelation(operation)
		issued, err := handler.issuer.Issue(ctx, ACMEIssueRequest{
			DirectoryURL: order.ACMEAccount.DirectoryURL, AccountURI: order.ACMEAccount.AccountURI,
			AccountSigner:  accountSigner,
			CertificateKey: certificateSigner, Names: certificate.Names, OrderURL: order.OrderURL,
		}, ACMEIssueCallbacks{
			RecordOrderURL: func(callContext context.Context, value string) error {
				return handler.repository.RecordTLSCertificateOrderURL(callContext, operation.ID, value)
			},
			PresentHTTP01: func(callContext context.Context, token, keyAuthorization string) error {
				key := tlsChallengeIdempotencyKey("present", operation.ID, token)
				response, err := handler.agent.ReconcileACMEHTTP01(callContext, key, correlation, acmehttp01.Intent{
					Action: acmehttp01.ActionPresent, Token: token, KeyAuthorization: keyAuthorization,
				})
				if err != nil || !response.Presented {
					return classifyTLSAgentFailure(err, "tls.http01_present_failed")
				}
				return nil
			},
			CleanupHTTP01: func(callContext context.Context, token string) error {
				key := tlsChallengeIdempotencyKey("cleanup", operation.ID, token)
				response, err := handler.agent.ReconcileACMEHTTP01(callContext, key, correlation, acmehttp01.Intent{
					Action: acmehttp01.ActionCleanup, Token: token,
				})
				if err != nil || response.Presented {
					return classifyTLSAgentFailure(err, "tls.http01_cleanup_failed")
				}
				return nil
			},
		})
		if err != nil {
			var issueFailure *acmeIssueFailure
			if errors.As(err, &issueFailure) {
				return nil, &Failure{Code: issueFailure.code, Retryable: issueFailure.retryable}
			}
			var failure *Failure
			if errors.As(err, &failure) {
				return nil, failure
			}
			return nil, &Failure{Code: "tls.acme_authority_unavailable", Retryable: true}
		}
		metadata, err := validateIssuedCertificate(
			issued, certificate.Names, certificateSigner, handler.now().UTC(), handler.random,
		)
		if err != nil {
			return nil, &Failure{Code: "tls.certificate_validation_failed"}
		}
		certificate, err = handler.repository.StageTLSCertificate(ctx, core.StageTLSCertificateParams{
			OperationID: operation.ID, CertificateID: certificate.ID,
			FullChainPEM: metadata.fullChainPEM, CertificateURL: issued.CertificateURL,
			FingerprintSHA256: metadata.fingerprint, Issuer: metadata.issuer, SerialHex: metadata.serialHex,
			NotBefore: metadata.notBefore, ExpiresAt: metadata.expiresAt, NextRenewalAt: metadata.nextRenewalAt,
			ActorID: operation.ActorID, RequestID: operation.RequestID,
		})
		if err != nil {
			return nil, classifyTLSRepositoryFailure(err)
		}
	}
	if certificate.Status != core.TLSCertificateStaged && certificate.Status != core.TLSCertificateActive {
		return nil, &Failure{Code: "tls.certificate_state_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "staging", 70, "tls.certificate.staging", nil); err != nil {
		return nil, err
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(certificateSigner)
	if err != nil {
		return nil, &Failure{Code: "tls.certificate_key_invalid"}
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	clear(privateKeyDER)
	bundle := tlsartifact.Bundle{
		CertificateID: string(certificate.ID), Names: certificate.Names,
		FullChainPEM: certificate.FullChainPEM, PrivateKeyPEM: string(privateKeyPEM),
	}
	clear(privateKeyPEM)
	if tlsartifact.Validate(bundle) != nil {
		return nil, &Failure{Code: "tls.certificate_bundle_invalid"}
	}
	correlation := tlsLifecycleCorrelation(operation)
	staged, err := handler.agent.StageTLSCertificate(
		ctx, "tls-stage-"+string(operation.ID), correlation, bundle,
	)
	bundle.PrivateKeyPEM = ""
	if err != nil || staged.CertificateID != string(certificate.ID) {
		return nil, classifyTLSAgentFailure(err, "tls.certificate_stage_failed")
	}
	revision, specs, options, err := handler.activationRevision(ctx, operation, certificate)
	if err != nil {
		return nil, classifyTLSRepositoryFailure(err)
	}
	activation, err := handler.nginx.runPayloadWithProgress(
		ctx,
		operation,
		reporter,
		NGINXActivationPayload{
			DesiredStateRevisionID: string(revision.ID), Domains: specs, Options: options,
		},
		nginxActivationProgress{validating: 75, activating: 80, recording: 90},
	)
	if err != nil {
		return nil, err
	}
	certificate, err = handler.repository.ActivateTLSCertificate(ctx, core.ActivateTLSCertificateParams{
		AccountID: *operation.AccountID, DomainID: order.DomainID, CertificateID: certificate.ID,
		DesiredStateRevisionID: revision.ID, OperationID: operation.ID,
		ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyTLSRepositoryFailure(err)
	}
	activation["certificateId"] = string(certificate.ID)
	activation["expiresAt"] = certificate.ExpiresAt.UTC().Format(time.RFC3339)
	activation["names"] = certificate.Names
	activation["purpose"] = order.Purpose
	return activation, nil
}

type tlsActivationDocument struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Domains       []nginxconfig.DomainSpec `json:"domains"`
	Options       nginxconfig.Options      `json:"options"`
}

func (handler *TLSCertificateLifecycleHandler) activationRevision(
	ctx context.Context,
	operation core.Operation,
	certificate core.TLSCertificate,
) (core.DesiredStateRevision, []nginxconfig.DomainSpec, nginxconfig.Options, error) {
	if existing, err := handler.repository.DesiredStateRevisionForOperation(
		ctx, *operation.AccountID, operation.ID,
	); err == nil {
		document, decodeErr := decodeTLSActivationDocument(existing.Document)
		return existing, document.Domains, document.Options, decodeErr
	} else if !errors.Is(err, core.ErrNotFound) {
		return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, err
	}
	account, err := handler.repository.GetHostingAccount(ctx, *operation.AccountID)
	if err != nil {
		return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, err
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, core.ErrConflict
	}
	domains, err := handler.repository.ListDomains(ctx, *operation.AccountID, false)
	if err != nil {
		return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, err
	}
	found := false
	for index := range domains {
		if domains[index].ID != certificate.DomainID {
			continue
		}
		if domains[index].Status != core.DomainActive || !slices.Equal(domains[index].TLS.Names, certificate.Names) {
			return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, core.ErrConflict
		}
		domains[index].TLS.ActiveCertificateRef = string(certificate.ID)
		domains[index].TLS.IssuanceStatus = core.TLSActive
		found = true
	}
	if !found {
		return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, core.ErrNotFound
	}
	options := nginxconfig.DefaultOptions()
	specs, err := nginxconfig.SpecsFromDomains(identity, domains, options)
	if err != nil {
		return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, core.ErrConflict
	}
	document := tlsActivationDocument{SchemaVersion: tlsActivationSchemaVersion, Domains: specs, Options: options}
	object, err := structToObject(document)
	if err != nil {
		return core.DesiredStateRevision{}, nil, nginxconfig.Options{}, err
	}
	revision, err := handler.repository.CreateDesiredStateRevision(ctx, core.CreateDesiredStateRevisionParams{
		AccountID: *operation.AccountID, Document: object, Reason: "tls.certificate.activate",
		OperationID: &operation.ID, ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
	return revision, specs, options, err
}

func decodeTLSActivationDocument(value map[string]any) (tlsActivationDocument, error) {
	var document tlsActivationDocument
	if err := decodeStrictObject(value, &document); err != nil ||
		document.SchemaVersion != tlsActivationSchemaVersion || document.Domains == nil {
		return tlsActivationDocument{}, errors.New("invalid TLS activation document")
	}
	return document, nil
}

type issuedCertificateMetadata struct {
	fullChainPEM  string
	fingerprint   string
	issuer        string
	serialHex     string
	notBefore     time.Time
	expiresAt     time.Time
	nextRenewalAt time.Time
}

func validateIssuedCertificate(
	issued ACMEIssueResult,
	expectedNames []string,
	signer crypto.Signer,
	now time.Time,
	random io.Reader,
) (issuedCertificateMetadata, error) {
	if len(issued.DERChain) == 0 || len(issued.DERChain) > 10 || signer == nil || random == nil {
		return issuedCertificateMetadata{}, errors.New("invalid certificate result")
	}
	certificates := make([]*x509.Certificate, 0, len(issued.DERChain))
	var pemChain bytes.Buffer
	for _, der := range issued.DERChain {
		certificate, err := x509.ParseCertificate(der)
		if err != nil {
			return issuedCertificateMetadata{}, err
		}
		certificates = append(certificates, certificate)
		if err := pem.Encode(&pemChain, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			return issuedCertificateMetadata{}, err
		}
	}
	leaf := certificates[0]
	names := slices.Clone(leaf.DNSNames)
	slices.Sort(names)
	if !slices.Equal(names, expectedNames) || leaf.IsCA || leaf.NotBefore.After(now.Add(5*time.Minute)) ||
		!leaf.NotAfter.After(now.Add(24*time.Hour)) || leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 ||
		!allowsServerAuthentication(leaf.ExtKeyUsage) {
		return issuedCertificateMetadata{}, errors.New("leaf certificate does not match request")
	}
	publicKey, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return issuedCertificateMetadata{}, err
	}
	leafKey, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil || !bytes.Equal(publicKey, leafKey) {
		return issuedCertificateMetadata{}, errors.New("certificate key mismatch")
	}
	for _, name := range expectedNames {
		if leaf.VerifyHostname(name) != nil {
			return issuedCertificateMetadata{}, errors.New("certificate hostname mismatch")
		}
	}
	for index := 0; index+1 < len(certificates); index++ {
		if certificates[index].CheckSignatureFrom(certificates[index+1]) != nil {
			return issuedCertificateMetadata{}, errors.New("certificate chain mismatch")
		}
	}
	lifetime := leaf.NotAfter.Sub(leaf.NotBefore)
	if lifetime <= 0 {
		return issuedCertificateMetadata{}, errors.New("certificate lifetime is invalid")
	}
	var randomBytes [8]byte
	if _, err := io.ReadFull(random, randomBytes[:]); err != nil {
		return issuedCertificateMetadata{}, err
	}
	// Persist a renewal point between 60% and 65% of the lifetime. This keeps
	// more than the recommended one-third lifetime as a backstop while avoiding
	// synchronized renewal spikes. ARI can replace this fallback later.
	jitterFraction := float64(binary.BigEndian.Uint64(randomBytes[:])) / float64(^uint64(0))
	renewalOffset := time.Duration(float64(lifetime) * (0.60 + 0.05*jitterFraction))
	digest := sha256.Sum256(leaf.Raw)
	issuerName := strings.TrimSpace(leaf.Issuer.CommonName)
	if issuerName == "" && len(leaf.Issuer.Organization) > 0 {
		issuerName = strings.TrimSpace(leaf.Issuer.Organization[0])
	}
	if issuerName == "" || len(issuerName) > 253 {
		return issuedCertificateMetadata{}, errors.New("certificate issuer is invalid")
	}
	return issuedCertificateMetadata{
		fullChainPEM: pemChain.String(), fingerprint: hex.EncodeToString(digest[:]),
		issuer: issuerName, serialHex: strings.ToLower(leaf.SerialNumber.Text(16)),
		notBefore: leaf.NotBefore.UTC(), expiresAt: leaf.NotAfter.UTC(),
		nextRenewalAt: leaf.NotBefore.UTC().Add(renewalOffset),
	}, nil
}

func allowsServerAuthentication(usages []x509.ExtKeyUsage) bool {
	if len(usages) == 0 {
		return true
	}
	return slices.Contains(usages, x509.ExtKeyUsageServerAuth) || slices.Contains(usages, x509.ExtKeyUsageAny)
}

func decodeTLSCertificateLifecyclePayload(value map[string]any) (TLSCertificateLifecyclePayload, error) {
	var payload TLSCertificateLifecyclePayload
	if err := decodeStrictObject(value, &payload); err != nil {
		return TLSCertificateLifecyclePayload{}, err
	}
	if _, err := NewTLSCertificateLifecyclePayload(payload); err != nil {
		return TLSCertificateLifecyclePayload{}, err
	}
	return payload, nil
}

func tlsLifecycleCorrelation(operation core.Operation) agentprotocol.AuditCorrelation {
	correlation := agentprotocol.AuditCorrelation{
		OperationID: string(operation.ID), ActorKind: agentprotocol.ActorSystem,
		AccountID: string(*operation.AccountID),
	}
	if operation.ActorID != nil {
		correlation.ActorKind = agentprotocol.ActorIdentity
		correlation.ActorID = string(*operation.ActorID)
	}
	return correlation
}

func tlsChallengeIdempotencyKey(action string, operationID core.ID, token string) string {
	digest := sha256.Sum256([]byte(token))
	return "tls-http01-" + action + "-" + string(operationID) + "-" + hex.EncodeToString(digest[:6])
}

func classifyTLSRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "tls.lifecycle_state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "tls.lifecycle_state_conflict"}
	case errors.Is(err, core.ErrSecretStorageUnavailable):
		return &Failure{Code: "tls.secret_storage_unavailable"}
	default:
		return &Failure{Code: "tls.lifecycle_state_unavailable", Retryable: true}
	}
}

func classifyTLSAgentFailure(err error, fallback string) error {
	if err == nil {
		return &Failure{Code: fallback, Retryable: true}
	}
	if errors.Is(err, agentprotocol.ErrInvalidRequest) {
		return &Failure{Code: "tls.agent_request_rejected"}
	}
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: fallback, Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorACMEHTTP01Conflict, agentprotocol.ErrorTLSCertificateConflict,
		agentprotocol.ErrorIdempotencyConflict, agentprotocol.ErrorInvalidRequest:
		return &Failure{Code: "tls.agent_state_conflict"}
	default:
		return &Failure{Code: fallback, Retryable: true}
	}
}
