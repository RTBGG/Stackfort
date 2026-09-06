// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/operations"
	"github.com/RTBGG/stackfort/internal/panelconfig"
	"github.com/RTBGG/stackfort/internal/paneltls"
)

func panelFixture(t *testing.T) (*panelManager, PanelRequest, []byte, []byte) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root for filesystem ownership qualification")
	}
	spec, _ := nginxbaseline.ForDistribution("debian")
	manager := &panelManager{root: t.TempDir(), spec: spec, runner: &fakeRunner{}, roots: x509.NewCertPool(),
		health: func(context.Context, panelconfig.Config, []byte) error { return nil }}
	for _, directory := range []string{nginxbaseline.PanelDirectory, nginxbaseline.SitesDirectory, panelconfig.TLSDirectory, acmehttp01.ChallengeDirectory, "/root"} {
		if err := os.MkdirAll(manager.path(directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{nginxbaseline.PanelConfigurationPath: nginxbaseline.Panel(spec), nginxbaseline.MainConfiguration: nginxbaseline.Main(spec), nginxbaseline.MarkerPath: nginxbaseline.OwnershipMarker} {
		if err := manager.write(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	cert, key := panelTestCertificate(t, manager, time.Now())
	request := PanelRequest{Action: "configure", Hostname: "panel.example.com", CertificatePath: "/root/cert.pem", PrivateKeyPath: "/root/key.pem"}
	if err := manager.write(request.CertificatePath, cert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manager.write(request.PrivateKeyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	return manager, request, cert, key
}

func panelTestCertificate(t *testing.T, manager *panelManager, now time.Time) ([]byte, []byte) {
	t.Helper()
	bundle, err := paneltls.New(now, "panel.example.com", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	block, key := pem.Decode(bundle)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	manager.roots.AddCert(leaf)
	return pem.EncodeToMemory(block), key
}

func TestPanelConfigureRotateNoopDisableAndReservation(t *testing.T) {
	manager, request, _, _ := panelFixture(t)
	ctx := context.Background()
	status, err := manager.manage(ctx, request)
	if err != nil || !status.Enabled || status.URL != "https://panel.example.com/" {
		t.Fatal(status, err)
	}
	_, previous, _ := manager.current()
	status, err = manager.manage(ctx, request)
	if err != nil || !status.Enabled {
		t.Fatal(status, err)
	}
	if manager.runner.(*fakeRunner).calls[agentexec.ProfileSystemdReloadNGINX] != 1 {
		t.Fatal("no-op reloaded NGINX")
	}
	workspace := &linuxActivationWorkspace{store: &linuxActivationStore{root: manager.root}}
	for _, name := range []string{"panel.example.com", "*.example.com"} {
		if workspace.checkPanelReservation([]byte("server_name "+name+";")) == nil {
			t.Fatal("tenant may shadow panel")
		}
	}
	if err := workspace.checkPanelReservation([]byte("server_name other.example.com;")); err != nil {
		t.Fatal(err)
	}
	cert, key := panelTestCertificate(t, manager, time.Now())
	if err := manager.write(request.CertificatePath, cert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manager.write(request.PrivateKeyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.manage(ctx, request); err != nil {
		t.Fatal(err)
	}
	_, rotated, _ := manager.current()
	if bytes.Equal(previous, rotated) {
		t.Fatal("certificate did not rotate")
	}
	status, err = manager.manage(ctx, PanelRequest{Action: "disable"})
	if err != nil || status.Enabled {
		t.Fatal(status, err)
	}
	bootstrap, err := manager.read(nginxbaseline.PanelConfigurationPath, false)
	if err != nil || string(bootstrap) != nginxbaseline.Panel(manager.spec) {
		t.Fatal("bootstrap endpoint changed")
	}
}

func TestPanelFailedValidationAndHealthRestorePriorConfiguration(t *testing.T) {
	for _, failure := range []string{"candidate", "reload", "health"} {
		t.Run(failure, func(t *testing.T) {
			manager, request, _, _ := panelFixture(t)
			if _, err := manager.manage(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			_, previous, _ := manager.current()
			cert, key := panelTestCertificate(t, manager, time.Now())
			if err := manager.write(request.CertificatePath, cert, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := manager.write(request.PrivateKeyPath, key, 0o600); err != nil {
				t.Fatal(err)
			}
			runner := manager.runner.(*fakeRunner)
			switch failure {
			case "candidate":
				runner.fail = agentexec.ProfileNGINXTestPanelCandidate
			case "reload":
				runner.failCall = map[agentexec.ProfileID]int{agentexec.ProfileSystemdReloadNGINX: 2}
			case "health":
				manager.health = func(context.Context, panelconfig.Config, []byte) error { return ErrHealthCheckFailed }
			}
			if _, err := manager.manage(context.Background(), request); err == nil {
				t.Fatal("expected failed change")
			}
			_, current, err := manager.current()
			if err != nil || !bytes.Equal(previous, current) {
				t.Fatal("prior configuration not restored", err)
			}
			if _, err := manager.read(panelconfig.JournalPath, true); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rollback journal not cleared", err)
			}
		})
	}
}

func TestPanelInterruptedTransactionRecoversAndBlocksTenantActivation(t *testing.T) {
	manager, request, _, _ := panelFixture(t)
	if _, err := manager.manage(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	_, previous, _ := manager.current()
	challenge, _ := panelconfig.Render(manager.spec, panelconfig.Config{Hostname: "next.example.com", AutoRenew: true, ChallengeOnly: true})
	if err := manager.savePanelJournal(panelJournal{SchemaVersion: 1, Previous: string(previous), Next: challenge}); err != nil {
		t.Fatal(err)
	}
	if err := manager.replace(challenge); err != nil {
		t.Fatal(err)
	}
	workspace := &linuxActivationWorkspace{store: &linuxActivationStore{root: manager.root}}
	if workspace.checkPanelReservation([]byte("server_name unrelated.example.com;")) == nil {
		t.Fatal("pending transaction not fenced")
	}
	status, err := manager.manage(context.Background(), PanelRequest{Action: "status"})
	if err != nil || !status.RecoveryRequired {
		t.Fatal(status, err)
	}
	status, err = manager.manage(context.Background(), PanelRequest{Action: "recover"})
	if err != nil || status.RecoveryRequired || status.Hostname != request.Hostname || !status.Enabled {
		t.Fatal(status, err)
	}
}

func TestPanelRejectsUnsafeInputFiles(t *testing.T) {
	for _, failure := range []string{"key-mode", "symlink", "parent-symlink", "hardlink", "writable-parent", "oversized", "fifo"} {
		t.Run(failure, func(t *testing.T) {
			manager, request, _, _ := panelFixture(t)
			path := manager.path(request.PrivateKeyPath)
			switch failure {
			case "key-mode":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".real", path); err != nil {
					t.Fatal(err)
				}
			case "parent-symlink":
				if err := os.Rename(manager.path("/root"), manager.path("/real-root")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(manager.path("/real-root"), manager.path("/root")); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "writable-parent":
				if err := os.Chmod(filepath.Dir(path), 0o777); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(path, make([]byte, panelconfig.MaximumFile+1), 0o600); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				request.PrivateKeyPath = "/dev/null"
			}
			if _, err := manager.manage(context.Background(), request); err == nil {
				t.Fatal("unsafe file accepted")
			}
			if _, err := manager.read(panelconfig.ConfigurationPath, false); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed import changed active state")
			}
		})
	}
}

func TestPanelRefusesExistingTenantHostAndAlias(t *testing.T) {
	manager, request, _, _ := panelFixture(t)
	revision := "019c1234-5678-7abc-8def-0123456789ab"
	directory := manager.path(nginxbaseline.SiteRevisionsDirectory + "/" + revision)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "account-test.conf"), []byte("server_name example.com panel.example.com;\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("site-revisions/"+revision, manager.path(nginxbaseline.CurrentSitesLink)); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.manage(context.Background(), request); err == nil {
		t.Fatal("panel took over a hosting alias")
	}
}

func TestPanelAutomaticIssuanceRenewalAndNoop(t *testing.T) {
	manager, _, cert, key := panelFixture(t)
	issued := 0
	manager.issue = func(ctx context.Context, host, email string, account crypto.Signer, callbacks operations.ACMEIssueCallbacks) ([]byte, []byte, error) {
		issued++
		if host != "panel.example.com" || email != "admin@example.com" || account == nil {
			t.Fatal("wrong ACME identity")
		}
		token := "abcdefghijklmnopqrstuv"
		if err := callbacks.PresentHTTP01(ctx, token, token+"."+strings.Repeat("a", 43)); err != nil {
			t.Fatal(err)
		}
		if body, err := manager.read(acmehttp01.ChallengeDirectory+"/"+token, false); err != nil || len(body) == 0 {
			t.Fatal("challenge not presented", err)
		}
		if err := callbacks.CleanupHTTP01(ctx, token); err != nil {
			t.Fatal(err)
		}
		return append([]byte(nil), cert...), append([]byte(nil), key...), nil
	}
	request := PanelRequest{Action: "issue", Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true}
	status, err := manager.manage(context.Background(), request)
	if err != nil || !status.Enabled || !status.AutoRenew || issued != 1 {
		t.Fatal(status, issued, err)
	}
	if _, err := manager.manage(context.Background(), PanelRequest{Action: "renew"}); err != nil || issued != 1 {
		t.Fatal("renewed a fresh certificate", err)
	}
	// Install an old, still-valid certificate into the test's trusted store.
	oldCert, oldKey := panelTestCertificate(t, manager, time.Now().Add(-380*24*time.Hour))
	oldConfig, oldBundle, err := panelconfig.Import(request.Hostname, oldCert, oldKey, time.Now(), manager.roots)
	if err != nil {
		t.Fatal(err)
	}
	oldConfig.AutoRenew = true
	if err := manager.storeBundle(oldConfig, oldBundle); err != nil {
		t.Fatal(err)
	}
	oldRendered, _ := panelconfig.Render(manager.spec, oldConfig)
	if err := manager.replace(oldRendered); err != nil {
		t.Fatal(err)
	}
	attempt, _ := json.Marshal(map[string]time.Time{"at": time.Now().Add(-2 * time.Hour)})
	if err := manager.write(panelACMEAttemptPath, attempt, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = manager.manage(context.Background(), PanelRequest{Action: "renew"})
	if err != nil || issued != 2 || !status.CertificateExpiresAt.After(time.Now().Add(100*24*time.Hour)) {
		t.Fatal(status, issued, err)
	}
}

func TestPanelACMEFailureCleansTokensAndPreservesFallback(t *testing.T) {
	manager, _, _, _ := panelFixture(t)
	manager.issue = func(ctx context.Context, _, _ string, _ crypto.Signer, callbacks operations.ACMEIssueCallbacks) ([]byte, []byte, error) {
		token := "abcdefghijklmnopqrstuv"
		if err := callbacks.PresentHTTP01(ctx, token, token+"."+strings.Repeat("a", 43)); err != nil {
			t.Fatal(err)
		}
		return nil, nil, errors.New("authority rejected validation")
	}
	request := PanelRequest{Action: "issue", Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true}
	if _, err := manager.manage(context.Background(), request); err == nil {
		t.Fatal("issuance should fail")
	}
	status, err := manager.status()
	if err != nil || status.Enabled || status.RecoveryRequired {
		t.Fatal(status, err)
	}
	if _, err := manager.read(acmehttp01.ChallengeDirectory+"/abcdefghijklmnopqrstuv", false); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("challenge leaked", err)
	}
	if _, err := manager.manage(context.Background(), request); err == nil || !strings.Contains(err.Error(), "cooldown") {
		t.Fatal("no persistent rate limit", err)
	}
}

type panelRoundTripper func(*http.Request) (*http.Response, error)

func (run panelRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return run(request)
}

func TestPanelACMETransportRejectsUnapprovedOriginsAndBoundsBodies(t *testing.T) {
	called := false
	transport := panelACMETransport{panelRoundTripper(func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 3<<20)))}, nil
	})}
	for _, url := range []string{"http://acme-v02.api.letsencrypt.org/directory", "https://127.0.0.1/admin", "https://acme-v02.api.letsencrypt.org.attacker.test/", "https://user@acme-v02.api.letsencrypt.org/directory", "https://acme-v02.api.letsencrypt.org:444/"} {
		request, _ := http.NewRequest(http.MethodGet, url, nil)
		if _, err := transport.RoundTrip(request); err == nil || called {
			t.Fatal("authority boundary escaped", url)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, panelACMEDirectory, nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err == nil || len(content) != 2<<20 {
		t.Fatal("response was not bounded", len(content), err)
	}
}
