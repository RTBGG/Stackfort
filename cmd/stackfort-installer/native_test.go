// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func TestNativeCLIValidationAndNoImplicitResume(t *testing.T) {
	digest := strings.Repeat("a", 64)
	tests := []struct {
		args  []string
		valid bool
	}{
		{[]string{"status"}, true},
		{[]string{"status", "--format=json"}, true},
		{[]string{"approve-recovery", "--yes", "--state-sha256=" + digest, "--package-sha256=" + digest}, true},
		{[]string{"cancel-recovery", "--yes", "--approval-sha256=" + digest}, true},
		{nil, false}, {[]string{"resume", "--yes"}, false}, {[]string{"reset", "--yes"}, false},
		{[]string{"status", "--yes"}, false}, {[]string{"status", "--format=yaml"}, false},
		{[]string{"status", "--source-dir=/release"}, false}, {[]string{"status", "extra"}, false},
		{[]string{"status", "--state-sha256=" + digest}, false},
		{[]string{"approve-recovery", "--state-sha256=" + digest, "--package-sha256=" + digest}, false},
		{[]string{"approve-recovery", "--yes", "--state-sha256=" + digest}, false},
		{[]string{"approve-recovery", "--yes", "--state-sha256=" + strings.ToUpper(digest), "--package-sha256=" + digest}, false},
		{[]string{"cancel-recovery", "--approval-sha256=" + digest}, false},
		{[]string{"cancel-recovery", "--yes", "--approval-sha256=" + digest, "--package-sha256=" + digest}, false},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			var out, failure bytes.Buffer
			called := false
			manage := func(_ context.Context, request installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error) {
				called = true
				if err := request.Validate(); err != nil {
					t.Fatal(err)
				}
				return installapply.NativeOperatorStatus{SchemaVersion: 1}, nil
			}
			code := runNative(t.Context(), test.args, &out, &failure, manage)
			if called != test.valid || (code == exitReady) != test.valid {
				t.Fatalf("called=%t exit=%d error=%s", called, code, failure.String())
			}
			if test.valid && strings.Contains(strings.Join(test.args, " "), "--format=json") {
				var report installapply.NativeOperatorStatus
				if json.Unmarshal(out.Bytes(), &report) != nil || report.PublicResumeEnabled || report.LiveReadinessVerified {
					t.Fatal(out.String())
				}
			}
		})
	}
}

func TestNativeCLIErrorHelpAndOutputFailure(t *testing.T) {
	for _, cause := range []error{installapply.ErrAdmissionRecovery, errors.New("unsafe record")} {
		var out, failure bytes.Buffer
		code := runNative(t.Context(), []string{"status"}, &out, &failure, func(context.Context, installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error) {
			return installapply.NativeOperatorStatus{}, cause
		})
		want := exitError
		if errors.Is(cause, installapply.ErrAdmissionRecovery) {
			want = exitBlocked
		}
		if code != want || out.Len() != 0 {
			t.Fatal(code, out.String(), failure.String())
		}
	}
	var out, failure bytes.Buffer
	if runNative(t.Context(), []string{"status", "--help"}, &out, &failure, func(context.Context, installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error) {
		t.Fatal("help accessed state")
		return installapply.NativeOperatorStatus{}, nil
	}) != exitReady {
		t.Fatal(failure.String())
	}
	for _, format := range []string{"text", "json"} {
		if runNative(t.Context(), []string{"status", "--format=" + format}, nativeFailWriter{}, &failure, func(context.Context, installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error) {
			return installapply.NativeOperatorStatus{SchemaVersion: 1}, nil
		}) != exitError {
			t.Fatal("output failure swallowed")
		}
	}
}

type nativeFailWriter struct{}

func (nativeFailWriter) Write([]byte) (int, error) { return 0, errors.New("output closed") }

func TestNativeTextNeverClaimsLiveReadinessOrCompletedRecovery(t *testing.T) {
	var out bytes.Buffer
	status := installapply.NativeOperatorStatus{SchemaVersion: 1}
	if err := writeNativeStatus(&out, "status", status); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"recorded state only", "NOT verified", "resume is disabled", "No services or reboot were started"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	var usage bytes.Buffer
	writeUsage(&usage)
	if !strings.Contains(usage.String(), "native approve-recovery") {
		t.Fatal(usage.String())
	}
}
