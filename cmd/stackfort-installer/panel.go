// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/panelconfig"
	"github.com/RTBGG/stackfort/internal/updateapply"
)

type managePanelFunc func(context.Context, hostnginx.PanelRequest) (hostnginx.PanelStatus, error)

func runPanel(ctx context.Context, arguments []string, stdout, stderr io.Writer, manage managePanelFunc) int {
	if manage == nil {
		manage = manageInstalledPanel
	}
	if len(arguments) == 0 {
		writeUsage(stderr)
		return exitError
	}
	action := arguments[0]
	flags := flag.NewFlagSet("panel "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	hostname := flags.String("hostname", "", "canonical DNS hostname, without scheme, port, or path")
	certificate := flags.String("certificate", "", "root-owned full certificate chain PEM file (no symlinks)")
	key := flags.String("private-key", "", "root-owned mode-0600 unencrypted private key PEM file (no symlinks)")
	email := flags.String("email", "", "Let's Encrypt contact email")
	terms := flags.Bool("accept-terms", false, "accept Let's Encrypt's subscriber agreement for panel issuance and renewal")
	yes := flags.Bool("yes", false, "confirm changing the management origin")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(arguments[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitReady
		}
		return exitError
	}
	valid := flags.NArg() == 0 && (*format == "text" || *format == "json")
	switch action {
	case "configure":
		valid = valid && *yes && panelconfig.Hostname(*hostname) == nil && *certificate != "" && *key != "" && *email == "" && !*terms
	case "issue":
		valid = valid && *yes && *terms && panelconfig.Hostname(*hostname) == nil && panelconfig.Email(*email) == nil && *certificate == "" && *key == ""
	case "disable", "recover", "renew":
		valid = valid && *yes && *hostname == "" && *certificate == "" && *key == ""
		valid = valid && *email == "" && !*terms
	case "status":
		valid = valid && !*yes && *hostname == "" && *certificate == "" && *key == ""
		valid = valid && *email == "" && !*terms
	default:
		valid = false
	}
	if !valid {
		writeUsage(stderr)
		return exitError
	}
	status, err := manage(ctx, hostnginx.PanelRequest{Action: action, Hostname: *hostname, CertificatePath: *certificate, PrivateKeyPath: *key, Email: *email, AcceptTerms: *terms})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "panel hostname operation failed:", err)
		return exitError
	}
	if *format == "json" {
		if json.NewEncoder(stdout).Encode(status) != nil {
			return exitError
		}
	} else {
		if status.Enabled {
			_, err = fmt.Fprintf(stdout, "Panel endpoint: %s\nCertificate expires: %s\nAutomatic renewal: %t\n", status.URL, status.CertificateExpiresAt.UTC().Format("2006-01-02T15:04:05Z"), status.AutoRenew)
		} else {
			_, err = fmt.Fprintln(stdout, "Custom panel hostname is disabled.")
		}
		if err != nil {
			return exitError
		}
		_, _ = fmt.Fprintln(stdout, "Fallback: https://<server-address>:8443/ (local bootstrap certificate)")
		if status.RecoveryRequired {
			_, _ = fmt.Fprintln(stdout, "Interrupted change: run panel recover --yes before changing hosted domains.")
		}
	}
	return exitReady
}

func manageInstalledPanel(ctx context.Context, request hostnginx.PanelRequest) (hostnginx.PanelStatus, error) {
	if os.Geteuid() != 0 {
		return hostnginx.PanelStatus{}, errors.New("panel management requires root")
	}
	// Serialize even timer-driven renewals with active install/update commands.
	// Nonblocking locks make contention a retryable operator/timer failure.
	updates := updateapply.NewFileStore()
	updateLock, err := updates.AcquireLock()
	if err != nil {
		return hostnginx.PanelStatus{}, err
	}
	defer updateLock.Close()
	installation := installapply.NewFileStore()
	installLock, err := installation.AcquireLock()
	if err != nil {
		return hostnginx.PanelStatus{}, err
	}
	defer installLock.Close()
	installed, exists, err := installation.Load()
	if err != nil || !exists || installed.Status != installapply.InstallComplete {
		return hostnginx.PanelStatus{}, errors.New("complete or recover the installation before managing the panel hostname")
	}
	update, exists, err := updates.Load()
	if err != nil {
		return hostnginx.PanelStatus{}, err
	}
	if exists && update.Status != updateapply.StatusComplete && update.Status != updateapply.StatusRolledBack {
		return hostnginx.PanelStatus{}, errors.New("recover the interrupted platform update before managing the panel hostname")
	}
	return hostnginx.ManagePanel(ctx, request)
}
