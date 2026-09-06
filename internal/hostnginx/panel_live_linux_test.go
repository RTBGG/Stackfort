// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/operations"
)

// Requires an explicitly disposable, already-installed host. It makes no real
// CA request and trusts a private fixture certificate only inside this process.
func TestPanelDisposableLiveNGINX(t *testing.T) {
	if os.Getenv("STACKFORT_TEST_PANEL_HOST") != "1" || os.Geteuid() != 0 {
		t.Skip("explicit disposable-host opt-in required")
	}
	spec, err := nginxbaseline.ForDistribution(hostcapabilities.NewInspector().InspectPlatform().DistributionID)
	if err != nil {
		t.Fatal(err)
	}
	manager := &panelManager{root: "/", spec: spec, runner: agentexec.NewRunner(), roots: x509.NewCertPool(), health: panelHostnameHealth}
	status, err := manager.status()
	if err != nil || status.Hostname != "" || status.RecoveryRequired {
		t.Fatal("fixture must have no configured panel host", err)
	}
	for _, path := range []string{panelACMEConsentPath, panelACMEAccountPath, panelACMEAttemptPath} {
		if _, err := manager.read(path, true); !os.IsNotExist(err) {
			t.Fatal("fixture contains existing panel ACME state", path)
		}
	}
	cert, key := panelTestCertificate(t, manager, time.Now())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := manager.manage(ctx, PanelRequest{Action: "disable"}); err != nil {
			t.Error("disable live fixture", err)
		}
		for _, path := range []string{panelACMEConsentPath, panelACMEAccountPath, panelACMEAttemptPath} {
			if err := manager.remove(path); err != nil {
				t.Error(err)
			}
		}
	})
	manager.issue = func(ctx context.Context, host, _ string, _ crypto.Signer, callbacks operations.ACMEIssueCallbacks) ([]byte, []byte, error) {
		token := "stackfortPanelLiveToken0123456789"
		authorization := token + "." + strings.Repeat("a", 43)
		if err := callbacks.PresentHTTP01(ctx, token, authorization); err != nil {
			return nil, nil, err
		}
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		var received bool
		for attempt := 0; attempt < 20; attempt++ {
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/.well-known/acme-challenge/"+token, nil)
			request.Host = host
			response, err := client.Do(request)
			if err == nil {
				body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
				_ = response.Body.Close()
				if response.StatusCode == 200 && string(body) == authorization {
					received = true
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !received {
			t.Fatal("real NGINX did not serve the exact HTTP-01 challenge")
		}
		if err := callbacks.CleanupHTTP01(ctx, token); err != nil {
			return nil, nil, err
		}
		if _, err := manager.read(acmehttp01.ChallengeDirectory+"/"+token, false); !os.IsNotExist(err) {
			t.Fatal("token survived cleanup")
		}
		return append([]byte(nil), cert...), append([]byte(nil), key...), nil
	}
	status, err = manager.manage(context.Background(), PanelRequest{Action: "issue", Hostname: "panel.example.com", Email: "admin@example.com", AcceptTerms: true})
	if err != nil || !status.Enabled || !status.AutoRenew {
		t.Fatal(status, err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: manager.roots, ServerName: "panel.example.com"},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", "127.0.0.1:443")
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/", "/api/v1/health"} {
		response, err := client.Get("https://panel.example.com" + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("panel route failed", path, response.StatusCode)
		}
	}
	request, _ := http.NewRequest(http.MethodGet, "https://panel.example.com/api/v1/health", nil)
	request.Host = "unknown.example.com"
	if response, err := client.Do(request); err == nil {
		_ = response.Body.Close()
		if response.StatusCode == 200 {
			t.Fatal("unknown Host accessed the named management origin")
		}
	}
	if _, err := manager.manage(context.Background(), PanelRequest{Action: "renew"}); err != nil {
		t.Fatal("renewal no-op", err)
	}
	t.Log("Live HTTP-01, trusted HTTPS UI/API, wrong-Host rejection, renewal no-op and cleanup passed")
}
