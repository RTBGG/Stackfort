// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func runNativeRecoveryPlan(ctx context.Context, arguments []string, stdout, stderr io.Writer, manage manageNativeFunc) int {
	flags := flag.NewFlagSet("native recovery-plan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "read-only recovery advice: text or json (never authorizes restore)")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitReady
		}
		return exitError
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		_, _ = fmt.Fprintln(stderr, "native recovery-plan accepts only --format=text|json; no backup, device, restore, reinstallation or approval options")
		return exitError
	}
	if manage == nil {
		manage = installapply.ManageNativeInstallation
	}
	// The sole operation is existing locked status inspection, never approval.
	status, inspectErr := manage(ctx, installapply.NativeOperatorRequest{Action: "status"})
	plan := installapply.AssessNativeRecovery(status, inspectErr)
	var err error
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(plan)
	} else {
		err = writeNativeRecoveryPlan(stdout, plan)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "write native recovery plan:", err)
		return exitError
	}
	if !plan.InspectionComplete {
		// Do not leak arbitrary partial records/error strings into this report.
		_, _ = fmt.Fprintln(stderr, "Native inspection unavailable; no partial evidence exported. Use native status locally for diagnostic errors and review before sharing.")
		return exitError
	}
	if plan.NeedsReview {
		return exitBlocked
	}
	return exitReady
}

func writeNativeRecoveryPlan(output io.Writer, plan installapply.NativeRecoveryPlan) error {
	if _, err := fmt.Fprintf(output, "Native recovery advice (%s): %s\nRecorded state only; live readiness and backups are NOT verified.\nNo recovery, restore, reinstallation, service start or reboot is authorized or executed. Public native resume remains disabled.\n%s\n",
		plan.PolicyVersion, plan.Classification, plan.Summary); err != nil {
		return err
	}
	if evidence := plan.Evidence; evidence != nil {
		if _, err := fmt.Fprintf(output, "Operation: %s\n", evidence.OperationID); err != nil {
			return err
		}
		for _, field := range []struct{ name, value string }{
			{"Recovery decision (not restore permission)", evidence.RecoveryMode}, {"Recovery choice SHA-256", evidence.RecoveryChoiceSHA256},
			{"Prerequisites", evidence.PrerequisitePhase}, {"Prerequisite SHA-256", evidence.PrerequisiteSHA256},
			{"Storage", string(evidence.StoragePhase)}, {"Storage failure code", evidence.FailureCode},
			{"Admission", evidence.AdmissionPhase}, {"Admission state SHA-256", evidence.Review.StateSHA256},
			{"Package journal SHA-256", evidence.Review.PackageSHA256}, {"Approval", evidence.ApprovalStatus},
			{"Approval SHA-256", evidence.ApprovalSHA256},
		} {
			if field.value != "" {
				if _, err := fmt.Fprintf(output, "%s: %s\n", field.name, field.value); err != nil {
					return err
				}
			}
		}
	}
	for _, step := range plan.NextSteps {
		if _, err := fmt.Fprintln(output, "- "+step); err != nil {
			return err
		}
	}
	return nil
}
