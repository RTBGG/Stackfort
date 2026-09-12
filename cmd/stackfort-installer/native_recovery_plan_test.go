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

func TestNativeRecoveryPlanCLIReadOnlyAndExitCodes(t *testing.T) {
	for _, scenario := range []string{"absent", "prerequisites", "read-error", "invalid-state"} {
		for _, format := range []string{"text", "json"} {
			t.Run(scenario+"-"+format, func(t *testing.T) {
				var out, failure bytes.Buffer
				calls := 0
				code := runNative(t.Context(), []string{"recovery-plan", "--format=" + format}, &out, &failure, func(_ context.Context, request installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error) {
					calls++
					if request != (installapply.NativeOperatorRequest{Action: "status"}) {
						t.Fatal("not read-only status", request)
					}
					status := installapply.NativeOperatorStatus{SchemaVersion: 1}
					if scenario == "read-error" {
						return status, errors.New("secret-untrusted-error")
					}
					if scenario == "invalid-state" {
						status.SchemaVersion = 0
					}
					if scenario == "prerequisites" {
						status.Prerequisites = &installapply.NativePrerequisiteStatus{OperationID: "30000000-0000-4000-8000-000000000099", Phase: "applying", RecordSHA256: strings.Repeat("a", 64)}
					}
					return status, nil
				})
				want := exitReady
				if scenario == "prerequisites" {
					want = exitBlocked
				}
				if scenario == "read-error" || scenario == "invalid-state" {
					want = exitError
				}
				if code != want || calls != 1 || strings.Contains(out.String()+failure.String(), "secret-") {
					t.Fatal(code, calls, out.String(), failure.String())
				}
				if format == "json" {
					var report installapply.NativeRecoveryPlan
					if json.Unmarshal(out.Bytes(), &report) != nil || report.AutomaticRecoveryEnabled || report.BackupVerified || report.DestructiveActionsAuthorized || report.PublicResumeEnabled || report.LiveReadinessVerified {
						t.Fatal(out.String())
					}
				} else if !strings.Contains(out.String(), "NOT verified") || !strings.Contains(out.String(), "No recovery, restore, reinstallation") {
					t.Fatal(out.String())
				}
			})
		}
	}
}

func TestNativeRecoveryPlanCLIRejectsMutationOptionsAndOutputFailure(t *testing.T) {
	for _, argument := range []string{"--yes", "--backup=/backup", "--device=/dev/sda", "--restore", "--force", "--format=yaml", "--approval-sha256=" + strings.Repeat("a", 64), "extra", "--help"} {
		var out, failure bytes.Buffer
		code := runNative(t.Context(), []string{"recovery-plan", argument}, &out, &failure, func(context.Context, installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error) {
			t.Fatal("invalid option/help accessed installation")
			return installapply.NativeOperatorStatus{}, nil
		})
		want := exitError
		if argument == "--help" {
			want = exitReady
		}
		if code != want {
			t.Fatal(argument, code)
		}
	}
	for _, format := range []string{"text", "json"} {
		var failure bytes.Buffer
		if runNative(t.Context(), []string{"recovery-plan", "--format=" + format}, nativeFailWriter{}, &failure, func(context.Context, installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error) {
			return installapply.NativeOperatorStatus{SchemaVersion: 1}, nil
		}) != exitError {
			t.Fatal("output error ignored")
		}
	}
	var out bytes.Buffer
	writeUsage(&out)
	if !strings.Contains(out.String(), "native recovery-plan") {
		t.Fatal("missing usage")
	}
}
