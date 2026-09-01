// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostfilesystem"
	"github.com/RTBGG/stackfort/internal/hostidentity"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/hostlogs"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
)

func TestDisposableHostDomainLogPrivacyAndRetention(t *testing.T) {
	if os.Getenv(disposableHostOptIn) != "1" {
		t.Skipf("set %s=1 only inside a disposable Stackfort VM", disposableHostOptIn)
	}
	if os.Geteuid() != 0 {
		t.Fatal("disposable host integration test must run as root")
	}
	if _, err := hostnginx.NewReconciler().Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile NGINX baseline: %v", err)
	}

	identity := disposableIdentity(t, availableManagedID(t, 249_300))
	t.Cleanup(func() { cleanupIdentity(t, identity) })
	t.Cleanup(func() { removeDisposableDomainLogDirectory(t, identity.AccountID) })
	activated := false
	t.Cleanup(func() {
		if activated {
			deactivateDisposableDomainLogs(t, identity)
		}
	})
	if _, err := hostidentity.NewReconciler().ReconcileBase(t.Context(), identity); err != nil {
		t.Fatalf("reconcile identity: %v", err)
	}
	filesystem := hostfilesystem.NewReconciler()
	if _, err := filesystem.Reconcile(t.Context(), hostingstorage.Spec{
		Identity: identity, ProjectID: identity.UID,
	}); err != nil {
		t.Fatalf("reconcile filesystem: %v", err)
	}
	if _, err := filesystem.EnsureDocumentRoot(
		t.Context(), identity, "public_html", agentprotocol.DocumentRootAccessStatic,
	); err != nil {
		t.Fatalf("prepare static document root: %v", err)
	}

	compactAccountID := strings.ReplaceAll(identity.AccountID, "-", "")
	host := "logs-" + compactAccountID[len(compactAccountID)-12:] + ".stackfort.test"
	domain, err := core.NormalizeDomainName(host)
	if err != nil {
		t.Fatal(err)
	}
	manager := hostlogs.NewManager()
	if err := manager.Ensure(t.Context(), identity, []core.NormalizedDomainName{domain}); err != nil {
		t.Fatalf("prepare domain log files: %v", err)
	}
	ensureDisposableDomainLogSELinuxContext(t)
	accessPath := hostinglogs.DomainFile(identity.AccountID, domain.ASCII, string(agentprotocol.HostingLogAccess))
	errorPath := hostinglogs.DomainFile(identity.AccountID, domain.ASCII, string(agentprotocol.HostingLogError))
	assertRootOwnedMode(t, filepath.Dir(accessPath), 0o700)
	assertRootOwnedMode(t, accessPath, 0o640)
	assertRootOwnedMode(t, errorPath, 0o640)
	assertDisposableDomainLogSELinuxContext(t, accessPath)

	writeManagedTestFile(t, filepath.Join(identity.HomeDirectory, "public_html", "index.html"),
		[]byte("stackfort-domain-log-probe\n"), identity.UID, 0o640)
	activation, err := hostnginx.NewActivator().Activate(t.Context(), hostnginx.ActivationSpec{
		Identity: identity, RevisionID: mustUUIDv7(t), DesiredStateRevisionID: mustUUIDv7(t),
		Domains: []nginxconfig.DomainSpec{{
			Name: domain, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth,
			Target: nginxconfig.TargetSpec{Type: core.DomainTargetStatic, DocumentRoot: "public_html"},
		}}, Options: nginxconfig.DefaultOptions(),
	})
	if err != nil || !activation.ConfigurationTested || !activation.ReloadPerformed || !activation.HealthChecked {
		t.Fatalf("activate log-qualified site = %#v, %v", activation, err)
	}
	activated = true

	const (
		querySecret     = "query-secret-74631"
		authorization   = "authorization-secret-83142"
		cookieSecret    = "cookie-secret-90253"
		referrerSecret  = "referrer-secret-16475"
		userAgentSecret = "user-agent-secret-27586"
	)
	response := requestDomainLogProbe(t, host, "/index.html?token="+querySecret, map[string]string{
		"Authorization": authorization,
		"Cookie":        "session=" + cookieSecret,
		"Referer":       "https://referrer.stackfort.test/" + referrerSecret,
		"User-Agent":    userAgentSecret,
	})
	if !strings.Contains(response, "HTTP/1.1 200") || !strings.Contains(response, "stackfort-domain-log-probe") {
		t.Fatalf("domain log probe response = %q", response)
	}
	rawAccess := waitForDomainLogContent(t, accessPath, `"path":"/index.html"`)
	for _, secret := range []string{querySecret, authorization, cookieSecret, referrerSecret, userAgentSecret} {
		if strings.Contains(rawAccess, secret) {
			t.Fatalf("managed access log persisted secret %q", secret)
		}
	}
	if strings.Contains(rawAccess, `"path":"/index.html?`) {
		t.Fatal("managed access log persisted a query string")
	}

	first, err := manager.Read(t.Context(), agentprotocol.HostingLogReadRequest{
		Identity: identity, Domain: domain, Kind: agentprotocol.HostingLogAccess, Limit: 1,
	})
	if err != nil || len(first.Records) != 1 || first.Next == "" {
		t.Fatalf("read newest access log page = %#v, %v", first, err)
	}
	if first.Records[0].Path != "/index.html" || first.Records[0].Host != host ||
		!first.SensitiveRedaction || first.QueryStringsStored || first.RetentionDays != hostinglogs.RetentionDays {
		t.Fatalf("newest access log record = %#v", first)
	}
	second, err := manager.Read(t.Context(), agentprotocol.HostingLogReadRequest{
		Identity: identity, Domain: domain, Kind: agentprotocol.HostingLogAccess,
		Cursor: first.Next, Limit: 1,
	})
	if err != nil || len(second.Records) != 1 {
		t.Fatalf("read prior access log page = %#v, %v", second, err)
	}

	foreign := disposableIdentity(t, availableManagedID(t, 249_350))
	if _, err := manager.Read(t.Context(), agentprotocol.HostingLogReadRequest{
		Identity: foreign, Domain: domain, Kind: agentprotocol.HostingLogAccess, Limit: 1,
	}); !errors.Is(err, hostlogs.ErrNotFound) {
		t.Fatalf("cross-account log read error = %v, want not found", err)
	}

	const errorLine = "2026/08/29 15:00:00 [error] 42#42: *1 request: \"GET /reset/reset-secret?token=error-query-secret HTTP/1.1\", authorization=Bearer error-auth-secret, cookie=session=error-cookie-secret, api_key=error-api-secret\n"
	appendRootLogLine(t, errorPath, errorLine)
	errorPage, err := manager.Read(t.Context(), agentprotocol.HostingLogReadRequest{
		Identity: identity, Domain: domain, Kind: agentprotocol.HostingLogError, Limit: 10,
	})
	if err != nil || len(errorPage.Records) == 0 || !strings.Contains(errorPage.Records[0].Message, "[REDACTED]") {
		t.Fatalf("read redacted error log = %#v, %v", errorPage, err)
	}
	for _, secret := range []string{"reset-secret", "error-query-secret", "error-auth-secret", "error-cookie-secret", "error-api-secret"} {
		if strings.Contains(errorPage.Records[0].Message, secret) {
			t.Fatalf("account-facing error log exposed secret %q: %q", secret, errorPage.Records[0].Message)
		}
	}

	symlinkIdentity := disposableIdentity(t, availableManagedID(t, 249_400))
	symlinkPath := filepath.Join(hostinglogs.RootDirectory, symlinkIdentity.AccountID)
	if err := os.Symlink("/etc", symlinkPath); err != nil {
		t.Fatalf("create log-directory symlink fixture: %v", err)
	}
	if err := manager.Ensure(t.Context(), symlinkIdentity, []core.NormalizedDomainName{domain}); !errors.Is(err, hostlogs.ErrConflict) {
		_ = os.Remove(symlinkPath)
		t.Fatalf("symlink-backed log directory error = %v, want conflict", err)
	}
	if err := os.Remove(symlinkPath); err != nil {
		t.Fatalf("remove log-directory symlink fixture: %v", err)
	}

	assertDisposableDomainLogRotation(t)
	deactivateDisposableDomainLogs(t, identity)
	activated = false
	t.Log("STACKFORT_QUALIFICATION domain-log-redaction-retention=passed")
}

func requestDomainLogProbe(t *testing.T, host, requestPath string, headers map[string]string) string {
	t.Helper()
	connection, err := net.DialTimeout("tcp", "127.0.0.1:80", 2*time.Second)
	if err != nil {
		t.Fatalf("connect to domain log probe: %v", err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	var request strings.Builder
	request.WriteString("GET " + requestPath + " HTTP/1.1\r\nHost: " + host + "\r\n")
	for name, value := range headers {
		request.WriteString(name + ": " + value + "\r\n")
	}
	request.WriteString("Connection: close\r\n\r\n")
	if _, err := io.WriteString(connection, request.String()); err != nil {
		t.Fatalf("write domain log probe: %v", err)
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		t.Fatalf("read domain log probe: %v", err)
	}
	return string(response)
}

func waitForDomainLogContent(t *testing.T, path, wanted string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		content, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(content), wanted) {
			return string(content)
		}
		if time.Now().After(deadline) {
			t.Fatalf("domain log %s did not contain %q: %v / %q", path, wanted, err, content)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func appendRootLogLine(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open managed error log: %v", err)
	}
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		t.Fatalf("append managed error log: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close managed error log: %v", err)
	}
}

func ensureDisposableDomainLogSELinuxContext(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/sbin/semanage"); err != nil {
		return
	}
	expression := "/var/log/stackfort/accounts(/.*)?"
	add := exec.Command("/usr/sbin/semanage", "fcontext", "-a", "-t", "httpd_log_t", expression)
	if output, err := add.CombinedOutput(); err != nil {
		modify := exec.Command("/usr/sbin/semanage", "fcontext", "-m", "-t", "httpd_log_t", expression)
		if modifyOutput, modifyErr := modify.CombinedOutput(); modifyErr != nil {
			t.Fatalf("reconcile domain log SELinux context: add=%v %s; modify=%v %s",
				err, output, modifyErr, modifyOutput)
		}
	}
	if output, err := exec.Command("/usr/sbin/restorecon", "-R", hostinglogs.RootDirectory).CombinedOutput(); err != nil {
		t.Fatalf("restore domain log SELinux context: %v: %s", err, output)
	}
}

func assertDisposableDomainLogSELinuxContext(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat("/usr/sbin/semanage"); err != nil {
		return
	}
	label, err := exec.Command("/usr/bin/ls", "-Zd", path).Output()
	if err != nil || !strings.Contains(string(label), "httpd_log_t") {
		t.Fatalf("domain log SELinux label = %q / %v", label, err)
	}
}

func assertDisposableDomainLogRotation(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/sbin/logrotate"); err != nil {
		t.Fatalf("logrotate is unavailable: %v", err)
	}
	temporary := t.TempDir()
	accountsRoot := filepath.Join(temporary, "accounts")
	accountRoot := filepath.Join(accountsRoot, "account")
	if err := os.MkdirAll(accountRoot, 0o700); err != nil {
		t.Fatalf("create logrotate fixture directory: %v", err)
	}
	active := filepath.Join(accountRoot, "domain.access.log")
	if err := os.WriteFile(active, []byte("first\n"), 0o640); err != nil {
		t.Fatalf("write logrotate fixture: %v", err)
	}
	configuration := strings.ReplaceAll(hostinglogs.RetentionConfiguration(), hostinglogs.RootDirectory, accountsRoot)
	configurationPath := filepath.Join(temporary, "logrotate.conf")
	statePath := filepath.Join(temporary, "logrotate.state")
	if err := os.WriteFile(configurationPath, []byte(configuration), 0o600); err != nil {
		t.Fatalf("write logrotate configuration: %v", err)
	}
	runLogrotate := func() {
		command := exec.Command("/usr/sbin/logrotate", "--force", "--state", statePath, configurationPath)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("run logrotate fixture: %v: %s", err, output)
		}
	}
	runLogrotate()
	if err := os.WriteFile(active, []byte("second\n"), 0o640); err != nil {
		t.Fatalf("write second logrotate fixture: %v", err)
	}
	runLogrotate()
	assertRootOwnedMode(t, active, 0o640)
	assertRootOwnedMode(t, active+".1", 0o640)
	if info, err := os.Stat(active + ".2.gz"); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("delay-compressed prior log is missing: %#v / %v", info, err)
	}
}

func deactivateDisposableDomainLogs(t *testing.T, identity hostingidentity.Spec) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := hostnginx.NewActivator().Activate(ctx, hostnginx.ActivationSpec{
		Identity: identity, RevisionID: mustUUIDv7(t), DesiredStateRevisionID: mustUUIDv7(t),
		Domains: []nginxconfig.DomainSpec{}, Options: nginxconfig.DefaultOptions(),
	})
	if err != nil || !result.ConfigurationTested || !result.HealthChecked {
		t.Errorf("deactivate disposable domain logs = %#v, %v", result, err)
	}
}

func removeDisposableDomainLogDirectory(t *testing.T, accountID string) {
	t.Helper()
	target := filepath.Join(hostinglogs.RootDirectory, accountID)
	if filepath.Dir(target) != hostinglogs.RootDirectory || filepath.Base(target) != accountID {
		t.Errorf("refusing unsafe domain log cleanup path: %q", target)
		return
	}
	if err := os.RemoveAll(target); err != nil {
		t.Errorf("remove disposable domain logs: %v", err)
	}
}
