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

type manageNativeFunc func(context.Context, installapply.NativeOperatorRequest) (installapply.NativeOperatorStatus, error)

func runNative(ctx context.Context, arguments []string, stdout, stderr io.Writer, manage manageNativeFunc) int {
	if len(arguments) == 0 {
		writeUsage(stderr)
		return exitError
	}
	if arguments[0] == "recovery-plan" {
		return runNativeRecoveryPlan(ctx, arguments[1:], stdout, stderr, manage)
	}
	flags := flag.NewFlagSet("native "+arguments[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	state := flags.String("state-sha256", "", "exact reviewed admission-state SHA-256")
	packages := flags.String("package-sha256", "", "exact reviewed package-journal SHA-256")
	approval := flags.String("approval-sha256", "", "exact pending approval SHA-256 to cancel")
	yes := flags.Bool("yes", false, "confirm this state-bound approval change (does not start installation)")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(arguments[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitReady
		}
		return exitError
	}
	request := installapply.NativeOperatorRequest{Action: arguments[0],
		Review: installapply.AdmissionRecovery{StateSHA256: *state, PackageSHA256: *packages}, ApprovalSHA256: *approval}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") || request.Validate() != nil ||
		(*yes != (request.Action != "status")) {
		_, _ = fmt.Fprintln(stderr, "native requires status, or an explicitly confirmed approval change with the exact reviewed digests; no resume/reset/force option exists")
		return exitError
	}
	if manage == nil {
		manage = installapply.ManageNativeInstallation
	}
	status, err := manage(ctx, request)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "native installation operation failed:", err)
		if errors.Is(err, installapply.ErrAdmissionRecovery) {
			return exitBlocked
		}
		return exitError
	}
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(status)
	} else {
		err = writeNativeStatus(stdout, request.Action, status)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "write native installation status:", err)
		return exitError
	}
	return exitReady
}

func writeNativeStatus(output io.Writer, action string, status installapply.NativeOperatorStatus) error {
	if _, err := fmt.Fprintln(output, "Native installation: recorded state only; live readiness is NOT verified.\nPublic native resume is disabled. No services or reboot were started."); err != nil {
		return err
	}
	if status.Prerequisites != nil {
		if _, err := fmt.Fprintf(output, "Prerequisite operation: %s\nPrerequisites: %s\nRecord SHA-256: %s\n", status.Prerequisites.OperationID, status.Prerequisites.Phase, status.Prerequisites.RecordSHA256); err != nil {
			return err
		}
	}
	if status.RecoveryChoice != nil {
		if _, err := fmt.Fprintf(output, "Recovery decision consumed for preparation: %s\nRecovery choice SHA-256: %s\nNot permission to restore or reinstall.\n", status.RecoveryChoice.Mode, status.RecoveryChoice.RecordSHA256); err != nil {
			return err
		}
	}
	if status.Storage == nil {
		_, err := fmt.Fprintln(output, "No native storage journal exists.")
		return err
	}
	if _, err := fmt.Fprintf(output, "Operation: %s\nStorage: %s\n", status.Storage.Plan.OperationID, status.Storage.Phase); err != nil {
		return err
	}
	if status.Admission != nil && status.Admission.Exists {
		snapshot := status.Admission
		if _, err := fmt.Fprintf(output, "Recorded admission: %s (attempt %d, boot %s)\nPackage journal complete: %t\nState SHA-256: %s\nPackage SHA-256: %s\n",
			snapshot.State.Phase, snapshot.State.Attempt, snapshot.State.BootID, snapshot.PackageComplete, snapshot.Review.StateSHA256, snapshot.Review.PackageSHA256); err != nil {
			return err
		}
	}
	if status.Approval != nil {
		if _, err := fmt.Fprintf(output, "Recovery approval: %s\nApproval SHA-256: %s\n", status.Approval.Status, status.ApprovalSHA256); err != nil {
			return err
		}
	}
	if action == "approve-recovery" {
		_, err := fmt.Fprintln(output, "Approval queued, NOT recovery completed. Only the qualified supervisor may consume it once and repeat all checks.")
		return err
	}
	return nil
}
