// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostnginx"
)

func TestPanelCLIRequiresExplicitSafeInputs(t *testing.T) {
	for _, arguments := range [][]string{nil, {"configure"}, {"disable"}, {"recover"}, {"configure", "--hostname=https://panel.example.com", "--certificate=/root/cert", "--private-key=/root/key", "--yes"}, {"status", "--hostname=panel.example.com"}, {"disable", "--yes", "extra"}, {"status", "--format=yaml"}} {
		var output bytes.Buffer
		called := false
		code := runPanel(context.Background(), arguments, &output, &output, func(context.Context, hostnginx.PanelRequest) (hostnginx.PanelStatus, error) {
			called = true
			return hostnginx.PanelStatus{}, nil
		})
		if code != exitError || called {
			t.Fatalf("unsafe invocation accepted: %v", arguments)
		}
	}
}

func TestPanelCLIPassesTypedRequest(t *testing.T) {
	var output bytes.Buffer
	code := runPanel(context.Background(), []string{"configure", "--hostname=panel.example.com", "--certificate=/root/cert", "--private-key=/root/key", "--yes", "--format=json"}, &output, &output,
		func(_ context.Context, request hostnginx.PanelRequest) (hostnginx.PanelStatus, error) {
			if request != (hostnginx.PanelRequest{Action: "configure", Hostname: "panel.example.com", CertificatePath: "/root/cert", PrivateKeyPath: "/root/key"}) {
				t.Fatal(request)
			}
			return hostnginx.PanelStatus{Enabled: true, Hostname: request.Hostname, URL: "https://panel.example.com/"}, nil
		})
	if code != exitReady || !strings.Contains(output.String(), `"url":"https://panel.example.com/"`) || strings.Contains(output.String(), "/root/key") {
		t.Fatal(code, output.String())
	}
}
