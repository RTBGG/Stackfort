// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostcache"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingresources"
	"github.com/RTBGG/stackfort/internal/hostlogs"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/hostphp"
	"github.com/RTBGG/stackfort/internal/hostresources"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/RTBGG/stackfort/internal/phpruntime"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

type cacheHTTPResult struct {
	status   int
	body     string
	decision string
	headers  http.Header
}

type cachePerformanceComparison struct {
	Name                  string  `json:"name"`
	WAFMode               string  `json:"wafMode"`
	DirectRPS             float64 `json:"directRps"`
	NGINXFastCGICacheRPS  float64 `json:"nginxFastcgiCacheRps"`
	VinylRPS              float64 `json:"vinylRps"`
	VinylToFastCGIRatio   float64 `json:"vinylToFastcgiRatio"`
	NGINXFastCGIP99Micros int64   `json:"nginxFastcgiP99Microseconds"`
	VinylP99Micros        int64   `json:"vinylP99Microseconds"`
}

func TestDisposableHostVinylCacheSafetyWAFAndPerformance(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host cache integration test must run as root")
	}
	if _, err := hostnginx.NewReconciler().Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile NGINX baseline: %v", err)
	}
	assertVinylRuntime(t)

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
	domainClient := &localDomainLifecycleClient{
		test: t, filesystem: filesystem, nginx: hostnginx.NewActivator(), php: hostphp.NewReconciler(),
	}
	domainHandler, err := operations.NewDomainLifecycleHandler(repository, domainClient)
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

	compactID := strings.ReplaceAll(string(account.ID), "-", "")
	suffix := compactID[len(compactID)-12:]
	directHost := "cache-direct-" + suffix + ".stackfort.test"
	vinylHost := "cache-vinyl-" + suffix + ".stackfort.test"
	secondHost := "cache-second-" + suffix + ".stackfort.test"
	respectHost := "cache-origin-" + suffix + ".stackfort.test"
	protectedHost := "cache-waf-" + suffix + ".stackfort.test"
	phpVersion := nativePHPVersion(t)
	off := core.WAFModeOff
	detection := core.WAFModeDetectionOnly
	blocking := core.WAFModeBlockingPL1
	disabled := core.CachePresetDisabled
	wordpress := core.CachePresetWordPress
	respectOrigin := core.CachePresetRespectOrigin
	createPHP := func(key, host string, waf *core.WAFMode, cache *core.CachePreset) core.Operation {
		operation := queueDisposableLifecycle(t, repository, account.ID, owner.ID, key,
			operations.DomainLifecyclePayload{
				Action: operations.DomainLifecycleCreate, Name: host,
				Target:  &core.DomainTargetSpec{Type: core.DomainTargetPHP, PHPVersion: phpVersion},
				WAFMode: waf, CachePreset: cache,
			})
		runDisposableLifecycle(t, runner, repository, account.ID, operation.ID)
		return operation
	}
	directDomain := createPHP("vm-cache-direct", directHost, &off, &disabled)
	vinylDomain := createPHP("vm-cache-vinyl", vinylHost, &off, &wordpress)
	createPHP("vm-cache-second", secondHost, &off, &wordpress)
	createPHP("vm-cache-origin", respectHost, &off, &respectOrigin)
	protectedDomain := createPHP("vm-cache-waf", protectedHost, &blocking, &wordpress)

	phpSource := `<?php
$path = parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH);
if ($path === '/set-cookie.php') {
    header('Set-Cookie: stackfort_session=' . bin2hex(random_bytes(4)) . '; Path=/; HttpOnly; SameSite=Lax');
} elseif ($path === '/private.php') {
    header('Cache-Control: private, no-store');
} elseif ($path !== '/no-expiry.php') {
    header('Cache-Control: public, max-age=120');
}
header('Content-Type: text/plain');
echo $_SERVER['HTTP_HOST'], '|', $path, '|', bin2hex(random_bytes(8)), "\n";
`
	for _, name := range []string{"index.php", "other.php", "set-cookie.php", "private.php", "no-expiry.php"} {
		writeAccountFile(t, identity, "public_html/"+name, phpSource)
	}
	wpAdmin := filepath.Join(identity.HomeDirectory, "public_html/wp-admin")
	if err := runAs(identity, "/usr/bin/mkdir", "-p", wpAdmin); err != nil {
		t.Fatalf("create WordPress administration fixture: %v", err)
	}
	writeAccountFile(t, identity, "public_html/wp-admin/index.php", phpSource)

	first := requestCache(t, vinylHost, http.MethodGet, "/index.php?case=anonymous", nil)
	second := requestCache(t, vinylHost, http.MethodGet, "/index.php?case=anonymous", nil)
	assertCacheDecision(t, first, http.StatusOK, "MISS")
	assertCacheDecision(t, second, http.StatusOK, "HIT")
	if first.body != second.body {
		t.Fatal("anonymous Vinyl hit did not preserve the cached representation")
	}

	assertNeverCached(t, vinylHost, http.MethodGet, "/index.php?case=cookie",
		map[string]string{"Cookie": "session=account-a"})
	assertNeverCached(t, vinylHost, http.MethodGet, "/index.php?case=authorization",
		map[string]string{"Authorization": "Bearer account-a"})
	assertNeverCached(t, vinylHost, http.MethodPost, "/index.php?case=post", nil)
	assertNeverCached(t, vinylHost, http.MethodGet, "/wp-admin/index.php?case=sensitive", nil)
	assertNeverStored(t, vinylHost, "/set-cookie.php?case=set-cookie")
	assertNeverStored(t, vinylHost, "/private.php?case=private")

	originFirst := requestCache(t, respectHost, http.MethodGet, "/index.php?case=origin", nil)
	originSecond := requestCache(t, respectHost, http.MethodGet, "/index.php?case=origin", nil)
	assertCacheDecision(t, originFirst, http.StatusOK, "MISS")
	assertCacheDecision(t, originSecond, http.StatusOK, "HIT")
	assertNeverStored(t, respectHost, "/no-expiry.php?case=no-origin-ttl")

	isolatedFirst := requestCache(t, vinylHost, http.MethodGet, "/index.php?case=host-isolation", nil)
	isolatedSecond := requestCache(t, secondHost, http.MethodGet, "/index.php?case=host-isolation", nil)
	assertCacheDecision(t, isolatedFirst, http.StatusOK, "MISS")
	assertCacheDecision(t, isolatedSecond, http.StatusOK, "MISS")
	if !strings.HasPrefix(isolatedFirst.body, vinylHost+"|") ||
		!strings.HasPrefix(isolatedSecond.body, secondHost+"|") || isolatedFirst.body == isolatedSecond.body {
		t.Fatalf("cache host-key isolation failed: %q / %q", isolatedFirst.body, isolatedSecond.body)
	}
	assertCacheDecision(t, requestCache(t, secondHost, http.MethodGet,
		"/index.php?case=host-isolation", nil), http.StatusOK, "HIT")

	const wafAttackQuery = "?lookup=1%20OR%201=1"
	assertWAFSQLiStatus(t, protectedHost, "/index.php"+wafAttackQuery, http.StatusForbidden)
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, protectedDomain.ID,
		"vm-cache-waf-detection", detection)
	assertWAFSQLiStatus(t, protectedHost, "/index.php"+wafAttackQuery, http.StatusOK)
	detectedRuleIDs := waitForDetectedWAFRuleIDs(t, identity, protectedHost, "/index.php")
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, protectedDomain.ID,
		"vm-cache-waf-blocking", blocking)
	exceptions := make([]core.Operation, 0, len(detectedRuleIDs))
	for index, ruleID := range detectedRuleIDs {
		exception := queueDisposableLifecycle(t, repository, account.ID, owner.ID,
			fmt.Sprintf("vm-cache-waf-exception-%d", index), operations.DomainLifecyclePayload{
				Action: operations.DomainLifecycleCreateWAFException, DomainID: string(protectedDomain.ID),
				WAFException: &operations.WAFExceptionIntent{
					RuleID: ruleID, RequestPath: "/index.php", ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
				},
			})
		runDisposableLifecycle(t, runner, repository, account.ID, exception.ID)
		exceptions = append(exceptions, exception)
	}
	assertWAFSQLiStatus(t, protectedHost, "/index.php"+wafAttackQuery, http.StatusOK)
	assertWAFSQLiStatus(t, protectedHost, "/other.php"+wafAttackQuery, http.StatusForbidden)
	for index, exception := range exceptions {
		removeException := queueDisposableLifecycle(t, repository, account.ID, owner.ID,
			fmt.Sprintf("vm-cache-waf-exception-remove-%d", index), operations.DomainLifecyclePayload{
				Action: operations.DomainLifecycleRemoveWAFException, DomainID: string(protectedDomain.ID),
				WAFExceptionID: string(exception.ID),
			})
		runDisposableLifecycle(t, runner, repository, account.ID, removeException.ID)
	}
	assertWAFSQLiStatus(t, protectedHost, "/index.php"+wafAttackQuery, http.StatusForbidden)

	manager := hostcache.NewManager(nil)
	purgePath := "/index.php"
	purgeQuery := purgePath + "?case=purge"
	assertCacheDecision(t, requestCache(t, vinylHost, http.MethodGet, purgeQuery, nil), http.StatusOK, "MISS")
	assertCacheDecision(t, requestCache(t, vinylHost, http.MethodGet, purgeQuery, nil), http.StatusOK, "HIT")
	purged, err := manager.Purge(t.Context(), agentprotocol.CachePurgeRequest{
		Identity: identity, DomainASCII: vinylHost, PathPrefix: purgePath,
	})
	if err != nil || !purged.Accepted || purged.PathPrefix != purgePath {
		t.Fatalf("scoped Vinyl purge = %#v / %v", purged, err)
	}
	assertCacheDecision(t, requestCache(t, vinylHost, http.MethodGet, purgeQuery, nil), http.StatusOK, "MISS")
	metrics, err := manager.Metrics(t.Context(), agentprotocol.CacheMetricsRequest{
		Identity: identity, DomainASCII: vinylHost,
	})
	if err != nil || metrics.Hits == 0 || metrics.Misses == 0 || metrics.Bypasses == 0 ||
		metrics.WindowRecords != metrics.Hits+metrics.Misses+metrics.Bypasses {
		t.Fatalf("log-derived cache metrics = %#v / %v", metrics, err)
	}

	measureMode := func(name, label string, mode core.WAFMode) cachePerformanceComparison {
		directMetric := measureHTTPBaseline(t, "cache-direct-"+label,
			"http://127.0.0.1/index.php?benchmark=direct-"+label, directHost, 3_000, 8)
		fastCGIMetric := measureFastCGICacheBaseline(t, identity, phpVersion, mode, label)
		vinylMetric := measureHTTPBaseline(t, "cache-vinyl-"+label,
			"http://127.0.0.1/index.php?benchmark=vinyl-"+label, vinylHost, 3_000, 8)
		comparison := cachePerformanceComparison{
			Name: name, WAFMode: string(mode), DirectRPS: directMetric.RequestsPerSecond,
			NGINXFastCGICacheRPS: fastCGIMetric.RequestsPerSecond, VinylRPS: vinylMetric.RequestsPerSecond,
			VinylToFastCGIRatio:   vinylMetric.RequestsPerSecond / fastCGIMetric.RequestsPerSecond,
			NGINXFastCGIP99Micros: fastCGIMetric.P99Microseconds, VinylP99Micros: vinylMetric.P99Microseconds,
		}
		encoded, marshalErr := json.Marshal(comparison)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		t.Logf("STACKFORT_PERFORMANCE %s", encoded)
		return comparison
	}
	offComparison := measureMode("php-cache-comparison", "waf-off", off)
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, directDomain.ID,
		"vm-cache-direct-waf-detection", detection)
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, vinylDomain.ID,
		"vm-cache-vinyl-waf-detection", detection)
	detectionComparison := measureMode("php-cache-waf-detection-comparison", "waf-detection", detection)
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, directDomain.ID,
		"vm-cache-direct-waf-blocking", blocking)
	editPHPWAFMode(t, runner, repository, account.ID, owner.ID, vinylDomain.ID,
		"vm-cache-vinyl-waf-blocking", blocking)
	blockingComparison := measureMode("php-cache-waf-blocking-comparison", "waf-blocking", blocking)
	if offComparison.WAFMode != string(off) || detectionComparison.WAFMode != string(detection) ||
		blockingComparison.WAFMode != string(blocking) {
		t.Fatal("cache performance matrix contains an unexpected WAF mode")
	}

	retired, err := hostnginx.NewActivator().Activate(t.Context(), hostnginx.ActivationSpec{
		Identity: identity, RevisionID: mustUUIDv7(t), DesiredStateRevisionID: mustUUIDv7(t),
		Domains: []nginxconfig.DomainSpec{}, Options: nginxconfig.DefaultOptions(),
	})
	if err != nil || !retired.ReloadPerformed {
		t.Fatalf("retire disposable cache account configuration = %#v, %v", retired, err)
	}
	if _, err := domainClient.php.Reconcile(t.Context(), phpruntime.PoolSetSpec{
		Identity: identity, Versions: []string{}, MaxChildren: phpruntime.DefaultMaxChildren,
		MemoryLimitMiB: phpruntime.DefaultMemoryMiB, RetireAbsent: true,
	}); err != nil {
		t.Fatalf("retire disposable cache PHP pool: %v", err)
	}
	assertPHPAccountPoolRetired(t, phpVersion, identity)
	t.Log("STACKFORT_QUALIFICATION cache-personalization-isolation=passed")
	t.Log("STACKFORT_QUALIFICATION cache-waf-order-and-exceptions=passed")
	t.Log("STACKFORT_QUALIFICATION cache-scoped-purge-and-metrics=passed")
}

func assertVinylRuntime(t *testing.T) {
	t.Helper()
	if output, err := exec.Command("/usr/bin/systemctl", "restart", cacheconfig.ServiceName).CombinedOutput(); err != nil {
		t.Fatalf("restart Vinyl: %v: %s", err, output)
	}
	if output, err := exec.Command("/usr/bin/systemctl", "reload", cacheconfig.ServiceName).CombinedOutput(); err != nil {
		t.Fatalf("reload Vinyl: %v: %s", err, output)
	}
	properties, err := exec.Command("/usr/bin/systemctl", "show", cacheconfig.ServiceName,
		"--property=ActiveState", "--property=User", "--property=Group", "--property=Slice",
		"--property=NoNewPrivileges", "--property=ProtectSystem", "--no-pager").Output()
	if err != nil {
		t.Fatal(err)
	}
	state := string(properties)
	for _, expected := range []string{
		"ActiveState=active", "User=vinyl", "Group=vinyl", "Slice=stackfort-core.slice",
		"NoNewPrivileges=yes", "ProtectSystem=strict",
	} {
		if !strings.Contains(state, expected) {
			t.Fatalf("Vinyl service property %q absent from %s", expected, state)
		}
	}
	for _, port := range []string{"6081", "6082"} {
		connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), time.Second)
		if err != nil {
			t.Fatalf("Vinyl loopback listener %s is unavailable: %v", port, err)
		}
		_ = connection.Close()
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr != nil || ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		for _, port := range []string{"6081", "6082"} {
			connection, dialErr := net.DialTimeout("tcp", net.JoinHostPort(ip.String(), port), 250*time.Millisecond)
			if dialErr == nil {
				_ = connection.Close()
				t.Fatalf("Vinyl exposed management/data listener %s on non-loopback %s", port, ip)
			}
		}
	}
	secret, err := os.Stat(cacheconfig.SecretPath)
	if err != nil || secret.Mode().Perm() != 0o600 {
		t.Fatalf("Vinyl management secret mode = %#v / %v", secret, err)
	}
	t.Log("STACKFORT_QUALIFICATION vinyl-runtime-sandbox-and-loopback=passed")
}

func requestCache(t *testing.T, host, method, path string, headers map[string]string) cacheHTTPResult {
	t.Helper()
	var requestBody io.Reader
	if method == http.MethodPost {
		requestBody = strings.NewReader("fixture=1")
	}
	return requestCacheWithBody(t, host, method, path, headers, requestBody)
}

func requestCacheWithBody(
	t *testing.T, host, method, path string, headers map[string]string, requestBody io.Reader,
) cacheHTTPResult {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, "http://127.0.0.1"+path, requestBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s%s: %v", method, host, path, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return cacheHTTPResult{
		status: response.StatusCode, body: string(responseBody),
		decision: response.Header.Get("X-Stackfort-Cache"), headers: response.Header.Clone(),
	}
}

func assertCacheDecision(t *testing.T, result cacheHTTPResult, status int, decision string) {
	t.Helper()
	if result.status != status || result.decision != decision {
		t.Fatalf("cache response = status %d, decision %q, body %q; want %d/%s",
			result.status, result.decision, result.body, status, decision)
	}
}

func assertNeverCached(t *testing.T, host, method, path string, headers map[string]string) {
	t.Helper()
	first := requestCache(t, host, method, path, headers)
	second := requestCache(t, host, method, path, headers)
	assertCacheDecision(t, first, http.StatusOK, "BYPASS")
	assertCacheDecision(t, second, http.StatusOK, "BYPASS")
	if first.body == second.body {
		t.Fatalf("personalized/unsafe request unexpectedly reused a response: %s %s", method, path)
	}
}

func assertNeverStored(t *testing.T, host, path string) {
	t.Helper()
	first := requestCache(t, host, http.MethodGet, path, nil)
	second := requestCache(t, host, http.MethodGet, path, nil)
	if first.status != http.StatusOK || second.status != http.StatusOK ||
		first.decision == "HIT" || second.decision == "HIT" || first.body == second.body {
		t.Fatalf("uncacheable origin response was stored: %#v / %#v", first, second)
	}
}

func assertWAFSQLiStatus(t *testing.T, host, path string, status int) {
	t.Helper()
	result := requestCache(t, host, http.MethodGet, path, nil)
	if result.status != status {
		t.Fatalf("WAF protocol response for %s%s = %d, want %d: %q", host, path, result.status, status, result.body)
	}
}

func waitForDetectedWAFRuleIDs(
	t *testing.T, identity hostingidentity.Spec, host, requestPath string,
) []uint32 {
	t.Helper()
	domain, err := core.NormalizeDomainName(host)
	if err != nil {
		t.Fatal(err)
	}
	manager := hostlogs.NewManager()
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, readErr := manager.ReadWAFEvents(t.Context(), agentprotocol.WAFEventReadRequest{
			Identity: identity, Domain: domain, Limit: agentprotocol.MaximumWAFEventEntries,
		})
		if readErr != nil {
			t.Fatalf("read sanitized WAF events: %v", readErr)
		}
		seen := map[uint32]struct{}{}
		for _, event := range response.Events {
			if event.Path == requestPath && event.RuleID >= 920000 && event.RuleID <= 944999 {
				seen[event.RuleID] = struct{}{}
			}
		}
		if len(seen) != 0 {
			rules := make([]uint32, 0, len(seen))
			for ruleID := range seen {
				rules = append(rules, ruleID)
			}
			sort.Slice(rules, func(left, right int) bool { return rules[left] < rules[right] })
			return rules
		}
		if time.Now().After(deadline) {
			t.Fatalf("detection-only request produced no sanitized exception-eligible WAF event: %#v", response.Events)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func measureFastCGICacheBaseline(
	t *testing.T,
	identity hostingidentity.Spec,
	phpVersion string,
	wafMode core.WAFMode,
	label string,
) httpBaselineMetric {
	t.Helper()
	socket, err := phpruntime.SocketPath(identity, phpVersion)
	if err != nil {
		t.Fatal(err)
	}
	// /var/cache/nginx is root-only on Debian 13. /var/lib/nginx is the
	// distribution-owned, worker-traversable and SELinux-labelled NGINX state
	// root on every supported target.
	cacheRoot := fmt.Sprintf("/var/lib/nginx/stackfort-benchmark-%d-%s", identity.UID, label)
	configuration := "/etc/nginx/stackfort/global/90-cache-benchmark.conf"
	if err := os.MkdirAll(cacheRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	platform := hostcapabilities.NewInspector().InspectPlatform()
	baseline, err := nginxbaseline.ForDistribution(platform.DistributionID)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := user.Lookup(baseline.WorkerUser)
	if err != nil {
		t.Fatal(err)
	}
	workerUID, uidErr := strconv.Atoi(worker.Uid)
	workerGID, gidErr := strconv.Atoi(worker.Gid)
	if uidErr != nil || gidErr != nil || os.Chown(cacheRoot, workerUID, workerGID) != nil {
		t.Fatalf("assign NGINX benchmark cache directory to %s", baseline.WorkerUser)
	}
	zone := "stackfort_benchmark_" + strings.ReplaceAll(label, "-", "_")
	wafDirectives := ""
	wafProfile, err := wafconfig.ProfilePath(wafMode)
	if err != nil {
		t.Fatal(err)
	}
	if wafProfile != "" {
		wafDirectives = fmt.Sprintf("    coraza on;\n    coraza_transaction_id $request_id;\n    coraza_rules_file %q;\n", wafProfile)
	}
	source := fmt.Sprintf(`fastcgi_cache_path %s levels=1:2 keys_zone=%s:8m inactive=5m use_temp_path=off;

server {
    listen 127.0.0.1:8008;
    server_name cache-benchmark.stackfort.test;
    root %s/public_html;
    index index.php;
%s

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        try_files $uri =404;
        include /etc/nginx/fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
        fastcgi_pass unix:%s;
        fastcgi_cache %s;
        fastcgi_cache_methods GET HEAD;
        fastcgi_cache_valid 200 2m;
        fastcgi_cache_bypass $http_authorization $http_cookie;
        fastcgi_no_cache $http_authorization $http_cookie $upstream_http_set_cookie;
        add_header X-Stackfort-FastCGI-Cache $upstream_cache_status always;
        add_header X-Stackfort-Benchmark-WAF %q always;
    }
}
`, cacheRoot, zone, identity.HomeDirectory, wafDirectives, socket, zone, label)
	if err := os.WriteFile(configuration, []byte(source), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(configuration)
		_ = os.RemoveAll(cacheRoot)
		_ = exec.Command("/usr/bin/systemctl", "reload", "nginx.service").Run()
	})
	if output, err := exec.Command("/usr/sbin/nginx", "-t").CombinedOutput(); err != nil {
		t.Fatalf("validate NGINX FastCGI benchmark: %v: %s", err, output)
	}
	if output, err := exec.Command("/usr/bin/systemctl", "reload", "nginx.service").CombinedOutput(); err != nil {
		t.Fatalf("activate NGINX FastCGI benchmark: %v: %s", err, output)
	}
	waitForFastCGIBenchmarkGeneration(t, label, wafMode)
	return measureHTTPBaseline(t, "cache-nginx-fastcgi-"+label,
		"http://127.0.0.1:8008/index.php?benchmark=fastcgi-"+label,
		"cache-benchmark.stackfort.test", 3_000, 8)
}

func waitForFastCGIBenchmarkGeneration(t *testing.T, label string, wafMode core.WAFMode) {
	t.Helper()
	const host = "cache-benchmark.stackfort.test"
	client := &http.Client{
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true},
		Timeout:   3 * time.Second,
	}
	requestOnce := func(path string) (int, string, error) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:8008"+path, nil)
		if err != nil {
			return 0, "", err
		}
		request.Host = host
		response, err := client.Do(request)
		if err != nil {
			return 0, "", err
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return response.StatusCode, response.Header.Get("X-Stackfort-Benchmark-WAF"), nil
	}
	deadline := time.Now().Add(5 * time.Second)
	consecutive := 0
	for consecutive < 8 && time.Now().Before(deadline) {
		status, generation, err := requestOnce("/index.php?generation=" + label)
		if err == nil && status == http.StatusOK && generation == label {
			consecutive++
		} else {
			consecutive = 0
		}
		if consecutive < 8 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if consecutive != 8 {
		t.Fatalf("NGINX FastCGI benchmark generation %q did not become exclusive", label)
	}
	wantAttackStatus := http.StatusOK
	if wafMode == core.WAFModeBlockingPL1 {
		wantAttackStatus = http.StatusForbidden
	}
	status, generation, err := requestOnce("/index.php?lookup=1%20OR%201=1&generation=" + label)
	if err != nil || status != wantAttackStatus || generation != label {
		t.Fatalf("NGINX FastCGI benchmark WAF probe = %d/%q/%v, want %d/%q",
			status, generation, err, wantAttackStatus, label)
	}
}
