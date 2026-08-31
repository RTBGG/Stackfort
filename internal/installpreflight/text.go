// SPDX-License-Identifier: AGPL-3.0-or-later

package installpreflight

import (
	"fmt"
	"io"
	"strings"
)

func WriteText(output io.Writer, result Result) error {
	verdict := "BLOCKED"
	if result.Ready {
		verdict = "READY"
	}
	if _, err := fmt.Fprintf(output,
		"Stackfort installer preflight (read-only)\nResult: %s\nHost: %s %s, %s, kernel %s\n\nChecks\n",
		verdict, result.Capabilities.Platform.DistributionID, result.Capabilities.Platform.VersionID,
		result.Capabilities.Platform.Architecture, result.Capabilities.Platform.KernelRelease,
	); err != nil {
		return err
	}
	for _, check := range result.Checks {
		label := strings.ToUpper(string(check.Status))
		if _, err := fmt.Fprintf(output, "[%s] %s: %s", label, check.ID, check.Summary); err != nil {
			return err
		}
		if check.Detail != "" {
			if _, err := fmt.Fprintf(output, " — %s", check.Detail); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output); err != nil {
			return err
		}
		if check.Remediation != "" {
			if _, err := fmt.Fprintf(output, "       Fix: %s [%s]\n", check.Remediation, check.ReasonCode); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(output, "\nInstallation plan (no changes have been applied)"); err != nil {
		return err
	}
	if err := writePlanSection(output, "Packages", len(result.Plan.Packages), func(index int) string {
		item := result.Plan.Packages[index]
		return item.Name + " — " + item.Action
	}); err != nil {
		return err
	}
	if err := writePlanSection(output, "Files and directories", len(result.Plan.Files), func(index int) string {
		item := result.Plan.Files[index]
		return fmt.Sprintf("%s — %s; %s %s", item.Path, item.Action, item.Owner, item.Mode)
	}); err != nil {
		return err
	}
	if err := writePlanSection(output, "Users", len(result.Plan.Users), func(index int) string {
		item := result.Plan.Users[index]
		return fmt.Sprintf("%s — %s; home %s; shell %s", item.Name, item.Action, item.Home, item.Shell)
	}); err != nil {
		return err
	}
	if err := writePlanSection(output, "Services", len(result.Plan.Services), func(index int) string {
		item := result.Plan.Services[index]
		return item.Name + " — " + item.Action
	}); err != nil {
		return err
	}
	if err := writePlanSection(output, "Ports and local endpoints", len(result.Plan.Ports), func(index int) string {
		item := result.Plan.Ports[index]
		return fmt.Sprintf("%s (%s) — %s", item.Endpoint, item.Scope, item.Purpose)
	}); err != nil {
		return err
	}
	return writePlanSection(output, "Security-module changes", len(result.Plan.Security), func(index int) string {
		item := result.Plan.Security[index]
		return item.Provider + " — " + item.Action
	})
}

func writePlanSection(output io.Writer, heading string, count int, item func(int) string) error {
	if _, err := fmt.Fprintln(output, heading+":"); err != nil {
		return err
	}
	if count == 0 {
		_, err := fmt.Fprintln(output, "  - none (unsupported platform)")
		return err
	}
	for index := range count {
		if _, err := fmt.Fprintln(output, "  - "+item(index)); err != nil {
			return err
		}
	}
	return nil
}
