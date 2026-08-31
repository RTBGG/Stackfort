// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostacme"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/hostphp"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/hosttls"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/tlsartifact"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

type wafAttack struct {
	name    string
	suffix  string
	headers map[string]string
}

var wafAttackCorpus = []wafAttack{
	{name: "sqli", suffix: "?id=1%27%20OR%20%271%27%3D%271%27--"},
	{name: "xss", suffix: "?q=%3Cscript%3Ealert%281%3C%2Fscript%3E"},
	{name: "lfi", suffix: "?file=..%2F..%2F..%2Fetc%2Fpasswd"},
	{name: "command-injection", suffix: "?cmd=%3Bcat%20%2Fetc%2Fpasswd"},
	{name: "scanner", headers: map[string]string{"User-Agent": "sqlmap/1.8.0#stable"}},
}

type wafHTTPResult struct {
	status   int
	body     string
	location string
	tls      *tls.ConnectionState
}

type wafPerformanceComparison struct {
	Name                    string  `json:"name"`
	DetectionToOffRPSRatio  float64 `json:"detectionToOffRpsRatio"`
	BlockingToOffRPSRatio   float64 `json:"blockingToOffRpsRatio"`
	DetectionP99Microsecond int64   `json:"detectionP99Microseconds"`
	BlockingP99Microsecond  int64   `json:"blockingP99Microseconds"`
}

func TestDisposableHostWAFRuntimeAndPerformance(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host WAF integration test must run as root")
	}
	if _, err := hostnginx.NewReconciler().Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile NGINX baseline: %v", err)
	}
	securityStarted := time.Now().UTC().Add(-time.Second)
	assertWAFValidationFailurePreservesLiveRevision(t)

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

	filesystem := hostfilesystem.NewReconciler()
	accountClient := &localHostingAccountReconcileClient{
		test: t, identity: hostidentity.NewReconciler(), filesystem: filesystem,
		resources: hostresources.NewReconciler(),
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

	compactAccountID := strings.ReplaceAll(string(account.ID), "-", "")
	suffix := compactAccountID[len(compactAccountID)-12:]
	offHost := "waf-off-" + suffix + ".stackfort.test"
	detectionHost := "waf-detect-" + suffix + ".stackfort.test"
	blockingHost := "waf-block-" + suffix + ".stackfort.test"
	phpHost := "waf-php-" + suffix + ".stackfort.test"
	redirectHost := "waf-redirect-" + suffix + ".stackfort.test"

	off := core.WAFModeOff
	detection := core.WAFModeDetectionOnly
	blocking := core.WAFModeBlockingPL1
	createStatic := func(key, host string, mode *core.WAFMode) core.Operation {
		operation := queueDisposableLifecycle(t, repository, account.ID, owner.ID, key,
			operations.DomainLifecyclePayload{
				Action: operations.DomainLifecycleCreate, Name: host,
				Target: &core.DomainTargetSpec{Type: core.DomainTargetStatic}, WAFMode: mode,
			})
		runDisposableLifecycle(t, runner, repository, account.ID, operation.ID)
		return operation
	}
	createStatic("vm-waf-create-off", offHost, &off)
	createStatic("vm-waf-create-detection", detectionHost, &detection)
	blockingDomain := createStatic("vm-waf-create-blocking", blockingHost, &blocking)
	writeAccountFile(t, identity, "public_html/index.html", "stackfort-waf-static-ok\n")

	phpVersion := nativePHPVersion(t)
	phpDomain := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-waf-create-php",
		operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleCreate, Name: phpHost,
			Target:  &core.DomainTargetSpec{Type: core.DomainTargetPHP, PHPVersion: phpVersion},
			WAFMode: &off,
		})
	runDisposableLifecycle(t, runner, repository, account.ID, phpDomain.ID)
	writeAccountFile(t, identity, "public_html/index.php", `<?php
header('Content-Type: text/plain');
echo "stackfort-waf-php-ok\n";
`)

	redirectDomain := queueDisposableLifecycle(t, repository, account.ID, owner.ID, "vm-waf-create-redirect",
		operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleCreate, Name: redirectHost,
			Target: &core.DomainTargetSpec{Type: core.DomainTargetRedirect, Redirect: &core.RedirectSpec{
				StatusCode: core.RedirectTemporary, TargetURL: "https://destination.stackfort.test/waf",
			}},
			WAFMode: &blocking,
		})
	runDisposableLifecycle(t, runner, repository, account.ID, redirectDomain.ID)
	assertActiveWAFProfile(t, account.ID, offHost, "")
	assertActiveWAFProfile(t, account.ID, detectionHost, wafconfig.DetectionPL1Path)
	assertActiveWAFProfile(t, account.ID, blockingHost, wafconfig.BlockingPL1Path)

	assertWAFBenignResponse(t, offHost, "/index.html?name=stackfort", http.StatusOK, "stackfort-waf-static-ok")
	assertWAFCorpus(t, offHost, "/index.html", http.StatusOK, "stackfort-waf-static-ok", "")
	assertNoWAFEvents(t, wafErrorPath(account.ID, offHost))

	assertWAFBenignResponse(t, detectionHost, "/index.html?name=stackfort", http.StatusOK, "stackfort-waf-static-ok")
	assertWAFCorpus(t, detectionHost, "/index.html", http.StatusOK, "stackfort-waf-static-ok", "")

	assertWAFBenignResponse(t, blockingHost, "/index.html?name=stackfort", http.StatusOK, "stackfort-waf-static-ok")
	assertWAFCorpus(t, blockingHost, "/index.html", http.StatusForbidden, "", "")
	waitForWAFEvents(t, wafErrorPath(account.ID, blockingHost), len(wafAttackCorpus))

	phpLog := wafErrorPath(account.ID, phpHost)
	assertWAFBenignResponse(t, phpHost, "/index.php?name=stackfort", http.StatusOK, "stackfort-waf-php-ok")
	assertWAFCorpus(t, phpHost, "/index.php", http.StatusOK, "stackfort-waf-php-ok", "")
	assertNoWAFEvents(t, phpLog)
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, phpDomain.ID, "vm-waf-php-detection", detection)
	assertActiveWAFProfile(t, account.ID, phpHost, wafconfig.DetectionPL1Path)
	assertWAFCorpus(t, phpHost, "/index.php", http.StatusOK, "stackfort-waf-php-ok", "")
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, phpDomain.ID, "vm-waf-php-blocking", blocking)
	assertActiveWAFProfile(t, account.ID, phpHost, wafconfig.BlockingPL1Path)
	assertWAFCorpus(t, phpHost, "/index.php", http.StatusForbidden, "", "")
	waitForWAFEvents(t, phpLog, len(wafAttackCorpus))

	assertWAFBenignResponse(t, redirectHost, "/benign", int(core.RedirectTemporary), "")
	assertWAFCorpus(t, redirectHost, "/", http.StatusForbidden, "", "")
	waitForWAFEvents(t, wafErrorPath(account.ID, redirectHost), len(wafAttackCorpus))

	assertWAFACMEBypass(t, blockingHost)
	offMetric := measureHTTPBaseline(t, "waf-static-off", "http://127.0.0.1/index.html?probe=1", offHost, 3_000, 8)
	detectionMetric := measureHTTPBaseline(t, "waf-static-detection", "http://127.0.0.1/index.html?probe=1", detectionHost, 3_000, 8)
	blockingMetric := measureHTTPBaseline(t, "waf-static-blocking-pl1", "http://127.0.0.1/index.html?probe=1", blockingHost, 3_000, 8)
	comparison := wafPerformanceComparison{
		Name:                    "waf-static-mode-overhead",
		DetectionToOffRPSRatio:  detectionMetric.RequestsPerSecond / offMetric.RequestsPerSecond,
		BlockingToOffRPSRatio:   blockingMetric.RequestsPerSecond / offMetric.RequestsPerSecond,
		DetectionP99Microsecond: detectionMetric.P99Microseconds,
		BlockingP99Microsecond:  blockingMetric.P99Microseconds,
	}
	encodedComparison, err := json.Marshal(comparison)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("STACKFORT_PERFORMANCE %s", encodedComparison)
	if detectionMetric.RequestsPerSecond < 250 || blockingMetric.RequestsPerSecond < 250 ||
		detectionMetric.P99Microseconds > (100*time.Millisecond).Microseconds() ||
		blockingMetric.P99Microseconds > (100*time.Millisecond).Microseconds() ||
		comparison.DetectionToOffRPSRatio < 0.01 || comparison.BlockingToOffRPSRatio < 0.01 {
		t.Fatalf("gross WAF throughput regression: %s", encodedComparison)
	}

	assertWAFTLS(t, repository, identity, blockingDomain.ID, blockingHost)
	assertWAFMandatoryAccessControl(t, securityStarted)

	retired, err := hostnginx.NewActivator().Activate(t.Context(), hostnginx.ActivationSpec{
		Identity: identity, RevisionID: mustUUIDv7(t), DesiredStateRevisionID: mustUUIDv7(t),
		Domains: []nginxconfig.DomainSpec{}, Options: nginxconfig.DefaultOptions(),
	})
	if err != nil || !retired.ReloadPerformed {
		t.Fatalf("retire disposable WAF account configuration = %#v, %v", retired, err)
	}
	if _, err := client.php.Reconcile(t.Context(), phpruntime.PoolSetSpec{
		Identity: identity, Versions: []string{}, MaxChildren: phpruntime.DefaultMaxChildren,
		MemoryLimitMiB: phpruntime.DefaultMemoryMiB, RetireAbsent: true,
	}); err != nil {
		t.Fatalf("retire disposable WAF PHP pool: %v", err)
	}
	assertPHPAccountPoolRetired(t, phpVersion, identity)
	t.Log("STACKFORT_QUALIFICATION waf-runtime-attack-corpus=passed")
}

func assertWAFValidationFailurePreservesLiveRevision(t *testing.T) {
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
		AccountID: accountID, Username: username, UID: 200_010, GID: 200_010, HomeDirectory: home,
	}
	host := "waf-rollback-" + strings.ReplaceAll(accountID[len(accountID)-12:], "-", "") + ".stackfort.test"
	name, err := core.NormalizeDomainName(host)
	if err != nil {
		t.Fatal(err)
	}
	target, targetHost, _, err := core.NormalizeRedirectURL("https://destination.stackfort.test/live")
	if err != nil {
		t.Fatal(err)
	}
	makeSpec := func(mode core.WAFMode) hostnginx.ActivationSpec {
		return hostnginx.ActivationSpec{
			Identity: identity, RevisionID: mustUUIDv7(t), DesiredStateRevisionID: mustUUIDv7(t),
			Domains: []nginxconfig.DomainSpec{{
				Name: name, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth, WAFMode: mode,
				Target: nginxconfig.TargetSpec{Type: core.DomainTargetRedirect, Redirect: &nginxconfig.RedirectSpec{
					StatusCode: core.RedirectTemporary, TargetURL: target, TargetASCIIHost: targetHost,
				}},
			}}, Options: nginxconfig.DefaultOptions(),
		}
	}
	activator := hostnginx.NewActivator()
	activeSpec := makeSpec(core.WAFModeOff)
	active, err := activator.Activate(t.Context(), activeSpec)
	if err != nil || !active.ReloadPerformed {
		t.Fatalf("activate WAF rollback fixture = %#v, %v", active, err)
	}
	assertWAFBenignResponse(t, host, "/live", int(core.RedirectTemporary), "")
	beforeLink, err := os.Readlink(nginxbaseline.CurrentSitesLink)
	if err != nil {
		t.Fatal(err)
	}

	original, err := os.ReadFile(wafconfig.BlockingPL1Path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(wafconfig.BlockingPL1Path)
	if err != nil {
		t.Fatal(err)
	}
	restored := false
	t.Cleanup(func() {
		if !restored {
			_ = os.WriteFile(wafconfig.BlockingPL1Path, original, info.Mode().Perm())
		}
	})
	if err := os.WriteFile(wafconfig.BlockingPL1Path,
		[]byte("SecRuleEngine On\nInclude /etc/nginx/stackfort/coraza/does-not-exist.conf\n"),
		info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	failedSpec := makeSpec(core.WAFModeBlockingPL1)
	// coraza-nginx compiles file-backed SecLang only after the worker fork;
	// nginx -t therefore accepts this candidate and the activation health
	// checkpoint must detect the fatal worker initialization instead.
	if _, err := activator.Activate(t.Context(), failedSpec); !errors.Is(err, hostnginx.ErrHealthCheckFailed) {
		t.Fatalf("invalid WAF candidate error = %v", err)
	}
	if err := os.WriteFile(wafconfig.BlockingPL1Path, original, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	restored = true
	afterLink, err := os.Readlink(nginxbaseline.CurrentSitesLink)
	if err != nil || afterLink != beforeLink {
		t.Fatalf("active revision changed after rejected WAF candidate: before=%q after=%q err=%v",
			beforeLink, afterLink, err)
	}
	if _, err := os.Lstat(filepath.Join(nginxbaseline.SiteRevisionsDirectory, failedSpec.RevisionID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected WAF candidate remains: %v", err)
	}
	if output, err := exec.Command("/usr/sbin/nginx", "-t", "-q", "-c", nginxbaseline.MainConfiguration).CombinedOutput(); err != nil {
		t.Fatalf("restored live NGINX configuration is invalid: %v: %s", err, output)
	}
	assertWAFBenignResponse(t, host, "/live", int(core.RedirectTemporary), "")
	empty := hostnginx.ActivationSpec{
		Identity: identity, RevisionID: mustUUIDv7(t), DesiredStateRevisionID: mustUUIDv7(t),
		Domains: []nginxconfig.DomainSpec{}, Options: nginxconfig.DefaultOptions(),
	}
	if result, err := activator.Activate(t.Context(), empty); err != nil || !result.ReloadPerformed {
		t.Fatalf("retire WAF rollback fixture = %#v, %v", result, err)
	}
	t.Log("STACKFORT_QUALIFICATION waf-reload-failure-rollback=passed")
}

func editPHPWAFMode(
	t *testing.T,
	runner *operations.Runner,
	repository *core.Repository,
	accountID core.ID,
	ownerID core.ID,
	domainID core.ID,
	key string,
	mode core.WAFMode,
) {
	t.Helper()
	operation := queueDisposableLifecycle(t, repository, accountID, ownerID, key,
		operations.DomainLifecyclePayload{
			Action: operations.DomainLifecycleEdit, DomainID: string(domainID), WAFMode: &mode,
		})
	runDisposableLifecycle(t, runner, repository, accountID, operation.ID)
}

func assertWAFCorpus(t *testing.T, host, basePath string, status int, body, location string) {
	t.Helper()
	for _, attack := range wafAttackCorpus {
		result := requestWAFHTTP(t, "http", host, basePath+attack.suffix, attack.headers)
		if result.status != status || body != "" && !strings.Contains(result.body, body) ||
			location != "" && result.location != location {
			t.Fatalf("WAF %s request for %s = status %d body %q location %q, want %d/%q/%q",
				attack.name, host, result.status, result.body, result.location, status, body, location)
		}
	}
}

func assertWAFBenignResponse(t *testing.T, host, path string, status int, body string) {
	t.Helper()
	result := requestWAFHTTP(t, "http", host, path, map[string]string{
		"User-Agent": "Stackfort-Qualification/1.0",
	})
	if result.status != status || body != "" && !strings.Contains(result.body, body) {
		t.Fatalf("benign WAF request for %s = status %d body %q, want %d/%q",
			host, result.status, result.body, status, body)
	}
}

func requestWAFHTTP(t *testing.T, scheme, host, path string, headers map[string]string) wafHTTPResult {
	t.Helper()
	address := "127.0.0.1:80"
	transport := &http.Transport{Proxy: nil, DisableCompression: true, ForceAttemptHTTP2: false}
	if scheme == "https" {
		address = "127.0.0.1:443"
		transport.TLSClientConfig = &tls.Config{
			ServerName: host, MinVersion: tls.VersionTLS12,
			InsecureSkipVerify: true, // #nosec G402 -- disposable self-signed integration certificate; SAN is checked below.
		}
	}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, scheme+"://"+host+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request %s://%s%s: %v", scheme, host, path, err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return wafHTTPResult{
		status: response.StatusCode, body: string(content), location: response.Header.Get("Location"), tls: response.TLS,
	}
}

func wafErrorPath(accountID core.ID, host string) string {
	return hostinglogs.DomainFile(string(accountID), host, "error")
}

func assertActiveWAFProfile(t *testing.T, accountID core.ID, host, profile string) {
	t.Helper()
	target, err := os.Readlink(nginxbaseline.CurrentSitesLink)
	if err != nil {
		t.Fatal(err)
	}
	configuration := filepath.Join(filepath.Dir(nginxbaseline.CurrentSitesLink), target,
		"account-"+string(accountID)+".conf")
	content, err := os.ReadFile(configuration)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, block := range strings.Split(string(content), "server {") {
		if !strings.Contains(block, "server_name ") || !strings.Contains(block, host) {
			continue
		}
		found++
		if profile == "" {
			if strings.Contains(block, "coraza on;") || strings.Contains(block, "coraza_rules_file") ||
				strings.Contains(block, "coraza_transaction_id") {
				t.Fatalf("WAF-off server for %s enables Coraza", host)
			}
			continue
		}
		if !strings.Contains(block, "coraza on;") ||
			!strings.Contains(block, "coraza_transaction_id $request_id;") ||
			!strings.Contains(block, `coraza_rules_file "`+profile+`";`) {
			t.Fatalf("active server for %s does not use fixed WAF profile %s", host, profile)
		}
	}
	if found == 0 {
		t.Fatalf("active NGINX configuration has no server for %s", host)
	}
}

func assertNoWAFEvents(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(content)), "coraza:") {
		t.Fatalf("WAF-off domain emitted Coraza events in %s", path)
	}
}

func waitForWAFEvents(t *testing.T, path string, minimum int) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		content, err := os.ReadFile(path)
		if err == nil {
			count := strings.Count(strings.ToLower(string(content)), "coraza:")
			if count >= minimum {
				return count
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("WAF log %s did not reach %d Coraza events: %v", path, minimum, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertWAFACMEBypass(t *testing.T, host string) {
	t.Helper()
	token := strings.ReplaceAll(mustUUIDv7(t), "-", "")
	keyAuthorization := token + "." + strings.Repeat("a", 43)
	presenter := hostacme.NewPresenter()
	t.Cleanup(func() {
		_, _ = presenter.Reconcile(context.Background(), mustUUIDv7(t), acmehttp01.Intent{
			Action: acmehttp01.ActionCleanup, Token: token,
		})
	})
	if result, err := presenter.Reconcile(t.Context(), mustUUIDv7(t), acmehttp01.Intent{
		Action: acmehttp01.ActionPresent, Token: token, KeyAuthorization: keyAuthorization,
	}); err != nil || !result.Presented {
		t.Fatalf("present blocking-domain ACME challenge = %#v, %v", result, err)
	}
	path := "/.well-known/acme-challenge/" + token + wafAttackCorpus[0].suffix
	response := requestWAFHTTP(t, "http", host, path, nil)
	if response.status != http.StatusOK || strings.TrimSpace(response.body) != keyAuthorization {
		t.Fatalf("blocking-domain ACME bypass = status %d body %q", response.status, response.body)
	}
	if result, err := presenter.Reconcile(t.Context(), mustUUIDv7(t), acmehttp01.Intent{
		Action: acmehttp01.ActionCleanup, Token: token,
	}); err != nil || result.Presented {
		t.Fatalf("cleanup blocking-domain ACME challenge = %#v, %v", result, err)
	}
	t.Log("STACKFORT_QUALIFICATION waf-acme-bypass=passed")
}

func assertWAFTLS(
	t *testing.T,
	repository *core.Repository,
	identity hostingidentity.Spec,
	domainID core.ID,
	host string,
) {
	t.Helper()
	bundle := disposableTLSBundle(t, mustUUIDv7(t), host)
	directory := filepath.Dir(filepath.Dir(mustTLSArtifactPath(t, bundle.CertificateID)))
	t.Cleanup(func() {
		candidate := filepath.Join(directory, bundle.CertificateID)
		if filepath.Dir(candidate) == directory {
			_ = os.RemoveAll(candidate)
		}
	})
	if result, err := hosttls.NewStager().Stage(t.Context(), mustUUIDv7(t), bundle); err != nil || !result.Changed {
		t.Fatalf("stage WAF TLS certificate = %#v, %v", result, err)
	}
	domains, err := repository.ListDomains(t.Context(), core.ID(identity.AccountID), false)
	if err != nil {
		t.Fatal(err)
	}
	specs, err := nginxconfig.SpecsFromDomains(identity, domains, nginxconfig.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index := range specs {
		if specs[index].Name.ASCII == host {
			if string(domains[index].ID) == string(domainID) || specs[index].WAFMode == core.WAFModeBlockingPL1 {
				specs[index].TLSCertificateID = bundle.CertificateID
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("blocking TLS domain %s is absent from desired state", domainID)
	}
	activation, err := hostnginx.NewActivator().Activate(t.Context(), hostnginx.ActivationSpec{
		Identity: identity, RevisionID: mustUUIDv7(t), DesiredStateRevisionID: mustUUIDv7(t),
		Domains: specs, Options: nginxconfig.DefaultOptions(),
	})
	if err != nil || !activation.ReloadPerformed || !activation.HealthChecked {
		t.Fatalf("activate blocking TLS WAF domain = %#v, %v", activation, err)
	}
	benign := requestWAFHTTP(t, "https", host, "/index.html?name=stackfort", nil)
	if benign.status != http.StatusOK || !strings.Contains(benign.body, "stackfort-waf-static-ok") ||
		benign.tls == nil || len(benign.tls.PeerCertificates) == 0 ||
		benign.tls.PeerCertificates[0].VerifyHostname(host) != nil {
		t.Fatalf("benign HTTPS WAF response = status %d body %q tls=%#v", benign.status, benign.body, benign.tls)
	}
	attack := requestWAFHTTP(t, "https", host, "/index.html"+wafAttackCorpus[0].suffix, nil)
	if attack.status != http.StatusForbidden {
		t.Fatalf("blocking HTTPS WAF attack status = %d, want 403", attack.status)
	}
	t.Log("STACKFORT_QUALIFICATION waf-tls=passed")
}

func mustTLSArtifactPath(t *testing.T, certificateID string) string {
	t.Helper()
	path, err := tlsartifact.CertificatePath(certificateID)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertWAFMandatoryAccessControl(t *testing.T, started time.Time) {
	t.Helper()
	provider := "apparmor"
	if _, err := os.Stat("/usr/sbin/getenforce"); err == nil {
		provider = "selinux"
		mode, err := exec.Command("/usr/sbin/getenforce").Output()
		if err != nil || strings.TrimSpace(string(mode)) != "Enforcing" {
			t.Fatalf("SELinux mode = %q, %v", mode, err)
		}
		for _, path := range []string{wafconfig.RuntimeRoot, wafconfig.PersistentRoot} {
			label, err := exec.Command("/usr/bin/ls", "-Zd", path).Output()
			if err != nil || !strings.Contains(string(label), "httpd_cache_t") {
				t.Fatalf("WAF SELinux label for %s = %q, %v", path, label, err)
			}
		}
	} else {
		enabled, err := os.ReadFile("/sys/module/apparmor/parameters/enabled")
		if err != nil || !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(string(enabled))), "Y") {
			t.Fatalf("AppArmor is not enabled: %q, %v", enabled, err)
		}
	}
	output, err := exec.Command("/usr/bin/journalctl", "--since", "@"+strconv.FormatInt(started.Unix(), 10),
		"--no-pager", "-o", "cat").Output()
	if err != nil {
		t.Fatalf("read mandatory-access-control journal: %v", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		lower := strings.ToLower(line)
		denied := strings.Contains(lower, "apparmor=\"denied\"") || strings.Contains(lower, "avc:  denied")
		scoped := strings.Contains(lower, "coraza") || strings.Contains(lower, "comm=\"nginx\"") ||
			strings.Contains(lower, strings.ToLower(wafconfig.RuntimeRoot))
		if denied && scoped {
			t.Fatalf("mandatory access control denied the WAF/NGINX runtime")
		}
	}
	t.Logf("STACKFORT_QUALIFICATION waf-mandatory-access-control=passed provider=%s", provider)
}
