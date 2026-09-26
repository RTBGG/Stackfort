// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestOnboardArmDiagnosticPreservesBoundedCause(t *testing.T) {
	var diagnostic onboardArmDiagnostic
	const cause = "native boot failed: verify one-shot initrd: initrd inventory exceeds 8 MiB byte limit\n"
	if _, err := io.WriteString(&diagnostic, cause); err != nil {
		t.Fatal(err)
	}
	runErr := errors.New("exit status 1")
	err := onboardArmFailure(runErr, nil, &diagnostic)
	if !errors.Is(err, runErr) || !strings.Contains(err.Error(), strings.TrimSpace(cause)) || !strings.Contains(err.Error(), "reboot was not requested") {
		t.Fatal(err)
	}
}

func TestOnboardArmDiagnosticDrainRedactionAndEscapes(t *testing.T) {
	secret := "sfb_" + strings.Repeat("A", 43)
	for _, chunks := range [][]string{
		{"failure: " + secret + "\n\x1b[31m\r\x00"},
		{"failure: sfb_", strings.Repeat("A", 43), "\n\x1b[31m\r\x00"},
		{strings.Repeat("x", onboardArmDiagnosticLimit-12), secret},
	} {
		var diagnostic onboardArmDiagnostic
		for _, chunk := range chunks {
			if n, err := io.WriteString(&diagnostic, chunk); err != nil || n != len(chunk) {
				t.Fatal("diagnostic caused a command pipe failure", n, err)
			}
		}
		message := onboardArmFailure(errors.New("exit status 1"), nil, &diagnostic).Error()
		if strings.Contains(message, "sfb_") || strings.ContainsAny(message, "\n\r\x00\x1b") || !strings.Contains(message, "[setup-code-redacted]") {
			t.Fatal("unsafe or unredacted diagnostic")
		}
	}
	var diagnostic onboardArmDiagnostic
	payload := strings.Repeat("x", 1<<20)
	if n, err := io.Copy(&diagnostic, strings.NewReader(payload)); err != nil || n != int64(len(payload)) || diagnostic.size != onboardArmDiagnosticLimit || !diagnostic.truncated {
		t.Fatal("bounded capture did not drain full output", n, err)
	}
	if !strings.Contains(onboardArmFailure(errors.New("exit status 1"), nil, &diagnostic).Error(), "[truncated after 8192 bytes]") {
		t.Fatal("truncation not disclosed")
	}
}

func TestOnboardArmDiagnosticTimeoutAndMissingOutput(t *testing.T) {
	for _, test := range []struct {
		context error
		want    string
	}{{nil, "reboot was not requested"}, {context.DeadlineExceeded, "arming timed out"}, {context.Canceled, "arming was cancelled"}} {
		message := onboardArmFailure(errors.New("process failed"), test.context, nil).Error()
		if !strings.Contains(message, test.want) || strings.Contains(message, "runtime diagnostic:") {
			t.Fatal(message)
		}
	}
}
