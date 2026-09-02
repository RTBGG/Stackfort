// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/updateapply"
)

func TestUpdaterVersionAndUsageDoNotTouchHostState(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := run(t.Context(), []string{"version"}, &stdout, &stderr); exit != exitReady ||
		!strings.Contains(stdout.String(), "stackfort-updater") || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if exit := run(t.Context(), nil, &stdout, &stderr); exit != exitError || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
}

func TestRecoveryPairAcceptsEitherVerifiedUpdaterSide(t *testing.T) {
	journal := commandJournal(updateapply.StatusApplying)
	for _, installed := range []string{"1.0.0", "1.1.0"} {
		current, err := resolveCurrentVersion(commandJournalLoader{journal: journal, exists: true}, installed, "1.1.0")
		if err != nil || current != "1.0.0" {
			t.Fatalf("installed=%s current=%s error=%v", installed, current, err)
		}
	}
	if _, err := resolveCurrentVersion(commandJournalLoader{journal: journal, exists: true}, "1.2.0", "1.1.0"); err == nil {
		t.Fatal("updater outside the immutable journal pair was accepted")
	}
	if _, err := resolveCurrentVersion(commandJournalLoader{journal: journal, exists: true}, "1.0.0", "1.2.0"); err == nil {
		t.Fatal("different recovery target was accepted")
	}
	loadErr := errors.New("journal unavailable")
	if _, err := resolveCurrentVersion(commandJournalLoader{err: loadErr}, "1.0.0", "1.1.0"); !errors.Is(err, loadErr) {
		t.Fatalf("journal error=%v", err)
	}
	journal.Status, journal.CompletedAt = updateapply.StatusRolledBack, journal.UpdatedAt
	current, err := resolveCurrentVersion(commandJournalLoader{journal: journal, exists: true}, "1.0.0", "1.1.0")
	if err != nil || current != "1.0.0" {
		t.Fatalf("terminal current=%s error=%v", current, err)
	}
}

type commandJournalLoader struct {
	journal updateapply.Journal
	exists  bool
	err     error
}

func (loader commandJournalLoader) Load() (updateapply.Journal, bool, error) {
	return loader.journal, loader.exists, loader.err
}

func commandJournal(status updateapply.Status) updateapply.Journal {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	journal := updateapply.Journal{
		SchemaVersion: updateapply.JournalSchemaVersion, Status: status,
		CurrentVersion: "1.0.0", CurrentDigest: strings.Repeat("a", 64),
		TargetVersion: "1.1.0", TargetDigest: strings.Repeat("b", 64),
		StartedAt: now, UpdatedAt: now,
	}
	for _, stage := range []updateapply.StageID{
		updateapply.StageStopServices, updateapply.StageBackupState, updateapply.StageNativePackages,
		updateapply.StagePayload, updateapply.StageConfiguration, updateapply.StageMigration,
		updateapply.StageStartServices, updateapply.StageHealth,
	} {
		journal.Stages = append(journal.Stages, updateapply.StageState{ID: stage, Status: updateapply.StagePending})
	}
	return journal
}

func TestUpdaterRequiresExactConfirmationBeforePlatformCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, arguments := range [][]string{
		{"apply", "--version=1.2.3"},
		{"apply", "--version=v1.2.3", "--yes"},
		{"apply", "--version=1.2.3", "--yes", "extra"},
	} {
		stdout.Reset()
		stderr.Reset()
		if exit := run(t.Context(), arguments, &stdout, &stderr); exit != exitError {
			t.Fatalf("arguments=%#v exit=%d", arguments, exit)
		}
	}
}
