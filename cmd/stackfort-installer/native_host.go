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
	"github.com/google/uuid"
)

func runNativeHost(ctx context.Context, arguments []string, stdout, stderr io.Writer, inspect func(context.Context) (installapply.NativeHostReport, error)) int {
	flags := flag.NewFlagSet("native-host", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitReady
		}
		return exitError
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return exitError
	}
	report, err := inspect(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "native host inspection:", err)
		return exitError
	}
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(report)
	} else {
		_, err = fmt.Fprintf(stdout, "Native host qualification: eligible=%t prerequisites-ready=%t public-activation=false\nProfile: %s\nMissing packages: %v\n", report.Eligible, report.PrerequisitesReady, report.Profile, report.MissingPackages)
		for _, check := range report.Checks {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(stdout, "[%s] %s: %s %s\n", check.Status, check.ID, check.Detail, check.Remediation)
		}
	}
	if err != nil {
		return exitError
	}
	if !report.Eligible {
		return exitBlocked
	}
	return exitReady
}

// Internal APT guard; never exposed as an installation or recovery switch.
func runNativePrerequisiteCheck(ctx context.Context, arguments []string, input io.Reader, stderr io.Writer) int {
	flags := flag.NewFlagSet("native-prerequisite-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	operation := flags.String("operation-id", "", "sealed prerequisite operation")
	if err := flags.Parse(arguments); err != nil {
		return exitError
	}
	parsed, err := uuid.Parse(*operation)
	if err != nil || parsed.String() != *operation || parsed == uuid.Nil || flags.NArg() != 0 {
		return exitError
	}
	if err := installapply.CheckNativePrerequisiteTransaction(ctx, *operation, input); err != nil {
		_, _ = fmt.Fprintln(stderr, "prerequisite transaction rejected:", err)
		return exitBlocked
	}
	return exitReady
}
