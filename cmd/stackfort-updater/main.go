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
	"os/signal"
	"runtime"
	"syscall"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/updateapply"
)

const (
	exitReady = 0
	exitError = 1
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 1 && arguments[0] == "version" {
		build := buildinfo.Current()
		_, err := fmt.Fprintf(stdout, "stackfort-updater %s (%s, %s)\n", build.Version, build.Commit, build.BuildDate)
		if err != nil {
			return exitError
		}
		return exitReady
	}
	if len(arguments) == 1 && arguments[0] == "status" {
		return runStatus(stdout, stderr)
	}
	if len(arguments) > 0 && arguments[0] == "apply" {
		return runApply(ctx, arguments[1:], stdout, stderr)
	}
	writeUsage(stderr)
	return exitError
}

func runStatus(stdout, stderr io.Writer) int {
	journal, exists, err := updateapply.NewFileStore().Load()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "load updater status:", err)
		return exitError
	}
	if !exists {
		_, _ = fmt.Fprintln(stdout, "No staged update has run.")
		return exitReady
	}
	if err := updateapply.ValidateJournal(journal); err != nil {
		_, _ = fmt.Fprintln(stderr, "validate updater status:", err)
		return exitError
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(journal); err != nil {
		return exitError
	}
	return exitReady
}

func runApply(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	targetVersion := flags.String("version", "", "exact immutable target release version")
	format := flags.String("format", "text", "output format: text or json")
	confirmed := flags.Bool("yes", false, "confirm staged activation and bounded downtime")
	if err := flags.Parse(arguments); err != nil {
		return exitError
	}
	if flags.NArg() != 0 || *targetVersion == "" || !*confirmed || (*format != "text" && *format != "json") {
		_, _ = fmt.Fprintln(stderr, "apply requires --version=X.Y.Z, --yes, and --format=text|json")
		return exitError
	}
	if runtime.GOOS != "linux" {
		_, _ = fmt.Fprintln(stderr, "functional Stackfort updates require Linux")
		return exitError
	}
	store := updateapply.NewFileStore()
	lock, err := store.AcquireLock()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "acquire updater lock:", err)
		return exitError
	}
	defer lock.Close()
	currentVersion, err := resolveCurrentVersion(store, buildinfo.Current().Version, *targetVersion)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "resolve updater recovery pair:", err)
		return exitError
	}
	if comparison, err := updateapply.CompareVersions(currentVersion, *targetVersion); err != nil || comparison >= 0 {
		_, _ = fmt.Fprintln(stderr, "target release must be newer than the installed canonical release")
		return exitError
	}
	stager, err := updateapply.NewStager()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize release stager:", err)
		return exitError
	}
	current, target, err := stager.PreparePair(ctx, currentVersion, *targetVersion)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "prepare verified release pair:", err)
		return exitError
	}
	for _, prepared := range []updateapply.PreparedRelease{current, target} {
		if err := installapply.ValidateSourceTrust(prepared.Source); err != nil {
			_, _ = fmt.Fprintln(stderr, "release source trust validation failed:", err)
			return exitError
		}
	}
	runner, err := updateapply.NewLinuxRunner(stderr, target.Source)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize Linux updater:", err)
		return exitError
	}
	engine, err := updateapply.NewEngine(store, runner)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize update transaction:", err)
		return exitError
	}
	result, applyErr := engine.Apply(ctx, current.Source, target.Source)
	if result.Status != "" {
		if err := writeResult(stdout, *format, result); err != nil {
			_, _ = fmt.Fprintln(stderr, "write updater result:", err)
			return exitError
		}
	}
	if applyErr != nil {
		_, _ = fmt.Fprintln(stderr, "Stackfort update failed:", applyErr)
		return exitError
	}
	return exitReady
}

type journalLoader interface {
	Load() (updateapply.Journal, bool, error)
}

func resolveCurrentVersion(store journalLoader, installedVersion, requestedTarget string) (string, error) {
	journal, exists, err := store.Load()
	if err != nil {
		return "", err
	}
	if !exists {
		return installedVersion, nil
	}
	if err := updateapply.ValidateJournal(journal); err != nil {
		return "", err
	}
	if journal.Status != updateapply.StatusApplying && journal.Status != updateapply.StatusRollingBack &&
		journal.Status != updateapply.StatusRollbackFailed {
		return installedVersion, nil
	}
	if journal.TargetVersion != requestedTarget {
		return "", errors.New("non-terminal journal belongs to a different target")
	}
	if installedVersion != journal.CurrentVersion && installedVersion != journal.TargetVersion {
		return "", errors.New("running updater does not belong to the journal source pair")
	}
	return journal.CurrentVersion, nil
}

func writeResult(output io.Writer, format string, result updateapply.Result) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if result.Status == "" {
		return errors.New("updater returned no durable result")
	}
	_, err := fmt.Fprintf(output, "Stackfort update: %s\nFrom: %s\nTo: %s\nRecovered: %t\n",
		result.Status, result.CurrentVersion, result.TargetVersion, result.Recovered)
	if err != nil {
		return err
	}
	for _, stage := range result.Stages {
		if _, err := fmt.Fprintf(output, "[%s] %s (attempts=%d)\n", stage.Status, stage.ID, stage.Attempts); err != nil {
			return err
		}
	}
	return nil
}

func writeUsage(output io.Writer) {
	_, _ = fmt.Fprintln(output, "usage: stackfort-updater apply --version=X.Y.Z --yes [--format=text|json]")
	_, _ = fmt.Fprintln(output, "       stackfort-updater status")
	_, _ = fmt.Fprintln(output, "       stackfort-updater version")
}
