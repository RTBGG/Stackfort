// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/agentrpc"
	"github.com/RTBGG/stackfort/internal/certificates"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/domainlifecycle"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/httpapi"
	"github.com/RTBGG/stackfort/internal/operations"
	"golang.org/x/crypto/acme"
)

func TestDisposableHostPrivateACMETLSLifecycleOverAgentRPC(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host TLS lifecycle test must run as root")
	}
	if _, err := hostnginx.NewReconciler().Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile NGINX baseline: %v", err)
	}

	repository, owner, account := disposableLifecycleRepository(t)
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	t.Cleanup(func() { cleanupLifecycleSELinuxContexts(t, identity, "public_html") })
	resourceUnit, err := hostingresources.AccountSliceName(identity.UID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupResourceSlice(t, resourceUnit) })

	agent := startDisposableAgentRPC(t)
	accountHandler, err := operations.NewHostingAccountReconcileHandler(repository, agent)
	if err != nil {
		t.Fatal(err)
	}
	domainHandler, err := operations.NewDomainLifecycleHandler(repository, agent)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapRunner := newDisposableRunner(t, repository, map[string]operations.Handler{
		operations.HostingAccountReconcileKind: accountHandler,
		operations.DomainLifecycleKind:         domainHandler,
	})
	accountOperation := queueDisposableAccountReconcile(t, repository, account, owner.ID)
	runDisposableLifecycle(t, bootstrapRunner, repository, account.ID, accountOperation.ID)
	browserAPI, certificateService := startDisposableBrowserAPI(t, repository, owner)

	compactAccountID := strings.ReplaceAll(string(account.ID), "-", "")
	host := "cert-" + compactAccountID[len(compactAccountID)-12:] + ".stackfort.test"
	domainOperation := browserAPI.createStaticTLSDomain(t, account.ID, host)
	runDisposableLifecycle(t, bootstrapRunner, repository, account.ID, domainOperation.ID)
	writeAccountFile(t, identity, "public_html/index.html", "stackfort-private-acme-e2e\n")
	assertHostResponse(t, host, http.StatusOK, "stackfort-private-acme-e2e")
	domain, err := repository.GetDomain(t.Context(), account.ID, domainOperation.ID)
	if err != nil || domain.Status != core.DomainActive || domain.TLS.IssuanceStatus != core.TLSPending {
		t.Fatalf("TLS domain before issuance = %#v, %v", domain, err)
	}

	acmeAccount, err := repository.EnsureACMEAccount(t.Context(), core.EnsureACMEAccountParams{
		Environment: core.ACMELetsEncryptProduction, ContactEmail: "private-ca@stackfort.test",
		TermsAccepted: true, ActorID: &owner.ID, RequestID: "vm-private-acme-account",
	})
	if err != nil {
		t.Fatal(err)
	}
	accountSigner, err := repository.LoadACMEAccountSigner(t.Context(), acmeAccount.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateCA := newDisposablePrivateACMECA(t, accountSigner)
	defer privateCA.Close()
	acmeAccount, err = repository.CompleteACMERegistration(t.Context(), core.CompleteACMERegistrationParams{
		AccountID: acmeAccount.ID, AccountURI: privateCA.URL + "/account/1",
		OrdersURL: privateCA.URL + "/account/1/orders", TermsURL: privateCA.URL + "/terms",
		Status: core.ACMEAccountValid, ActorID: &owner.ID, RequestID: "vm-private-acme-register",
	})
	if err != nil || acmeAccount.Status != core.ACMEAccountValid {
		t.Fatalf("complete private ACME account = %#v, %v", acmeAccount, err)
	}

	issuer := privateEndpointIssuer{test: t, endpoint: privateCA.URL, delegate: operations.RFC8555Issuer{
		HTTPClient: privateCA.Client(),
	}}
	tlsHandler, err := operations.NewTLSCertificateLifecycleHandler(repository, agent, issuer)
	if err != nil {
		t.Fatal(err)
	}
	tlsRunner := newDisposableRunner(t, repository, map[string]operations.Handler{
		operations.TLSCertificateLifecycleKind: loggingDisposableHandler{test: t, delegate: tlsHandler},
	})
	initialOperation := browserAPI.queueCertificate(t, account.ID, domain.ID)
	runDisposableLifecycle(t, tlsRunner, repository, account.ID, initialOperation.ID)

	initial := activeDisposableCertificate(t, repository, account.ID, domain.ID)
	if initial.NextRenewalAt == nil || initial.NextRenewalAt.After(time.Now().UTC()) {
		t.Fatalf("private CA initial certificate is not immediately renewal-due: %#v", initial.NextRenewalAt)
	}
	if privateCA.validatedChallenges() != len(domain.TLS.Names) {
		t.Fatalf("private CA validated %d HTTP-01 challenges, want %d",
			privateCA.validatedChallenges(), len(domain.TLS.Names))
	}
	assertDisposableChallengeCleaned(t, host, privateCA.tokensForOrder(1)[0])
	initialFingerprint := assertDisposableHTTPSCertificate(
		t, host, "stackfort-private-acme-e2e", initial.FingerprintSHA256,
	)
	if initialFingerprint != initial.FingerprintSHA256 {
		t.Fatalf("NGINX certificate fingerprint = %s, database = %s",
			initialFingerprint, initial.FingerprintSHA256)
	}

	queued, err := certificateService.QueueAutomaticWork(t.Context(), 10)
	if err != nil || queued != 1 {
		t.Fatalf("queue due renewal = %d, %v", queued, err)
	}
	renewalOperation := latestDisposableOperation(t, repository, operations.TLSCertificateLifecycleKind)
	if renewalOperation.ID == initialOperation.ID {
		t.Fatal("renewal scheduler returned the initial issuance operation")
	}
	runDisposableLifecycle(t, tlsRunner, repository, account.ID, renewalOperation.ID)
	renewed := activeDisposableCertificate(t, repository, account.ID, domain.ID)
	if renewed.ID == initial.ID || renewed.FingerprintSHA256 == initial.FingerprintSHA256 {
		t.Fatalf("renewal did not replace certificate: initial=%s renewed=%s", initial.ID, renewed.ID)
	}
	retiredInitial, err := repository.GetTLSCertificate(t.Context(), initial.ID)
	if err != nil || retiredInitial.Status != core.TLSCertificateRetired {
		t.Fatalf("initial certificate after renewal = %#v, %v", retiredInitial, err)
	}
	if served := assertDisposableHTTPSCertificate(
		t, host, "stackfort-private-acme-e2e", renewed.FingerprintSHA256,
	); served != renewed.FingerprintSHA256 {
		t.Fatalf("NGINX still serves predecessor %s instead of %s", served, renewed.FingerprintSHA256)
	}

	failureOperation := queueDisposableTLSRenewal(t, repository, account.ID, domain.ID, renewed.ID, owner.ID)
	failureHandler, err := operations.NewTLSCertificateLifecycleHandler(repository, agent, unavailableDisposableIssuer{})
	if err != nil {
		t.Fatal(err)
	}
	failureRunner := newDisposableRunner(t, repository, map[string]operations.Handler{
		operations.TLSCertificateLifecycleKind: failureHandler,
	})
	var runError *operations.RunError
	if err := failureRunner.RunOnce(t.Context()); !errors.As(err, &runError) {
		t.Fatalf("failed renewal worker result = %T %v", err, err)
	}
	failed, err := repository.GetOperation(t.Context(), core.OperationScope{
		AccountID: &account.ID, OperationID: failureOperation.ID,
	})
	if err != nil || failed.Status != core.OperationFailed {
		t.Fatalf("failed renewal operation = %#v, %v", failed, err)
	}
	retained := activeDisposableCertificate(t, repository, account.ID, domain.ID)
	if retained.ID != renewed.ID || assertDisposableHTTPSCertificate(
		t, host, "stackfort-private-acme-e2e", renewed.FingerprintSHA256,
	) != renewed.FingerprintSHA256 {
		t.Fatal("failed renewal replaced or stopped serving the valid active certificate")
	}
	browserAPI.assertCertificateHistory(t, account.ID, domain.ID, renewed.ID, initial.ID)

	remove := browserAPI.removeDomain(t, account.ID, domain.ID)
	runDisposableLifecycle(t, bootstrapRunner, repository, account.ID, remove.ID)
	retiredRenewal, err := repository.GetTLSCertificate(t.Context(), renewed.ID)
	if err != nil || retiredRenewal.Status != core.TLSCertificateRetired {
		t.Fatalf("active certificate after domain retirement = %#v, %v", retiredRenewal, err)
	}
	assertDisposableHTTPSRejected(t, host)
	t.Logf("STACKFORT_QUALIFICATION private-acme-agent-rpc-lifecycle=passed challenges=%d certificates=%d",
		privateCA.validatedChallenges(), privateCA.issuedCertificates())
}

func startDisposableAgentRPC(t *testing.T) *agentclient.Client {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "stackfort-agent-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(directory, "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = os.Remove(directory)
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := &http.Server{
		Handler: agentrpc.NewHandler(logger), ErrorLog: agentrpc.NewRedactedHTTPErrorLogger(logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(agentrpc.NewPeerVerifiedListener(listener, uint32(os.Geteuid()), logger))
	}()
	client, err := agentclient.New(socketPath)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	if _, err := client.Handshake(t.Context(), "vm-private-acme-handshake",
		agentprotocol.MinimumVersion, agentprotocol.MaximumVersion); err != nil {
		client.Close()
		_ = listener.Close()
		t.Fatalf("agent RPC handshake: %v", err)
	}
	t.Cleanup(func() {
		client.Close()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
		if serveErr := <-done; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			t.Errorf("stop disposable agent RPC: %v", serveErr)
		}
		_ = os.Remove(socketPath)
		_ = os.Remove(directory)
	})
	return client
}

type disposableBrowserAPI struct {
	server       *httptest.Server
	client       *http.Client
	sessionToken string
	csrfToken    string
}

type disposableOperationResponse struct {
	OperationID core.ID              `json:"operationId"`
	DomainID    core.ID              `json:"domainId"`
	Status      core.OperationStatus `json:"status"`
}

func startDisposableBrowserAPI(
	t *testing.T,
	repository *core.Repository,
	owner core.Identity,
) (*disposableBrowserAPI, *certificates.Service) {
	t.Helper()
	sessionToken := "sfs_" + strings.Repeat("s", 43)
	csrfToken := "sfc_" + strings.Repeat("c", 43)
	sessionHash := sha256.Sum256([]byte(sessionToken))
	csrfHash := sha256.Sum256([]byte(csrfToken))
	if _, err := repository.CreateSession(t.Context(), core.CreateSessionParams{
		IdentityID: owner.ID, TokenHash: sessionHash[:], CSRFSecretHash: csrfHash[:],
		ExpiresAt: time.Now().UTC().Add(time.Hour), SourceAddress: "127.0.0.1",
		UserAgent: "Stackfort disposable browser API", RequestID: "vm-private-browser-session",
	}); err != nil {
		t.Fatal(err)
	}
	domainService, err := domainlifecycle.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	certificateService, err := certificates.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewTLSServer(httpapi.NewWithServices(logger, nil, httpapi.Services{
		Authentication: repository, Domains: domainService, TLSCertificates: certificateService,
	}))
	t.Cleanup(server.Close)
	return &disposableBrowserAPI{
		server: server, client: server.Client(), sessionToken: sessionToken, csrfToken: csrfToken,
	}, certificateService
}

func (fixture *disposableBrowserAPI) createStaticTLSDomain(
	t *testing.T,
	accountID core.ID,
	host string,
) core.Operation {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"name": host, "canonicalMode": core.CanonicalServeBoth, "tlsMode": core.TLSModeACME,
		"target": map[string]any{"type": core.DomainTargetStatic},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := fixture.mutation(t, http.MethodPost,
		"/api/v1/accounts/"+string(accountID)+"/domains", string(body), "vm-private-api-domain")
	if response.DomainID != response.OperationID {
		t.Fatalf("domain API response = %#v", response)
	}
	return core.Operation{ID: response.OperationID, AccountID: &accountID, Status: response.Status}
}

func (fixture *disposableBrowserAPI) queueCertificate(
	t *testing.T,
	accountID core.ID,
	domainID core.ID,
) core.Operation {
	t.Helper()
	response := fixture.mutation(t, http.MethodPost,
		"/api/v1/accounts/"+string(accountID)+"/domains/"+string(domainID)+"/tls/issue",
		"", "vm-private-api-certificate")
	if response.DomainID != domainID {
		t.Fatalf("certificate API response = %#v", response)
	}
	return core.Operation{ID: response.OperationID, AccountID: &accountID, Status: response.Status}
}

func (fixture *disposableBrowserAPI) removeDomain(
	t *testing.T,
	accountID core.ID,
	domainID core.ID,
) core.Operation {
	t.Helper()
	response := fixture.mutation(t, http.MethodDelete,
		"/api/v1/accounts/"+string(accountID)+"/domains/"+string(domainID),
		"", "vm-private-api-remove")
	if response.DomainID != domainID {
		t.Fatalf("remove-domain API response = %#v", response)
	}
	return core.Operation{ID: response.OperationID, AccountID: &accountID, Status: response.Status}
}

func (fixture *disposableBrowserAPI) mutation(
	t *testing.T,
	method string,
	path string,
	body string,
	idempotencyKey string,
) disposableOperationResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, fixture.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-Request-ID", "request-"+idempotencyKey)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.Header.Set("X-CSRF-Token", fixture.csrfToken)
	fixture.addCookies(request)
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("browser API %s %s = %d: %s", method, path, response.StatusCode, encoded)
	}
	var result disposableOperationResponse
	if err := json.Unmarshal(encoded, &result); err != nil || result.OperationID == "" ||
		result.Status != core.OperationPending {
		t.Fatalf("browser API operation response = %#v, %v", result, err)
	}
	return result
}

func (fixture *disposableBrowserAPI) assertCertificateHistory(
	t *testing.T,
	accountID core.ID,
	domainID core.ID,
	activeID core.ID,
	retiredID core.ID,
) {
	t.Helper()
	path := "/api/v1/accounts/" + string(accountID) + "/domains/" + string(domainID) + "/tls/certificates"
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fixture.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture.addCookies(request)
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(response.Body, 128<<10))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("certificate history API = %d/%s, %v", response.StatusCode, encoded, err)
	}
	lower := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"fullchain", "certificateurl", "privatekey", "private-key", "pem"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("certificate history exposed forbidden field %q: %s", forbidden, encoded)
		}
	}
	var payload struct {
		Certificates []struct {
			ID     core.ID                   `json:"id"`
			Status core.TLSCertificateStatus `json:"status"`
		} `json:"certificates"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	statuses := make(map[core.ID]core.TLSCertificateStatus)
	for _, certificate := range payload.Certificates {
		statuses[certificate.ID] = certificate.Status
	}
	if statuses[activeID] != core.TLSCertificateActive || statuses[retiredID] != core.TLSCertificateRetired {
		t.Fatalf("certificate history statuses = %#v", statuses)
	}
}

func (fixture *disposableBrowserAPI) addCookies(request *http.Request) {
	request.AddCookie(&http.Cookie{Name: "__Host-sf-id", Value: fixture.sessionToken, Path: "/", Secure: true})
	request.AddCookie(&http.Cookie{Name: "__Host-sf-csrf", Value: fixture.csrfToken, Path: "/", Secure: true})
}

func newDisposableRunner(
	t *testing.T,
	repository *core.Repository,
	handlers map[string]operations.Handler,
) *operations.Runner {
	t.Helper()
	runner, err := operations.NewRunner(repository, handlers, operations.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func latestDisposableOperation(t *testing.T, repository *core.Repository, kind string) core.Operation {
	t.Helper()
	items, err := repository.ListRecentOperations(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Kind == kind && item.Status == core.OperationPending {
			return item
		}
	}
	t.Fatalf("no pending %s operation found", kind)
	return core.Operation{}
}

func activeDisposableCertificate(
	t *testing.T,
	repository *core.Repository,
	accountID core.ID,
	domainID core.ID,
) core.TLSCertificate {
	t.Helper()
	items, err := repository.ListTLSCertificates(t.Context(), accountID, domainID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Status == core.TLSCertificateActive {
			return item
		}
	}
	t.Fatalf("no active TLS certificate found in %#v", items)
	return core.TLSCertificate{}
}

func queueDisposableTLSRenewal(
	t *testing.T,
	repository *core.Repository,
	accountID core.ID,
	domainID core.ID,
	replacesID core.ID,
	actorID core.ID,
) core.Operation {
	t.Helper()
	payload, err := operations.NewTLSCertificateLifecyclePayload(operations.TLSCertificateLifecyclePayload{
		DomainID: string(domainID), Environment: core.ACMELetsEncryptProduction,
		ReplacesCertificateID: string(replacesID),
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := repository.CreateOperation(t.Context(), core.CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: operations.TLSCertificateLifecycleKind,
		RetryClass: core.RetrySafe, RequestID: "vm-private-acme-failed-renewal",
		IdempotencyKey: "vm-private-acme-failed-renewal", Payload: payload, MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

type privateEndpointIssuer struct {
	test     *testing.T
	endpoint string
	delegate operations.RFC8555Issuer
}

func (issuer privateEndpointIssuer) Issue(
	ctx context.Context,
	request operations.ACMEIssueRequest,
	callbacks operations.ACMEIssueCallbacks,
) (operations.ACMEIssueResult, error) {
	request.DirectoryURL = issuer.endpoint + "/directory"
	request.AccountURI = issuer.endpoint + "/account/1"
	result, err := issuer.delegate.Issue(ctx, request, callbacks)
	if err != nil && issuer.test != nil {
		issuer.test.Logf("private ACME issuer failed: %T %v", err, err)
	}
	return result, err
}

type unavailableDisposableIssuer struct{}

func (unavailableDisposableIssuer) Issue(
	context.Context,
	operations.ACMEIssueRequest,
	operations.ACMEIssueCallbacks,
) (operations.ACMEIssueResult, error) {
	return operations.ACMEIssueResult{}, errors.New("private test authority intentionally unavailable")
}

type loggingDisposableHandler struct {
	test     *testing.T
	delegate operations.Handler
}

func (handler loggingDisposableHandler) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter operations.ProgressReporter,
) (map[string]any, error) {
	result, err := handler.delegate.Run(ctx, claimed, reporter)
	if err != nil {
		handler.test.Logf("TLS lifecycle handler returned %T: %v", err, err)
	}
	return result, err
}

type disposablePrivateACMECA struct {
	*httptest.Server

	test       *testing.T
	now        time.Time
	thumbprint string
	caKey      *ecdsa.PrivateKey
	caCert     *x509.Certificate
	caDER      []byte

	mu               sync.Mutex
	nextOrderID      int
	nextSerial       int64
	orders           map[int]*disposablePrivateOrder
	challengeCount   int
	certificateCount int
}

type disposablePrivateOrder struct {
	id        int
	names     []string
	tokens    []string
	accepted  []bool
	finalized bool
	leafDER   []byte
}

func newDisposablePrivateACMECA(t *testing.T, accountSigner crypto.Signer) *disposablePrivateACMECA {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	thumbprint, err := acme.JWKThumbprint(accountSigner.Public())
	if err != nil {
		t.Fatal(err)
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(9000), Subject: pkix.Name{CommonName: "Stackfort Private VM CA"},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &disposablePrivateACMECA{
		test: t, now: now, thumbprint: thumbprint, caKey: caKey, caCert: caCert, caDER: caDER,
		nextSerial: 9100, orders: make(map[int]*disposablePrivateOrder),
	}
	fixture.Server = httptest.NewTLSServer(fixture.handler())
	return fixture
}

func (fixture *disposablePrivateACMECA) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /directory", func(w http.ResponseWriter, _ *http.Request) {
		fixture.withNonce(w)
		fixture.writeJSON(w, http.StatusOK, map[string]any{
			"newNonce": fixture.URL + "/nonce", "newAccount": fixture.URL + "/new-account",
			"newOrder": fixture.URL + "/new-order",
		})
	})
	mux.HandleFunc("HEAD /nonce", func(w http.ResponseWriter, _ *http.Request) {
		fixture.withNonce(w)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /new-order", fixture.createOrder)
	mux.HandleFunc("POST /order/", fixture.readOrder)
	mux.HandleFunc("POST /authz/", fixture.readAuthorization)
	mux.HandleFunc("POST /challenge/", fixture.acceptChallenge)
	mux.HandleFunc("POST /cert/", fixture.readCertificate)
	return mux
}

func (fixture *disposablePrivateACMECA) createOrder(w http.ResponseWriter, request *http.Request) {
	payload, err := decodeDisposableJWSPayload(request)
	if err != nil {
		fixture.writeProblem(w, "malformed", err)
		return
	}
	var input struct {
		Identifiers []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"identifiers"`
	}
	if err := json.Unmarshal(payload, &input); err != nil || len(input.Identifiers) == 0 {
		fixture.writeProblem(w, "malformed", errors.New("invalid identifiers"))
		return
	}
	names := make([]string, 0, len(input.Identifiers))
	for _, identifier := range input.Identifiers {
		if identifier.Type != "dns" {
			fixture.writeProblem(w, "unsupportedIdentifier", errors.New("only DNS identifiers are supported"))
			return
		}
		names = append(names, identifier.Value)
	}
	slices.Sort(names)
	fixture.mu.Lock()
	fixture.nextOrderID++
	id := fixture.nextOrderID
	order := &disposablePrivateOrder{id: id, names: names}
	for index := range names {
		order.tokens = append(order.tokens, fmt.Sprintf("stackfortvm%08d%04d", id, index))
		order.accepted = append(order.accepted, false)
	}
	fixture.orders[id] = order
	object := fixture.orderObjectLocked(order)
	fixture.mu.Unlock()
	fixture.withNonce(w)
	w.Header().Set("Location", fixture.orderURL(id))
	fixture.writeJSON(w, http.StatusCreated, object)
}

func (fixture *disposablePrivateACMECA) readOrder(w http.ResponseWriter, request *http.Request) {
	parts := disposablePathParts(request.URL.Path, "order")
	if len(parts) == 2 && parts[1] == "finalize" {
		fixture.finalizeOrder(w, request, parts[0])
		return
	}
	if len(parts) != 1 {
		fixture.writeProblem(w, "malformed", errors.New("invalid order path"))
		return
	}
	if _, err := decodeDisposableJWSPayload(request); err != nil {
		fixture.writeProblem(w, "malformed", err)
		return
	}
	id, order := fixture.lookupOrder(parts[0])
	if order == nil {
		fixture.writeProblem(w, "orderNotFound", errors.New("unknown order"))
		return
	}
	fixture.mu.Lock()
	object := fixture.orderObjectLocked(order)
	fixture.mu.Unlock()
	fixture.withNonce(w)
	w.Header().Set("Location", fixture.orderURL(id))
	fixture.writeJSON(w, http.StatusOK, object)
}

func (fixture *disposablePrivateACMECA) readAuthorization(w http.ResponseWriter, request *http.Request) {
	parts := disposablePathParts(request.URL.Path, "authz")
	if len(parts) != 2 {
		fixture.writeProblem(w, "malformed", errors.New("invalid authorization path"))
		return
	}
	if _, err := decodeDisposableJWSPayload(request); err != nil {
		fixture.writeProblem(w, "malformed", err)
		return
	}
	id, order := fixture.lookupOrder(parts[0])
	index, indexErr := strconv.Atoi(parts[1])
	if order == nil || indexErr != nil || index < 0 || index >= len(order.names) {
		fixture.writeProblem(w, "orderNotFound", errors.New("unknown authorization"))
		return
	}
	fixture.mu.Lock()
	valid := order.accepted[index]
	status := acme.StatusPending
	if valid {
		status = acme.StatusValid
	}
	object := map[string]any{
		"status": status, "identifier": map[string]string{"type": "dns", "value": order.names[index]},
		"challenges": []map[string]any{{
			"type": "http-01", "url": fixture.challengeURL(id, index),
			"status": status, "token": order.tokens[index],
		}},
	}
	fixture.mu.Unlock()
	fixture.withNonce(w)
	fixture.writeJSON(w, http.StatusOK, object)
}

func (fixture *disposablePrivateACMECA) acceptChallenge(w http.ResponseWriter, request *http.Request) {
	parts := disposablePathParts(request.URL.Path, "challenge")
	if len(parts) != 2 {
		fixture.writeProblem(w, "malformed", errors.New("invalid challenge path"))
		return
	}
	if _, err := decodeDisposableJWSPayload(request); err != nil {
		fixture.writeProblem(w, "malformed", err)
		return
	}
	id, order := fixture.lookupOrder(parts[0])
	index, indexErr := strconv.Atoi(parts[1])
	if order == nil || indexErr != nil || index < 0 || index >= len(order.names) {
		fixture.writeProblem(w, "orderNotFound", errors.New("unknown challenge"))
		return
	}
	fixture.mu.Lock()
	name, token := order.names[index], order.tokens[index]
	fixture.mu.Unlock()
	expected := token + "." + fixture.thumbprint
	if err := probeDisposableHTTP01(name, token, expected); err != nil {
		fixture.writeProblem(w, "unauthorized", err)
		return
	}
	fixture.mu.Lock()
	if !order.accepted[index] {
		order.accepted[index] = true
		fixture.challengeCount++
	}
	fixture.mu.Unlock()
	fixture.withNonce(w)
	fixture.writeJSON(w, http.StatusOK, map[string]any{
		"type": "http-01", "url": fixture.challengeURL(id, index),
		"status": acme.StatusPending, "token": token,
	})
}

func (fixture *disposablePrivateACMECA) finalizeOrder(w http.ResponseWriter, request *http.Request, idText string) {
	payload, err := decodeDisposableJWSPayload(request)
	if err != nil {
		fixture.writeProblem(w, "malformed", err)
		return
	}
	id, order := fixture.lookupOrder(idText)
	if order == nil {
		fixture.writeProblem(w, "orderNotFound", errors.New("unknown order"))
		return
	}
	var input struct {
		CSR string `json:"csr"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		fixture.writeProblem(w, "badCSR", err)
		return
	}
	csrDER, err := base64.RawURLEncoding.DecodeString(input.CSR)
	if err != nil {
		fixture.writeProblem(w, "badCSR", err)
		return
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil || csr.CheckSignature() != nil {
		fixture.writeProblem(w, "badCSR", errors.New("invalid CSR"))
		return
	}
	csrNames := slices.Clone(csr.DNSNames)
	slices.Sort(csrNames)
	fixture.mu.Lock()
	if !slices.Equal(csrNames, order.names) || slices.Contains(order.accepted, false) {
		fixture.mu.Unlock()
		fixture.writeProblem(w, "badCSR", errors.New("CSR or authorization mismatch"))
		return
	}
	fixture.nextSerial++
	notBefore, notAfter := fixture.now.Add(-time.Hour), fixture.now.Add(90*24*time.Hour)
	if id == 1 {
		notBefore, notAfter = fixture.now.Add(-60*24*time.Hour), fixture.now.Add(30*24*time.Hour)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(fixture.nextSerial), Subject: pkix.Name{CommonName: order.names[0]},
		DNSNames: csrNames, NotBefore: notBefore, NotAfter: notAfter,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, createErr := x509.CreateCertificate(rand.Reader, template, fixture.caCert, csr.PublicKey, fixture.caKey)
	if createErr == nil {
		order.leafDER = leafDER
		order.finalized = true
		fixture.certificateCount++
	}
	object := fixture.orderObjectLocked(order)
	fixture.mu.Unlock()
	if createErr != nil {
		fixture.writeProblem(w, "serverInternal", createErr)
		return
	}
	fixture.withNonce(w)
	w.Header().Set("Location", fixture.orderURL(id))
	fixture.writeJSON(w, http.StatusOK, object)
}

func (fixture *disposablePrivateACMECA) readCertificate(w http.ResponseWriter, request *http.Request) {
	parts := disposablePathParts(request.URL.Path, "cert")
	if len(parts) != 1 {
		fixture.writeProblem(w, "malformed", errors.New("invalid certificate path"))
		return
	}
	if _, err := decodeDisposableJWSPayload(request); err != nil {
		fixture.writeProblem(w, "malformed", err)
		return
	}
	_, order := fixture.lookupOrder(parts[0])
	if order == nil {
		fixture.writeProblem(w, "orderNotFound", errors.New("unknown certificate"))
		return
	}
	fixture.mu.Lock()
	leafDER := slices.Clone(order.leafDER)
	fixture.mu.Unlock()
	if len(leafDER) == 0 {
		fixture.writeProblem(w, "orderNotReady", errors.New("certificate is not ready"))
		return
	}
	fixture.withNonce(w)
	w.Header().Set("Content-Type", "application/pem-certificate-chain")
	w.WriteHeader(http.StatusOK)
	_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: fixture.caDER})
}

func (fixture *disposablePrivateACMECA) lookupOrder(idText string) (int, *disposablePrivateOrder) {
	id, err := strconv.Atoi(idText)
	if err != nil {
		return 0, nil
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return id, fixture.orders[id]
}

func (fixture *disposablePrivateACMECA) orderObjectLocked(order *disposablePrivateOrder) map[string]any {
	status := acme.StatusPending
	if !slices.Contains(order.accepted, false) {
		status = acme.StatusReady
	}
	if order.finalized {
		status = acme.StatusValid
	}
	authorizations := make([]string, 0, len(order.names))
	identifiers := make([]map[string]string, 0, len(order.names))
	for index, name := range order.names {
		authorizations = append(authorizations, fixture.authorizationURL(order.id, index))
		identifiers = append(identifiers, map[string]string{"type": "dns", "value": name})
	}
	object := map[string]any{
		"status": status, "expires": fixture.now.Add(time.Hour), "identifiers": identifiers,
		"authorizations": authorizations, "finalize": fixture.orderURL(order.id) + "/finalize",
	}
	if order.finalized {
		object["certificate"] = fixture.certificateURL(order.id)
	}
	return object
}

func (fixture *disposablePrivateACMECA) withNonce(w http.ResponseWriter) {
	w.Header().Set("Replay-Nonce", base64.RawURLEncoding.EncodeToString([]byte(mustUUIDv7(fixture.test))))
}

func (fixture *disposablePrivateACMECA) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fixture.test.Errorf("encode private CA response: %v", err)
	}
}

func (fixture *disposablePrivateACMECA) writeProblem(w http.ResponseWriter, problem string, err error) {
	fixture.test.Logf("private ACME problem %s: %v", problem, err)
	fixture.withNonce(w)
	w.Header().Set("Content-Type", "application/problem+json")
	fixture.writeJSON(w, http.StatusBadRequest, map[string]any{
		"type": "urn:ietf:params:acme:error:" + problem, "detail": err.Error(),
	})
}

func (fixture *disposablePrivateACMECA) orderURL(id int) string {
	return fmt.Sprintf("%s/order/%d", fixture.URL, id)
}

func (fixture *disposablePrivateACMECA) authorizationURL(id, index int) string {
	return fmt.Sprintf("%s/authz/%d/%d", fixture.URL, id, index)
}

func (fixture *disposablePrivateACMECA) challengeURL(id, index int) string {
	return fmt.Sprintf("%s/challenge/%d/%d", fixture.URL, id, index)
}

func (fixture *disposablePrivateACMECA) certificateURL(id int) string {
	return fmt.Sprintf("%s/cert/%d", fixture.URL, id)
}

func (fixture *disposablePrivateACMECA) validatedChallenges() int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.challengeCount
}

func (fixture *disposablePrivateACMECA) issuedCertificates() int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.certificateCount
}

func (fixture *disposablePrivateACMECA) tokensForOrder(id int) []string {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.orders[id] == nil {
		return nil
	}
	return slices.Clone(fixture.orders[id].tokens)
}

func disposablePathParts(path, prefix string) []string {
	value := strings.TrimPrefix(path, "/"+prefix+"/")
	if value == path || value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

func decodeDisposableJWSPayload(request *http.Request) ([]byte, error) {
	encoded, err := io.ReadAll(io.LimitReader(request.Body, 128<<10))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(envelope.Payload)
}

func probeDisposableHTTP01(host, token, expected string) error {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", "127.0.0.1:80")
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://" + host + "/.well-known/acme-challenge/" + token)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		return fmt.Errorf("HTTP-01 response for %s was status %d with a mismatched body", host, response.StatusCode)
	}
	return nil
}

func assertDisposableChallengeCleaned(t *testing.T, host, token string) {
	t.Helper()
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", "127.0.0.1:80")
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get("http://" + host + "/.well-known/acme-challenge/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cleaned HTTP-01 token status = %d, want 404", response.StatusCode)
	}
}

func assertDisposableHTTPSCertificate(
	t *testing.T,
	host string,
	expectedBody string,
	expectedFingerprint string,
) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var connection *tls.Conn
	var err error
	var fingerprint string
	for {
		dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
		// #nosec G402 -- the disposable private CA is intentionally not in the host trust store.
		connection, err = tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
			ServerName: host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12,
		})
		if err == nil {
			state := connection.ConnectionState()
			if len(state.PeerCertificates) > 0 && state.PeerCertificates[0].VerifyHostname(host) == nil {
				digest := sha256.Sum256(state.PeerCertificates[0].Raw)
				fingerprint = hex.EncodeToString(digest[:])
				if expectedFingerprint == "" || fingerprint == expectedFingerprint {
					break
				}
			}
			_ = connection.Close()
			err = errors.New("managed HTTPS host has not switched to the expected certificate")
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial managed HTTPS host after reload deadline: %v", err)
	}
	defer connection.Close()
	state := connection.ConnectionState()
	if len(state.PeerCertificates) == 0 || state.PeerCertificates[0].VerifyHostname(host) != nil {
		t.Fatal("managed HTTPS host served the wrong certificate")
	}
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(connection, "GET / HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(connection)
	if err != nil || !strings.Contains(string(response), "HTTP/1.1 200") ||
		!strings.Contains(string(response), expectedBody) {
		t.Fatalf("managed HTTPS response = %q, %v", response, err)
	}
	return fingerprint
}

func assertDisposableHTTPSRejected(t *testing.T, host string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		dialer := &net.Dialer{Timeout: 500 * time.Millisecond}
		// #nosec G402 -- this negative probe expects NGINX to reject the handshake.
		connection, err := tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
			ServerName: host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return
		}
		_ = connection.Close()
		if time.Now().After(deadline) {
			t.Fatal("retired domain still completed an HTTPS handshake after reload deadline")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
