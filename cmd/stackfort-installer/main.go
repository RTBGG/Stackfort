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

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/installpreflight"
)

const (
	exitReady   = 0
	exitError   = 1
	exitBlocked = 2
)

type inspectFunc func(context.Context) (installpreflight.Result, error)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, installpreflight.Inspect))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer, inspect inspectFunc) int {
	if len(arguments) == 1 && arguments[0] == "version" {
		build := buildinfo.Current()
		if _, err := fmt.Fprintf(stdout, "stackfort-installer %s (%s, %s)\n", build.Version, build.Commit, build.BuildDate); err != nil {
			return exitError
		}
		return exitReady
	}
	if len(arguments) > 0 && arguments[0] == "install" {
		return runInstall(ctx, arguments[1:], stdout, stderr)
	}
	if len(arguments) == 0 || arguments[0] != "preflight" {
		writeUsage(stderr)
		return exitError
	}
	flags := flag.NewFlagSet("preflight", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(arguments[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitReady
		}
		return exitError
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		_, _ = fmt.Fprintln(stderr, "preflight accepts only --format=text or --format=json")
		return exitError
	}

	result, err := inspect(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "preflight inspection failed:", err)
		return exitError
	}
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			_, _ = fmt.Fprintln(stderr, "write preflight report:", err)
			return exitError
		}
	} else if err := installpreflight.WriteText(stdout, result); err != nil {
		_, _ = fmt.Fprintln(stderr, "write preflight report:", err)
		return exitError
	}
	if !result.Ready {
		return exitBlocked
	}
	return exitReady
}

func writeUsage(output io.Writer) {
	_, _ = fmt.Fprintln(output, "usage: stackfort-installer preflight [--format=text|json]")
	_, _ = fmt.Fprintln(output, "       stackfort-installer install --source-dir=/absolute/release/path --yes [--format=text|json]")
	_, _ = fmt.Fprintln(output, "       stackfort-installer version")
}

func runInstall(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourceDirectory := flags.String("source-dir", "", "absolute extracted release directory")
	format := flags.String("format", "text", "output format: text or json")
	confirmed := flags.Bool("yes", false, "confirm the printed I-001 installation plan")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitReady
		}
		return exitError
	}
	if flags.NArg() != 0 || *sourceDirectory == "" || !*confirmed || (*format != "text" && *format != "json") {
		_, _ = fmt.Fprintln(stderr, "install requires --source-dir=/absolute/path, --yes, and --format=text|json")
		return exitError
	}
	source, err := installapply.InspectSource(*sourceDirectory)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "release source validation failed:", err)
		return exitError
	}
	build := buildinfo.Current()
	if build.Version != "dev" && build.Version != source.Version {
		_, _ = fmt.Fprintf(stderr, "installer version %s cannot install release %s\n", build.Version, source.Version)
		return exitError
	}
	if err := installapply.ValidateSourceTrust(source); err != nil {
		_, _ = fmt.Fprintln(stderr, "release source trust validation failed:", err)
		return exitError
	}
	runner, err := installapply.NewLinuxRunner(stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize Linux installer:", err)
		return exitError
	}
	store := installapply.NewFileStore()
	_, journalExists, loadErr := store.Load()
	if loadErr != nil {
		_, _ = fmt.Fprintln(stderr, "load installation journal:", loadErr)
		return exitError
	}
	if !journalExists {
		if err := runner.Preflight(ctx); err != nil {
			return writeInstallError(err, stdout, stderr)
		}
	}
	lock, err := store.AcquireLock()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "acquire installer lock:", err)
		return exitError
	}
	defer lock.Close()
	engine, err := installapply.NewEngine(store, runner)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize installer engine:", err)
		return exitError
	}
	result, err := engine.Install(ctx, source)
	if err != nil {
		exit := writeInstallError(err, stdout, stderr)
		if result.Version != "" {
			_ = writeInstallResult(stdout, *format, result)
		}
		return exit
	}
	if err := writeInstallResult(stdout, *format, result); err != nil {
		_, _ = fmt.Fprintln(stderr, "write installation result:", err)
		return exitError
	}
	return exitReady
}

func writeInstallError(err error, stdout, stderr io.Writer) int {
	var preflight *installapply.PreflightError
	if errors.As(err, &preflight) {
		_ = installpreflight.WriteText(stdout, preflight.Result)
		return exitBlocked
	}
	_, _ = fmt.Fprintln(stderr, "installation failed:", err)
	return exitError
}

func writeInstallResult(output io.Writer, format string, result installapply.Result) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	_, err := fmt.Fprintf(output,
		"Stackfort installation: %s\nVersion: %s\nChanged: %t\nResumed: %t\nAlready installed: %t\n",
		result.Status, result.Version, result.Changed, result.Resumed, result.AlreadyInstalled,
	)
	if err != nil {
		return err
	}
	for _, stage := range result.Stages {
		if _, err := fmt.Fprintf(output, "[%s] %s (attempts=%d, changed=%t)\n",
			stage.Status, stage.ID, stage.Attempts, stage.Changed); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(output,
		"Management endpoint: https://<server-address>:8443/\n"+
			"The initial certificate is locally generated; verify the server address before accepting it.\n"+
			"Create the one-time administrator capability with:\n"+
			"  sudo -u stackfort -- /usr/local/bin/stackfort-api bootstrap create",
	)
	return err
}
