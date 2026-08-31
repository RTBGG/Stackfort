// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostacme"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/hostphp"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/hosttls"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/store"
	"github.com/RTBGG/stackfort/internal/tlsartifact"
	"github.com/google/uuid"
)

func TestDisposableHostManagedNGINXBaseline(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}

	reconciler := hostnginx.NewReconciler()
	first, err := reconciler.Reconcile(t.Context())
	if err != nil {
		t.Fatalf("reconcile NGINX baseline: %v", err)
	}
	if !first.ConfigurationTested {
		t.Fatalf("first NGINX result = %#v", first)
	}
	second, err := reconciler.Reconcile(t.Context())
	if err != nil {
		t.Fatalf("idempotent NGINX reconcile: %v", err)
	}
	if second.Changed || !second.ConfigurationTested || second.ActivationPerformed {
		t.Fatalf("second NGINX result = %#v", second)
	}

	for path, mode := range map[string]os.FileMode{
		nginxbaseline.ManagedRoot: 0o750, nginxbaseline.PanelDirectory: 0o750,
		nginxbaseline.SitesDirectory: 0o750, nginxbaseline.MainConfiguration: 0o640,
		nginxbaseline.SystemdDropInPath: 0o644,
	} {
		assertRootOwnedMode(t, path, mode)
	}
	trusted, err := os.ReadFile(nginxbaseline.TrustedProxiesPath)
	if err != nil {
		t.Fatalf("read trusted proxy configuration: %v", err)
	}
	if string(trusted) != nginxbaseline.TrustedProxies() {
		t.Fatal("trusted proxy configuration differs from the loopback-only baseline")
	}

	assertRejectingHTTPDefault(t)
	assertRejectingHTTPSDefault(t)
	controlGroup, err := exec.Command(
		"/usr/bin/systemctl", "show", "--property=ControlGroup", "--value", "nginx.service",
	).Output()
	if err != nil {
		t.Fatalf("inspect NGINX control group: %v", err)
	}
	if got := strings.TrimSpace(string(controlGroup)); got != "/stackfort.slice/stackfort-core.slice/nginx.service" {
		t.Fatalf("NGINX control group = %q", got)
	}
	enabled, err := exec.Command("/usr/bin/systemctl", "is-enabled", "nginx.service").Output()
	if err != nil || strings.TrimSpace(string(enabled)) != "enabled" {
		t.Fatalf("NGINX service is not enabled at boot: %q, %v", enabled, err)
	}
	assertRenderedAccountConfigurationParses(t)
	assertTransactionalSiteActivation(t)
	assertTLSCertificateStagingAndHTTPS(t)
}

func TestDisposableHostStaticDomainLifecycleAndWorkerAccess(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	if _, err := hostnginx.NewReconciler().Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile NGINX baseline: %v", err)
	}

	repository, owner, account := disposableLifecycleRepository(t)
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		t.Fatal(err)
	}
	compactAccountID := strings.ReplaceAll(string(account.ID), "-", "")
	hostSuffix := compactAccountID[len(compactAccountID)-12:]
	primaryHost := "a-" + hostSuffix + ".stackfort.test"
	sharedHost := "b-" + hostSuffix + ".stackfort.test"
	permanentRedirectHost := "r-" + hostSuffix + ".stackfort.test"
	temporaryRedirectHost := "t-" + hostSuffix + ".stackfort.test"
	phpHost := "p-" + hostSuffix + ".stackfort.test"
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	t.Cleanup(func() { cleanupLifecycleSELinuxContexts(t, identity, "public_html", "domains/edited") })
	resourceUnit, err := hostingresources.AccountSliceName(identity.UID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupResourceSlice(t, resourceUnit) })
	filesystem := hostfilesystem.NewReconciler()
	accountClient := &localHostingAccountReconcileClient{
		test: t, identity: hostidentity.NewReconciler(), filesystem: filesystem, resources: hostresources.NewReconciler(),
	}
	accountHandler, err := operations.NewHostingAccountReconcileHandler(repository, accountClient)
	if err != nil {
		t.Fatal(err)
	}
	client := &localDomainLifecycleClient{
		test: t, filesystem: filesystem, nginx: hostnginx.NewActivator(), php: hostphp.NewReconciler(),
	}
	domainHandler, err := operations.NewDomainLifecycleHandler(repository, client)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := operations.NewRunner(repository, map[string]operations.Handler{
		operations.HostingAccountReconcileKind: accountHandler,
		operations.DomainLifecycleKind:         domainHandler,
	}, operations.RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	accountOperation := queueDisposableAccountReconcile(t, repository, account, owner.ID)
	runDisposableLifecycle(t, runner, repository, account.ID, accountOperation.ID)
	ready, err := repository.HostingAccountHostReady(t.Context(), account.ID)
	if err != nil || !ready {
		t.Fatalf("hosting account host readiness = %v, %v", ready, err)
	}

	primary := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-create-primary", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleCreate, Name: primaryHost,
		Target: &core.DomainTargetSpec{Type: core.DomainTargetStatic},
	})
	runDisposableLifecycle(t, runner, repository, account.ID, primary.ID)
	writeAccountFile(t, identity, "public_html/index.html", "stackfort-f004-primary\n")
	assertHostResponse(t, primaryHost, 200, "stackfort-f004-primary")
	assertCrossAccountDomainIsolation(t, runner, repository, account.ID, primary.ID, primaryHost)
	measureHTTPBaseline(t, "nginx-static", "http://127.0.0.1/", primaryHost, 4_000, 8)
	if apiHealthAvailable(t.Context()) {
		measureHTTPBaseline(t, "control-api-health", "http://127.0.0.1:8080/api/v1/health", "", 2_000, 8)
	} else {
		t.Log("control API performance baseline skipped because no installed API is listening")
	}

	platform := hostcapabilities.NewInspector().InspectPlatform()
	phpVersion, err := phpruntime.ApprovedVersion(platform.DistributionID)
	if err != nil {
		t.Fatalf("resolve native PHP runtime: %v", err)
	}
	foreignRoot, err := os.MkdirTemp("/srv/hosting/accounts", ".stackfort-php-foreign-")
	if err != nil {
		t.Fatalf("create foreign isolation fixture: %v", err)
	}
	if err := os.Chmod(foreignRoot, 0o700); err != nil {
		t.Fatalf("secure foreign isolation fixture: %v", err)
	}
	foreignSecret := filepath.Join(foreignRoot, "secret.txt")
	if err := os.WriteFile(foreignSecret, []byte("must-not-leak\n"), 0o600); err != nil {
		t.Fatalf("write foreign isolation fixture: %v", err)
	}
	t.Cleanup(func() {
		if strings.HasPrefix(foreignRoot, "/srv/hosting/accounts/.stackfort-php-foreign-") {
			_ = os.RemoveAll(foreignRoot)
		}
	})
	phpDomain := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-create-php", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleCreate, Name: phpHost,
		Target: &core.DomainTargetSpec{Type: core.DomainTargetPHP, PHPVersion: phpVersion},
	})
	runDisposableLifecycle(t, runner, repository, account.ID, phpDomain.ID)
	phpSource := fmt.Sprintf(`<?php
$foreign = @file_get_contents(%q);
$written = @file_put_contents(__DIR__ . '/written.txt', "account-write-ok\n");
header('Content-Type: text/plain');
echo "stackfort-php-ok ", $foreign === false ? "isolated" : "leaked", " ", $written === false ? "write-failed" : "write-ok", "\n";
`, foreignSecret)
	writeAccountFile(t, identity, "public_html/index.php", phpSource)
	assertHostResponse(t, phpHost, http.StatusOK, "stackfort-php-ok isolated write-ok")
	assertPHPAccountPool(t, platform.DistributionID, phpVersion, identity)
	assertPHPAccountPoolObservation(t, client.php, phpVersion, identity, agentprotocol.PHPPoolActive, true)
	writtenPath := filepath.Join(identity.HomeDirectory, "public_html/written.txt")
	writtenInfo, err := os.Stat(writtenPath)
	if err != nil {
		t.Fatalf("PHP did not create its account-owned file: %v", err)
	}
	writtenStat, ok := writtenInfo.Sys().(*syscall.Stat_t)
	if !ok || writtenStat.Uid != identity.UID {
		t.Fatalf("PHP-created file identity = %#v", writtenStat)
	}
	removePHP := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-remove-php", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleRemove, DomainID: string(phpDomain.ID),
	})
	runDisposableLifecycle(t, runner, repository, account.ID, removePHP.ID)
	assertHostResponse(t, phpHost, 0, "")
	assertPHPAccountPoolRetired(t, phpVersion, identity)
	assertPHPAccountPoolObservation(t, client.php, phpVersion, identity, agentprotocol.PHPPoolMissing, false)
	t.Log("STACKFORT_QUALIFICATION php-account-pool-isolation=passed")
	t.Log("STACKFORT_QUALIFICATION php-account-pool-observability=passed")

	challengeToken := strings.ReplaceAll(mustUUIDv7(t), "-", "")
	keyAuthorization := challengeToken + "." + strings.Repeat("a", 43)
	presenter := hostacme.NewPresenter()
	t.Cleanup(func() {
		_, _ = presenter.Reconcile(context.Background(), mustUUIDv7(t), acmehttp01.Intent{
			Action: acmehttp01.ActionCleanup, Token: challengeToken,
		})
	})
	presented, err := presenter.Reconcile(t.Context(), mustUUIDv7(t), acmehttp01.Intent{
		Action: acmehttp01.ActionPresent, Token: challengeToken, KeyAuthorization: keyAuthorization,
	})
	if err != nil || !presented.Changed || !presented.Presented {
		t.Fatalf("present HTTP-01 response = %#v, %v", presented, err)
	}
	challengePath := "/.well-known/acme-challenge/" + challengeToken
	assertHostPathResponse(t, primaryHost, challengePath, 200, keyAuthorization)
	// The canonical www host normally redirects, but its challenge route must
	// win before the customer-facing redirect.
	assertHostPathResponse(t, "www."+primaryHost, challengePath, 200, keyAuthorization)
	cleaned, err := presenter.Reconcile(t.Context(), mustUUIDv7(t), acmehttp01.Intent{
		Action: acmehttp01.ActionCleanup, Token: challengeToken,
	})
	if err != nil || !cleaned.Changed || cleaned.Presented {
		t.Fatalf("cleanup HTTP-01 response = %#v, %v", cleaned, err)
	}
	assertHostPathResponse(t, primaryHost, challengePath, 404, "")
	assertHostRedirect(t, "www."+primaryHost, "/canonical?query=1", 301,
		"https://"+primaryHost+"/canonical?query=1")

	if err := runAs(identity, "/usr/bin/chmod", "0600", filepath.Join(identity.HomeDirectory, "public_html/index.html")); err != nil {
		t.Fatalf("restrict account-owned file: %v", err)
	}
	assertHostResponse(t, primaryHost, 403, "")
	if err := runAs(identity, "/usr/bin/chmod", "0640", filepath.Join(identity.HomeDirectory, "public_html/index.html")); err != nil {
		t.Fatalf("restore account-owned file access: %v", err)
	}
	assertHostResponse(t, primaryHost, 200, "stackfort-f004-primary")

	escape := filepath.Join(identity.HomeDirectory, "public_html/escape")
	if err := runAs(identity, "/usr/bin/ln", "-s", "/etc/passwd", escape); err != nil {
		t.Fatalf("create account-owned escape symlink: %v", err)
	}
	assertHostPathResponse(t, primaryHost, "/escape", 404, "")
	edit := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-edit-primary", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleEdit, DomainID: string(primary.ID),
		Target: &core.DomainTargetSpec{
			Type: core.DomainTargetStatic, RootMode: core.DocumentRootCustom,
			DocumentRoot: "domains/edited",
		},
	})
	runDisposableLifecycle(t, runner, repository, account.ID, edit.ID)
	writeAccountFile(t, identity, "domains/edited/index.html", "stackfort-f004-edited\n")
	assertHostResponse(t, primaryHost, 200, "stackfort-f004-edited")

	shared := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-create-shared", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleCreate, Name: sharedHost,
		Target: &core.DomainTargetSpec{
			Type: core.DomainTargetStatic, RootMode: core.DocumentRootShared,
			SharedWithDomainID: &primary.ID,
		},
	})
	runDisposableLifecycle(t, runner, repository, account.ID, shared.ID)
	assertHostResponse(t, sharedHost, 200, "stackfort-f004-edited")

	permanentRedirect := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-create-permanent-redirect", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleCreate, Name: permanentRedirectHost,
		Target: &core.DomainTargetSpec{Type: core.DomainTargetRedirect, Redirect: &core.RedirectSpec{
			StatusCode: core.RedirectPermanent, TargetURL: "https://destination.stackfort.test/base?fixed=1",
			HostMode: core.RedirectHostWWWOnly, PreservePath: true, PreserveQuery: true,
		}},
	})
	runDisposableLifecycle(t, runner, repository, account.ID, permanentRedirect.ID)
	assertHostPathResponse(t, permanentRedirectHost, "/article?id=7", 404, "")
	assertHostRedirect(t, "www."+permanentRedirectHost, "/article?id=7", 301,
		"https://destination.stackfort.test/base/article?fixed=1&id=7")

	temporaryRedirect := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-create-temporary-redirect", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleCreate, Name: temporaryRedirectHost,
		Target: &core.DomainTargetSpec{Type: core.DomainTargetRedirect, Redirect: &core.RedirectSpec{
			StatusCode: core.RedirectTemporary, TargetURL: "https://destination.stackfort.test/landing?fixed=1",
			HostMode: core.RedirectHostApexOnly,
		}},
	})
	runDisposableLifecycle(t, runner, repository, account.ID, temporaryRedirect.ID)
	assertHostRedirect(t, temporaryRedirectHost, "/ignored?source=ignored", 302,
		"https://destination.stackfort.test/landing?fixed=1")
	assertHostPathResponse(t, "www."+temporaryRedirectHost, "/ignored?source=ignored", 404, "")

	suspend := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-suspend-primary", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleSuspend, DomainID: string(primary.ID),
	})
	runDisposableLifecycle(t, runner, repository, account.ID, suspend.ID)
	assertHostResponse(t, primaryHost, 0, "")
	assertHostResponse(t, sharedHost, 200, "stackfort-f004-edited")

	resume := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-resume-primary", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleResume, DomainID: string(primary.ID),
	})
	runDisposableLifecycle(t, runner, repository, account.ID, resume.ID)
	assertHostResponse(t, primaryHost, 200, "stackfort-f004-edited")

	removePrimary := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-remove-primary", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleRemove, DomainID: string(primary.ID),
	})
	runDisposableLifecycle(t, runner, repository, account.ID, removePrimary.ID)
	assertHostResponse(t, primaryHost, 0, "")
	assertHostResponse(t, sharedHost, 200, "stackfort-f004-edited")
	if content, err := os.ReadFile(filepath.Join(identity.HomeDirectory, "domains/edited/index.html")); err != nil ||
		!strings.Contains(string(content), "stackfort-f004-edited") {
		t.Fatalf("shared/non-empty root was removed: %q, %v", content, err)
	}

	removeShared := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-remove-shared", operations.DomainLifecyclePayload{
		Action: operations.DomainLifecycleRemove, DomainID: string(shared.ID),
	})
	runDisposableLifecycle(t, runner, repository, account.ID, removeShared.ID)
	assertHostResponse(t, sharedHost, 0, "")
	if _, err := os.Stat(filepath.Join(identity.HomeDirectory, "domains/edited/index.html")); err != nil {
		t.Fatalf("non-empty root did not survive final domain removal: %v", err)
	}
	for key, domainID := range map[string]core.ID{
		"permanent-redirect": permanentRedirect.ID,
		"temporary-redirect": temporaryRedirect.ID,
	} {
		remove := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-remove-"+key, operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleRemove, DomainID: string(domainID),
		})
		runDisposableLifecycle(t, runner, repository, account.ID, remove.ID)
	}
	assertHostResponse(t, permanentRedirectHost, 0, "")
	assertHostResponse(t, temporaryRedirectHost, 0, "")
}

func assertCrossAccountDomainIsolation(
	t *testing.T,
	runner *operations.Runner,
	repository *core.Repository,
	accountID core.ID,
	domainID core.ID,
	host string,
) {
	t.Helper()
	foreignOwner, err := repository.CreateIdentity(t.Context(), core.CreateIdentityParams{
		Email: "vm-foreign@stackfort.test", DisplayName: "VM Foreign", Locale: core.LocaleEnglish,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignPackage, err := repository.CreatePackage(t.Context(), core.CreatePackageParams{
		Name: "VM foreign isolation", Slug: "vm-foreign-isolation",
		Limits: core.PackageLimits{MaxDomains: 1}, ActorID: &foreignOwner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignAccount, err := repository.CreateHostingAccount(t.Context(), core.CreateHostingAccountParams{
		Name: "VM Foreign", Slug: "vm-foreign", OwnerIdentityID: foreignOwner.ID,
		PackageID: foreignPackage.ID, ActorID: &foreignOwner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	foreignOperation := queueDisposableLifecycle(
		t, repository, foreignAccount.ID, foreignOwner.ID, "vm-cross-account-remove",
		operations.DomainLifecyclePayload{Action: operations.DomainLifecycleRemove, DomainID: string(domainID)},
	)
	runErr := runner.RunOnce(t.Context())
	var classified *operations.RunError
	if !errors.As(runErr, &classified) || classified.Code != "domain.lifecycle_state_invalid" {
		t.Fatalf("cross-account lifecycle error = %v", runErr)
	}
	operation, err := repository.GetOperation(t.Context(), core.OperationScope{
		AccountID: &foreignAccount.ID, OperationID: foreignOperation.ID,
	})
	if err != nil || operation.Status != core.OperationFailed ||
		operation.ErrorCode != "domain.lifecycle_state_invalid" {
		t.Fatalf("cross-account operation = %#v, %v", operation, err)
	}
	if _, err := repository.GetDomain(t.Context(), accountID, domainID); err != nil {
		t.Fatalf("cross-account attempt changed the owning domain: %v", err)
	}
	assertHostResponse(t, host, http.StatusOK, "stackfort-f004-primary")
	t.Log("STACKFORT_QUALIFICATION cross-account-domain-isolation=passed")
}

type httpBaselineMetric struct {
	Name              string  `json:"name"`
	Requests          int     `json:"requests"`
	Concurrency       int     `json:"concurrency"`
	DurationMillis    int64   `json:"durationMillis"`
	RequestsPerSecond float64 `json:"requestsPerSecond"`
	BytesPerSecond    float64 `json:"bytesPerSecond"`
	P50Microseconds   int64   `json:"p50Microseconds"`
	P95Microseconds   int64   `json:"p95Microseconds"`
	P99Microseconds   int64   `json:"p99Microseconds"`
}

type httpBaselineSample struct {
	latency time.Duration
	bytes   int64
	err     error
}

func apiHealthAvailable(ctx context.Context) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8080/api/v1/health", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return response.StatusCode == http.StatusOK
}

func measureHTTPBaseline(t *testing.T, name, target, host string, requestCount, concurrency int) httpBaselineMetric {
	t.Helper()
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, ForceAttemptHTTP2: false,
		MaxIdleConns: concurrency, MaxIdleConnsPerHost: concurrency, MaxConnsPerHost: concurrency,
		IdleConnTimeout: 10 * time.Second, ResponseHeaderTimeout: 2 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	requestOnce := func() httpBaselineSample {
		started := time.Now()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		if err != nil {
			return httpBaselineSample{err: err}
		}
		request.Host = host
		response, err := client.Do(request)
		if err != nil {
			return httpBaselineSample{latency: time.Since(started), err: err}
		}
		defer response.Body.Close()
		bytesRead, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		if readErr != nil {
			return httpBaselineSample{latency: time.Since(started), bytes: bytesRead, err: readErr}
		}
		if response.StatusCode != http.StatusOK {
			return httpBaselineSample{
				latency: time.Since(started), bytes: bytesRead,
				err: fmt.Errorf("unexpected HTTP status %d", response.StatusCode),
			}
		}
		return httpBaselineSample{latency: time.Since(started), bytes: bytesRead}
	}
	for index := 0; index < 32; index++ {
		var lastErr error
		for attempt := 1; attempt <= 5; attempt++ {
			if sample := requestOnce(); sample.err == nil {
				lastErr = nil
				break
			} else {
				lastErr = sample.err
			}
			if attempt < 5 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		if lastErr != nil {
			t.Fatalf("warm up %s after retries: %v", name, lastErr)
		}
	}

	jobs := make(chan struct{})
	results := make(chan httpBaselineSample, requestCount)
	for worker := 0; worker < concurrency; worker++ {
		go func() {
			for range jobs {
				results <- requestOnce()
			}
		}()
	}
	started := time.Now()
	go func() {
		defer close(jobs)
		for index := 0; index < requestCount; index++ {
			jobs <- struct{}{}
		}
	}()
	latencies := make([]time.Duration, 0, requestCount)
	var totalBytes int64
	for index := 0; index < requestCount; index++ {
		sample := <-results
		if sample.err != nil {
			t.Fatalf("measure %s: %v", name, sample.err)
		}
		latencies = append(latencies, sample.latency)
		totalBytes += sample.bytes
	}
	elapsed := time.Since(started)
	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	percentile := func(percent int) int64 {
		index := (len(latencies) - 1) * percent / 100
		return latencies[index].Microseconds()
	}
	requestsPerSecond := float64(requestCount) / elapsed.Seconds()
	metric := httpBaselineMetric{
		Name: name, Requests: requestCount, Concurrency: concurrency,
		DurationMillis: elapsed.Milliseconds(), RequestsPerSecond: requestsPerSecond,
		BytesPerSecond:  float64(totalBytes) / elapsed.Seconds(),
		P50Microseconds: percentile(50), P95Microseconds: percentile(95), P99Microseconds: percentile(99),
	}
	encoded, err := json.Marshal(metric)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("STACKFORT_PERFORMANCE %s", encoded)
	if requestsPerSecond < 100 || metric.P99Microseconds > time.Second.Microseconds() {
		t.Fatalf("gross performance regression for %s: %s", name, encoded)
	}
	return metric
}

type localDomainLifecycleClient struct {
	test       *testing.T
	filesystem *hostfilesystem.Reconciler
	nginx      *hostnginx.Activator
	php        *hostphp.Reconciler
}

type localHostingAccountReconcileClient struct {
	test       *testing.T
	identity   *hostidentity.Reconciler
	filesystem *hostfilesystem.Reconciler
	resources  *hostresources.Reconciler
}

func (client *localHostingAccountReconcileClient) ReconcileHostingIdentity(
	ctx context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	spec hostingidentity.Spec,
) (agentprotocol.HostingIdentityResponse, error) {
	result, err := client.identity.Reconcile(ctx, spec)
	if err != nil && client.test != nil {
		client.test.Logf("host account identity reconciliation error: %v", err)
	}
	return agentprotocol.HostingIdentityResponse{
		Changed: result.Changed(), GroupCreated: result.GroupCreated, UserCreated: result.UserCreated,
		UserRepaired: result.UserRepaired, DirectoryCreated: result.DirectoryCreated,
		OwnershipRepaired: result.OwnershipRepaired,
	}, err
}

func (client *localHostingAccountReconcileClient) ReconcileHostingFilesystem(
	ctx context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	spec hostingstorage.Spec,
) (agentprotocol.HostingFilesystemResponse, error) {
	result, err := client.filesystem.Reconcile(ctx, spec)
	if err != nil && client.test != nil {
		client.test.Logf("host account filesystem reconciliation error: %v", err)
	}
	return agentprotocol.HostingFilesystemResponse{
		ProjectID: spec.ProjectID, ProjectAssigned: result.Layout.ProjectAssigned,
		DirectoriesCreated: result.Layout.DirectoriesCreated, QuotaApplied: result.QuotaApplied,
		Capability: result.Capability,
	}, err
}

func (client *localHostingAccountReconcileClient) ReconcileHostingResources(
	ctx context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	spec hostingresources.Spec,
) (agentprotocol.HostingResourcesResponse, error) {
	result, err := client.resources.Reconcile(ctx, spec)
	if err != nil && client.test != nil {
		client.test.Logf("host account resource reconciliation error: %v", err)
	}
	return agentprotocol.HostingResourcesResponse{
		UID: spec.Identity.UID, UnitName: result.UnitName, ControlGroup: result.ControlGroup,
		UnitsChanged: result.UnitsChanged, LimitsApplied: result.LimitsApplied, Capability: result.Capability,
	}, err
}

func (client *localDomainLifecycleClient) EnsureHostingDocumentRoot(
	ctx context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	relativePath string,
	access agentprotocol.DocumentRootAccess,
) (agentprotocol.DocumentRootResponse, error) {
	result, err := client.filesystem.EnsureDocumentRoot(ctx, identity, relativePath, access)
	return agentprotocol.DocumentRootResponse{RelativePath: result.RelativePath, Created: result.Created}, err
}

func (client *localDomainLifecycleClient) ReconcilePHPPools(
	ctx context.Context,
	_ string,
	_ agentprotocol.AuditCorrelation,
	pools phpruntime.PoolSetSpec,
) (agentprotocol.PHPPoolSetResponse, error) {
	result, err := client.php.Reconcile(ctx, pools)
	if err != nil && client.test != nil {
		client.test.Logf("host PHP pool reconciliation error: %v", err)
	}
	return agentprotocol.PHPPoolSetResponse{
		Versions: result.Versions, Changed: result.Changed, Active: result.Active, Capability: result.Capability,
	}, err
}

func (client *localDomainLifecycleClient) ActivateNGINXSiteSpecs(
	ctx context.Context,
	_ string,
	correlation agentprotocol.AuditCorrelation,
	identity hostingidentity.Spec,
	desiredStateRevisionID string,
	domains []nginxconfig.DomainSpec,
	options nginxconfig.Options,
) (agentprotocol.NGINXActivationResponse, error) {
	result, err := client.nginx.Activate(ctx, hostnginx.ActivationSpec{
		Identity: identity, RevisionID: correlation.OperationID,
		DesiredStateRevisionID: desiredStateRevisionID, Domains: domains, Options: options,
	})
	if err != nil && client.test != nil {
		client.test.Logf("host NGINX activation error: %v", err)
	}
	return agentprotocol.NGINXActivationResponse{
		Changed: result.Changed, ConfigurationTested: result.ConfigurationTested,
		ReloadPerformed: result.ReloadPerformed, HealthChecked: result.HealthChecked,
		RecoveryPerformed: result.RecoveryPerformed, ActiveRevisionID: result.ActiveRevisionID,
		PreviousRevisionID:     result.PreviousRevisionID,
		DesiredStateRevisionID: result.DesiredStateRevisionID,
		ConfigDigest:           fmt.Sprintf("%x", result.ConfigDigest), RenderedDomains: result.RenderedDomains,
	}, err
}

func disposableLifecycleRepository(t *testing.T) (*core.Repository, core.Identity, core.HostingAccount) {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(t.Context(), filepath.Join(stateRoot, "stackfort.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	repository, err := core.NewRepositoryWithMasterKey(state, bytes.Repeat([]byte{0x73}, 32))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := repository.CreateIdentity(t.Context(), core.CreateIdentityParams{
		Email: "vm-lifecycle@stackfort.test", DisplayName: "VM Lifecycle", Locale: core.LocaleEnglish,
	})
	if err != nil {
		t.Fatal(err)
	}
	packageRecord, err := repository.CreatePackage(t.Context(), core.CreatePackageParams{
		Name: "VM static lifecycle", Slug: "vm-static-lifecycle",
		Limits: core.PackageLimits{
			MaxDomains: 6, AllowedPHPVersions: []string{nativePHPVersion(t)},
			Features: core.PackageFeatures{CustomRedirects: true, WAFExceptions: true},
		}, ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 32; index++ {
		account, createErr := repository.CreateHostingAccount(t.Context(), core.CreateHostingAccountParams{
			Name: fmt.Sprintf("VM Lifecycle %d", index), Slug: fmt.Sprintf("vm-lifecycle-%d", index),
			OwnerIdentityID: owner.ID, PackageID: packageRecord.ID, ActorID: &owner.ID,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		numericID := strconv.FormatUint(uint64(account.UnixIdentity.UID), 10)
		_, userErr := user.LookupId(numericID)
		_, groupErr := user.LookupGroupId(numericID)
		_, userMissing := userErr.(user.UnknownUserIdError)
		_, groupMissing := groupErr.(user.UnknownGroupIdError)
		if userMissing && groupMissing {
			return repository, owner, account
		}
	}
	t.Fatal("no unused lifecycle UID/GID was found in the first 32 managed IDs")
	return nil, core.Identity{}, core.HostingAccount{}
}

func nativePHPVersion(t *testing.T) string {
	t.Helper()
	platform := hostcapabilities.NewInspector().InspectPlatform()
	version, err := phpruntime.ApprovedVersion(platform.DistributionID)
	if err != nil {
		t.Fatalf("resolve native PHP version for %s: %v", platform.DistributionID, err)
	}
	return version
}

func assertPHPAccountPool(
	t *testing.T,
	distribution string,
	version string,
	identity hostingidentity.Spec,
) {
	t.Helper()
	profile, err := phpruntime.ForDistribution(distribution, version)
	if err != nil {
		t.Fatal(err)
	}
	unit, _ := phpruntime.UnitName(identity, version)
	configuration, _ := phpruntime.ConfigurationPath(identity, version)
	socket, _ := phpruntime.SocketPath(identity, version)
	assertRootOwnedMode(t, configuration, 0o640)
	assertRootOwnedMode(t, filepath.Join("/etc/systemd/system", unit), 0o644)
	properties, err := exec.Command(
		"/usr/bin/systemctl", "show", "--no-pager", "--property=ActiveState",
		"--property=UnitFileState", "--property=ControlGroup", "--property=MainPID", unit,
	).Output()
	if err != nil {
		t.Fatalf("inspect PHP pool unit %s: %v", unit, err)
	}
	observed := map[string]string{}
	for _, line := range strings.Split(string(properties), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			observed[key] = value
		}
	}
	wantedGroup := "/stackfort.slice/stackfort-accounts.slice/stackfort-accounts-" +
		strconv.FormatUint(uint64(identity.UID), 10) + ".slice/"
	if observed["ActiveState"] != "active" || observed["UnitFileState"] != "enabled" ||
		!strings.HasPrefix(observed["ControlGroup"], wantedGroup) {
		t.Fatalf("PHP pool unit state = %#v", observed)
	}
	socketInfo, err := os.Lstat(socket)
	if err != nil || socketInfo.Mode()&os.ModeSocket == 0 || socketInfo.Mode().Perm() != 0o600 {
		t.Fatalf("PHP pool socket = %#v / %v", socketInfo, err)
	}
	worker, err := user.Lookup(profile.NGINXUser)
	if err != nil {
		t.Fatal(err)
	}
	workerUID, _ := strconv.ParseUint(worker.Uid, 10, 32)
	socketStat, ok := socketInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(socketStat.Uid) != workerUID {
		t.Fatalf("PHP pool socket owner = %#v, want %d", socketStat, workerUID)
	}
	mainPID, err := strconv.ParseUint(observed["MainPID"], 10, 32)
	if err != nil || mainPID == 0 {
		t.Fatalf("PHP pool main PID = %q", observed["MainPID"])
	}
	children, err := os.ReadFile(filepath.Join("/proc", observed["MainPID"], "task", observed["MainPID"], "children"))
	if err != nil {
		t.Fatalf("inspect PHP pool workers: %v", err)
	}
	foundAccountWorker := false
	for _, child := range strings.Fields(string(children)) {
		info, statErr := os.Stat(filepath.Join("/proc", child))
		if statErr != nil {
			continue
		}
		stat, statOK := info.Sys().(*syscall.Stat_t)
		if statOK && stat.Uid == identity.UID {
			foundAccountWorker = true
			break
		}
	}
	if !foundAccountWorker {
		t.Fatalf("PHP pool has no worker running as account UID %d: %q", identity.UID, children)
	}
	if distribution == "rocky" {
		label, err := exec.Command("/usr/bin/ls", "-Zd", filepath.Join(identity.HomeDirectory, "public_html")).Output()
		if err != nil || !strings.Contains(string(label), "httpd_sys_rw_content_t") {
			t.Fatalf("PHP document-root SELinux label = %q / %v", label, err)
		}
	}
}

func assertPHPAccountPoolRetired(t *testing.T, version string, identity hostingidentity.Spec) {
	t.Helper()
	unit, _ := phpruntime.UnitName(identity, version)
	configuration, _ := phpruntime.ConfigurationPath(identity, version)
	socket, _ := phpruntime.SocketPath(identity, version)
	for _, path := range []string{configuration, filepath.Join("/etc/systemd/system", unit), socket} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("retired PHP artifact remains at %s: %v", path, err)
		}
	}
	if exec.Command("/usr/bin/systemctl", "is-active", "--quiet", unit).Run() == nil {
		t.Fatalf("retired PHP pool remains active: %s", unit)
	}
}

func assertPHPAccountPoolObservation(
	t *testing.T,
	reconciler *hostphp.Reconciler,
	version string,
	identity hostingidentity.Spec,
	wantState agentprotocol.PHPPoolState,
	requireMetrics bool,
) {
	t.Helper()
	inspection, err := reconciler.Inspect(t.Context(), agentprotocol.PHPPoolInspectRequest{
		Identity: identity, Versions: []string{version},
	})
	if err != nil || len(inspection.Pools) != 1 || inspection.Pools[0].Version != version ||
		inspection.Pools[0].State != wantState {
		t.Fatalf("PHP pool observation = %#v, want state %s: %v", inspection, wantState, err)
	}
	pool := inspection.Pools[0]
	if requireMetrics {
		if pool.MemoryBytes == nil || *pool.MemoryBytes == 0 || pool.CPUTimeNanosec == nil ||
			pool.Processes == nil || *pool.Processes == 0 {
			t.Fatalf("active PHP pool omitted aggregate accounting: %#v", pool)
		}
		return
	}
	if pool.MemoryBytes != nil || pool.CPUTimeNanosec != nil || pool.Processes != nil {
		t.Fatalf("missing PHP pool reported aggregate accounting: %#v", pool)
	}
}

func queueDisposableLifecycle(
	t *testing.T,
	repository *core.Repository,
	accountID core.ID,
	actorID core.ID,
	key string,
	payload operations.DomainLifecyclePayload,
) core.Operation {
	t.Helper()
	object, err := operations.NewDomainLifecyclePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := repository.CreateOperation(t.Context(), core.CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: operations.DomainLifecycleKind,
		RetryClass: core.RetrySafe, RequestID: "request-" + key,
		IdempotencyKey: key, Payload: object, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func queueDisposableAccountReconcile(
	t *testing.T,
	repository *core.Repository,
	account core.HostingAccount,
	actorID core.ID,
) core.Operation {
	t.Helper()
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		t.Fatal(err)
	}
	filesystem, err := repository.GetHostingFilesystemState(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := repository.GetHostingResourceState(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := operations.NewHostingAccountReconcilePayload(operations.HostingAccountReconcilePayload{
		Identity: identity, FilesystemRevision: filesystem.Revision,
		Storage:          hostingstorage.Spec{Identity: identity, ProjectID: filesystem.ProjectID},
		ResourceRevision: resources.Revision, Resources: hostingresources.Spec{Identity: identity},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := repository.CreateOperation(t.Context(), core.CreateOperationParams{
		AccountID: &account.ID, ActorID: &actorID, Kind: operations.HostingAccountReconcileKind,
		RetryClass: core.RetrySafe, RequestID: "vm-account-reconcile",
		IdempotencyKey: "vm-account-reconcile", Payload: payload, MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func runDisposableLifecycle(
	t *testing.T,
	runner *operations.Runner,
	repository *core.Repository,
	accountID core.ID,
	operationID core.ID,
) {
	t.Helper()
	if err := runner.RunOnce(t.Context()); err != nil {
		t.Fatalf("run lifecycle operation %s: %v", operationID, err)
	}
	operation, err := repository.GetOperation(t.Context(), core.OperationScope{
		AccountID: &accountID, OperationID: operationID,
	})
	if err != nil || operation.Status != core.OperationSucceeded {
		t.Fatalf("lifecycle operation %s = %#v, %v", operationID, operation, err)
	}
}

func writeAccountFile(t *testing.T, identity hostingidentity.Spec, relativePath, content string) {
	t.Helper()
	path := filepath.Join(identity.HomeDirectory, filepath.FromSlash(relativePath))
	command := exec.Command("/usr/sbin/runuser", "-u", identity.Username, "--", "/usr/bin/tee", path)
	command.Stdin = strings.NewReader(content)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("write account-owned static file: %v: %s", err, output)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != identity.UID || info.Mode().Perm()&0o600 != 0o600 || info.Mode().Perm()&0o007 != 0 {
		t.Fatalf("static file identity/mode = %#v / %o", stat, info.Mode().Perm())
	}
}

func cleanupLifecycleSELinuxContexts(t *testing.T, identity hostingidentity.Spec, relativePaths ...string) {
	t.Helper()
	if _, err := os.Stat("/usr/sbin/semanage"); err != nil {
		return
	}
	targetSet := map[string]struct{}{regexp.QuoteMeta(identity.HomeDirectory): {}}
	for _, relativePath := range relativePaths {
		components := strings.Split(relativePath, "/")
		for index := 1; index < len(components); index++ {
			targetSet[regexp.QuoteMeta(filepath.Join(identity.HomeDirectory,
				filepath.FromSlash(strings.Join(components[:index], "/"))))] = struct{}{}
		}
		targetSet[regexp.QuoteMeta(filepath.Join(identity.HomeDirectory,
			filepath.FromSlash(relativePath)))+"(/.*)?"] = struct{}{}
	}
	for target := range targetSet {
		_ = exec.Command("/usr/sbin/semanage", "fcontext", "-d", target).Run()
	}
}

func assertHostResponse(t *testing.T, host string, status int, contains string) {
	t.Helper()
	assertHostPathResponse(t, host, "/", status, contains)
}

func assertHostPathResponse(t *testing.T, host, requestPath string, status int, contains string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var content []byte
	for {
		content = requestHostPath(t, host, requestPath)
		if status == 0 {
			if len(content) == 0 {
				return
			}
		} else {
			statusLine := fmt.Sprintf("HTTP/1.1 %d ", status)
			if bytes.Contains(content, []byte(statusLine)) &&
				(contains == "" || bytes.Contains(content, []byte(contains))) {
				return
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status == 0 {
		t.Fatalf("Host %s should be rejected without response: %q", host, content)
	}
	t.Fatalf("Host %s response = %q, want status %d and %q", host, content, status, contains)
}

func assertHostRedirect(t *testing.T, host, requestPath string, status int, location string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		content := requestHostPath(t, host, requestPath)
		if bytes.Contains(content, []byte(fmt.Sprintf("HTTP/1.1 %d ", status))) &&
			bytes.Contains(content, []byte("Location: "+location+"\r\n")) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Host %s redirect response = %q, want status %d and Location %q",
				host, content, status, location)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func requestHostPath(t *testing.T, host, requestPath string) []byte {
	t.Helper()
	connection, err := net.DialTimeout("tcp", "127.0.0.1:80", 2*time.Second)
	if err != nil {
		t.Fatalf("connect for Host %s: %v", host, err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(connection, "GET "+requestPath+" HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write Host request %s: %v", host, err)
	}
	content, readErr := io.ReadAll(connection)
	if readErr != nil {
		t.Fatalf("read Host response %s: %v", host, readErr)
	}
	return content
}

func assertRenderedAccountConfigurationParses(t *testing.T) {
	t.Helper()
	const accountID = "019c1234-5678-7abc-8def-0123456789ad"
	username, err := hostingidentity.UsernameForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: 200_000, GID: 200_000, HomeDirectory: home,
	}
	staticName, err := core.NormalizeDomainName("static.stackfort.test")
	if err != nil {
		t.Fatal(err)
	}
	redirectName, err := core.NormalizeDomainName("redirect.stackfort.test")
	if err != nil {
		t.Fatal(err)
	}
	redirectURL, redirectHost, _, err := core.NormalizeRedirectURL(
		"https://destination.stackfort.test/base$stackfort_literal_marker?fixed=$also_literal",
	)
	if err != nil {
		t.Fatal(err)
	}
	domains := []core.Domain{
		{
			AccountID: core.ID(accountID), Name: staticName, Status: core.DomainActive,
			CanonicalMode: core.CanonicalServeBoth,
			TLS: core.DomainTLSState{
				Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
			},
			Target: core.DomainTarget{
				Type: core.DomainTargetStatic,
				DocumentRoot: &core.DocumentRoot{
					AccountID: core.ID(accountID), RelativePath: "public_html",
				},
			},
		},
		{
			AccountID: core.ID(accountID), Name: redirectName, Status: core.DomainActive,
			CanonicalMode: core.CanonicalPreferApex,
			TLS: core.DomainTLSState{
				Enabled: true, Mode: core.TLSModeACME, ChallengeType: core.TLSChallengeHTTP01,
			},
			Target: core.DomainTarget{
				Type: core.DomainTargetRedirect,
				Redirect: &core.DomainRedirect{
					StatusCode: core.RedirectPermanent, TargetURL: redirectURL,
					TargetASCIIHost: redirectHost, PreservePath: true,
					PreserveQuery: true, WildcardSubdomains: true,
				},
			},
		},
	}
	rendered, err := nginxconfig.RenderAccount(identity, domains, nginxconfig.DefaultOptions())
	if err != nil {
		t.Fatalf("render account NGINX configuration: %v", err)
	}
	temporary := t.TempDir()
	accountConfiguration := filepath.Join(temporary, rendered.FileName)
	if err := os.WriteFile(accountConfiguration, rendered.Content, 0o600); err != nil {
		t.Fatalf("write rendered account configuration: %v", err)
	}
	mainConfiguration := filepath.Join(temporary, "nginx.conf")
	mainContent := fmt.Sprintf(
		"pid %s;\nevents {}\nhttp {\n    include %s;\n}\n",
		filepath.Join(temporary, "nginx.pid"), accountConfiguration,
	)
	if err := os.WriteFile(mainConfiguration, []byte(mainContent), 0o600); err != nil {
		t.Fatalf("write NGINX syntax-test root: %v", err)
	}
	command := exec.Command("/usr/sbin/nginx", "-t", "-q", "-c", mainConfiguration)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("NGINX rejected typed renderer output: %v: %s\n%s", err, output, rendered.Content)
	}
}

func assertTransactionalSiteActivation(t *testing.T) {
	t.Helper()
	const accountID = "019c1234-5678-7abc-8def-0123456789ad"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: 200_000, GID: 200_000, HomeDirectory: home,
	}
	name, err := core.NormalizeDomainName("activation.stackfort.test")
	if err != nil {
		t.Fatal(err)
	}
	domains := []nginxconfig.DomainSpec{{
		Name: name, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth,
		Target: nginxconfig.TargetSpec{Type: core.DomainTargetStatic, DocumentRoot: "public_html"},
	}}
	firstRevision := mustUUIDv7(t)
	firstDesired := mustUUIDv7(t)
	firstSpec := hostnginx.ActivationSpec{
		Identity: identity, RevisionID: firstRevision, DesiredStateRevisionID: firstDesired,
		Domains: domains, Options: nginxconfig.DefaultOptions(),
	}
	first, err := hostnginx.NewActivator().Activate(t.Context(), firstSpec)
	if err != nil {
		t.Fatalf("activate first site revision: %v", err)
	}
	if !first.Changed || !first.ConfigurationTested || !first.ReloadPerformed ||
		!first.HealthChecked || first.ActiveRevisionID != firstRevision {
		t.Fatalf("first activation result = %#v", first)
	}

	// A fresh Activator models an agent process restart: durable host state, not
	// the in-memory RPC response cache, must make the same operation idempotent.
	second, err := hostnginx.NewActivator().Activate(t.Context(), firstSpec)
	if err != nil || second.Changed || second.ReloadPerformed || !second.HealthChecked {
		t.Fatalf("idempotent activation result = %#v, %v", second, err)
	}

	interruptedRevision := mustUUIDv7(t)
	interruptedDesired := mustUUIDv7(t)
	interruptedPath := filepath.Join(nginxbaseline.SiteRevisionsDirectory, interruptedRevision)
	if err := os.Mkdir(interruptedPath, 0o750); err != nil {
		t.Fatalf("create interrupted revision fixture: %v", err)
	}
	journal := struct {
		SchemaVersion          int    `json:"schemaVersion"`
		Phase                  string `json:"phase"`
		RevisionID             string `json:"revisionId"`
		PreviousRevisionID     string `json:"previousRevisionId"`
		AccountID              string `json:"accountId"`
		DesiredStateRevisionID string `json:"desiredStateRevisionId"`
	}{
		SchemaVersion: 1, Phase: "promoted", RevisionID: interruptedRevision,
		PreviousRevisionID: firstRevision, AccountID: accountID,
		DesiredStateRevisionID: interruptedDesired,
	}
	journalContent, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	journalContent = append(journalContent, '\n')
	journalFile, err := os.OpenFile(nginxbaseline.ActivationJournalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create interrupted journal fixture: %v", err)
	}
	if _, err := journalFile.Write(journalContent); err != nil {
		_ = journalFile.Close()
		t.Fatal(err)
	}
	if err := journalFile.Sync(); err != nil {
		_ = journalFile.Close()
		t.Fatal(err)
	}
	if err := journalFile.Close(); err != nil {
		t.Fatal(err)
	}
	temporaryLink := nginxbaseline.CurrentSitesLink + ".integration-" + interruptedRevision
	if err := os.Symlink(filepath.Join("site-revisions", interruptedRevision), temporaryLink); err != nil {
		t.Fatalf("create interrupted current link: %v", err)
	}
	if err := os.Rename(temporaryLink, nginxbaseline.CurrentSitesLink); err != nil {
		t.Fatalf("promote interrupted current link: %v", err)
	}

	finalRevision := mustUUIDv7(t)
	finalSpec := firstSpec
	finalSpec.RevisionID = finalRevision
	finalSpec.DesiredStateRevisionID = mustUUIDv7(t)
	final, err := hostnginx.NewActivator().Activate(t.Context(), finalSpec)
	if err != nil {
		t.Fatalf("recover and activate final site revision: %v", err)
	}
	if !final.Changed || !final.RecoveryPerformed || !final.ReloadPerformed ||
		final.ActiveRevisionID != finalRevision || final.PreviousRevisionID != firstRevision {
		t.Fatalf("recovered activation result = %#v", final)
	}
	if _, err := os.Lstat(nginxbaseline.ActivationJournalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("activation journal remains after commit: %v", err)
	}
	if _, err := os.Lstat(interruptedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted revision remains after recovery: %v", err)
	}
	include, info, err := readRootIntegrationFile(nginxbaseline.SitesCurrentIncludePath)
	if err != nil || info.Mode().Perm() != 0o640 || string(include) != nginxbaseline.SitesCurrentInclude() {
		t.Fatalf("active include is unsafe or changed: mode=%v err=%v content=%q", info, err, include)
	}
	linkInfo, err := os.Lstat(nginxbaseline.CurrentSitesLink)
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("active revision pointer is not a symlink: %v, %v", linkInfo, err)
	}
	target, err := os.Readlink(nginxbaseline.CurrentSitesLink)
	if err != nil || target != filepath.Join("site-revisions", finalRevision) {
		t.Fatalf("active revision pointer = %q, %v", target, err)
	}
	t.Log("STACKFORT_QUALIFICATION injected-nginx-promotion-recovery=passed")
}

func assertTLSCertificateStagingAndHTTPS(t *testing.T) {
	t.Helper()
	accountID := mustUUIDv7(t)
	username, err := hostingidentity.UsernameForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	home, err := hostingidentity.HomeDirectoryForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: 200_001, GID: 200_001, HomeDirectory: home,
	}
	host := "tls-" + strings.ReplaceAll(accountID[len(accountID)-12:], "-", "") + ".stackfort.test"
	bundle := disposableTLSBundle(t, mustUUIDv7(t), host)
	stager := hosttls.NewStager()
	first, err := stager.Stage(t.Context(), mustUUIDv7(t), bundle)
	if err != nil || !first.Changed || first.CertificateID != bundle.CertificateID {
		t.Fatalf("stage TLS certificate = %#v, %v", first, err)
	}
	second, err := stager.Stage(t.Context(), mustUUIDv7(t), bundle)
	if err != nil || second.Changed || second.CertificateID != bundle.CertificateID {
		t.Fatalf("idempotent TLS certificate stage = %#v, %v", second, err)
	}
	certificatePath, _ := tlsartifact.CertificatePath(bundle.CertificateID)
	privateKeyPath, _ := tlsartifact.PrivateKeyPath(bundle.CertificateID)
	assertRootOwnedMode(t, certificatePath, 0o644)
	assertRootOwnedMode(t, privateKeyPath, 0o600)

	name, err := core.NormalizeDomainName(host)
	if err != nil {
		t.Fatal(err)
	}
	redirectURL, redirectHost, _, err := core.NormalizeRedirectURL("https://destination.stackfort.test/secure")
	if err != nil {
		t.Fatal(err)
	}
	revisionID := mustUUIDv7(t)
	activation, err := hostnginx.NewActivator().Activate(t.Context(), hostnginx.ActivationSpec{
		Identity: identity, RevisionID: revisionID, DesiredStateRevisionID: mustUUIDv7(t),
		Domains: []nginxconfig.DomainSpec{{
			Name: name, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth,
			HTTP01Challenge: true, TLSCertificateID: bundle.CertificateID,
			Target: nginxconfig.TargetSpec{Type: core.DomainTargetRedirect, Redirect: &nginxconfig.RedirectSpec{
				StatusCode: core.RedirectPermanent, TargetURL: redirectURL, TargetASCIIHost: redirectHost,
			}},
		}}, Options: nginxconfig.DefaultOptions(),
	})
	if err != nil || !activation.ConfigurationTested || !activation.ReloadPerformed || !activation.HealthChecked {
		t.Fatalf("activate TLS site revision = %#v, %v", activation, err)
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	connection, err := tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
		ServerName: host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12, // test-only private CA
	})
	if err != nil {
		t.Fatalf("dial activated HTTPS host: %v", err)
	}
	defer connection.Close()
	if len(connection.ConnectionState().PeerCertificates) == 0 ||
		connection.ConnectionState().PeerCertificates[0].VerifyHostname(host) != nil {
		t.Fatalf("activated HTTPS host returned the wrong certificate")
	}
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(connection,
		"GET /probe HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(connection)
	if err != nil || !bytes.Contains(response, []byte("HTTP/1.1 301")) ||
		!bytes.Contains(response, []byte("Location: https://destination.stackfort.test/secure")) {
		t.Fatalf("HTTPS response = %q, %v", response, err)
	}
}

func disposableTLSBundle(t *testing.T, certificateID, host string) tlsartifact.Bundle {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Stackfort Disposable Test CA"},
		DNSNames: []string{host, "www." + host}, NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(24 * time.Hour), BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return tlsartifact.Bundle{
		CertificateID: certificateID, Names: []string{host, "www." + host},
		FullChainPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
	}
}

func mustUUIDv7(t *testing.T) string {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return value.String()
}

func readRootIntegrationFile(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	content, err := os.ReadFile(path)
	return content, info, err
}

func assertRootOwnedMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat managed NGINX path %s: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != mode || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("unsafe managed NGINX path %s: mode=%o metadata=%#v", path, info.Mode().Perm(), stat)
	}
}

func assertRejectingHTTPDefault(t *testing.T) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", "127.0.0.1:80", 2*time.Second)
	if err != nil {
		t.Fatalf("connect to NGINX HTTP default: %v", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(connection, "GET / HTTP/1.1\r\nHost: unassigned.stackfort.invalid\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatalf("write HTTP default request: %v", err)
	}
	content, _ := io.ReadAll(connection)
	if len(content) != 0 {
		t.Fatalf("rejecting HTTP default returned data: %q", content)
	}
}

func assertRejectingHTTPSDefault(t *testing.T) {
	t.Helper()
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	// #nosec G402 -- this negative integration probe expects the server to reject
	// the TLS handshake before certificate verification can occur.
	connection, err := tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
		ServerName: "unassigned.stackfort.invalid", InsecureSkipVerify: true,
		MinVersion: tls.VersionTLS12,
	})
	if err == nil {
		_ = connection.Close()
		t.Fatal("rejecting HTTPS default completed a TLS handshake")
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("HTTPS default timed out instead of rejecting the handshake: %v", err)
	}
}
