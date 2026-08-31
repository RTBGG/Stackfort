// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/installpreflight"
)

func TestPreflightCLIExitCodesAndFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arguments  []string
		result     installpreflight.Result
		inspectErr error
		wantExit   int
		wantOutput string
		wantError  string
	}{
		{"ready text", []string{"preflight"}, installpreflight.Result{ReadOnly: true, Ready: true}, nil, exitReady, "Result: READY", ""},
		{"blocked JSON", []string{"preflight", "--format=json"}, installpreflight.Result{ReadOnly: true}, nil, exitBlocked, `"ready": false`, ""},
		{"inspection error", []string{"preflight"}, installpreflight.Result{}, errors.New("probe failed"), exitError, "", "inspection failed"},
		{"unknown command", []string{"unknown"}, installpreflight.Result{}, nil, exitError, "", "usage:"},
		{"unconfirmed install", []string{"install", "--source-dir=/release"}, installpreflight.Result{}, nil, exitError, "", "requires --source-dir"},
		{"invalid format", []string{"preflight", "--format=yaml"}, installpreflight.Result{}, nil, exitError, "", "only --format"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			inspector := func(context.Context) (installpreflight.Result, error) {
				return test.result, test.inspectErr
			}
			exit := run(t.Context(), test.arguments, &stdout, &stderr, inspector)
			if exit != test.wantExit || !strings.Contains(stdout.String(), test.wantOutput) ||
				!strings.Contains(stderr.String(), test.wantError) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestVersionCLI(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	exit := run(t.Context(), []string{"version"}, &stdout, &stderr, nil)
	if exit != exitReady || !strings.HasPrefix(stdout.String(), "stackfort-installer ") || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestTextInstallResultIncludesSecurePanelHandoff(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := writeInstallResult(&output, "text", installapply.Result{
		Version: "1.2.3", Status: installapply.InstallComplete,
	})
	if err != nil {
		t.Fatalf("writeInstallResult: %v", err)
	}
	for _, expected := range []string{
		"https://<server-address>:8443/",
		"initial certificate is locally generated",
		"sudo -u stackfort -- /usr/local/bin/stackfort-api bootstrap create",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("result omits %q:\n%s", expected, output.String())
		}
	}
}
