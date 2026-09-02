// SPDX-License-Identifier: AGPL-3.0-or-later

package hostupdates

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/updateapply"
)

type runnerStub struct {
	invocation agentexec.Invocation
	result     agentexec.Result
	err        error
}

func (stub *runnerStub) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	stub.invocation = invocation
	return stub.result, stub.err
}

type journalStoreStub struct {
	journal updateapply.Journal
	exists  bool
	err     error
}

func (stub journalStoreStub) Load() (updateapply.Journal, bool, error) {
	return stub.journal, stub.exists, stub.err
}

func TestManagerStartsOnlyFixedCanonicalUpdateUnit(t *testing.T) {
	runner := &runnerStub{}
	manager := &Manager{runner: runner, store: journalStoreStub{}}
	if err := manager.Start(context.Background(), "1.2.3-beta.4"); err != nil {
		t.Fatal(err)
	}
	want := agentexec.Invocation{Profile: agentexec.ProfileSystemdStartPlatformUpdate, Values: []string{"1.2.3-beta.4"}}
	if !reflect.DeepEqual(runner.invocation, want) {
		t.Fatalf("invocation = %#v", runner.invocation)
	}
	if err := manager.Start(context.Background(), "1.2.3;reboot"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid version error = %v", err)
	}
}

func TestManagerRejectsActiveOrUntrustedJournal(t *testing.T) {
	active := validJournal(updateapply.StatusApplying)
	manager := &Manager{runner: &runnerStub{}, store: journalStoreStub{journal: active, exists: true}}
	if err := manager.Start(context.Background(), "1.2.4"); !errors.Is(err, ErrConflict) {
		t.Fatalf("active journal error = %v", err)
	}
	active.SchemaVersion = 999
	manager.store = journalStoreStub{journal: active, exists: true}
	if _, err := manager.Status(context.Background()); !errors.Is(err, ErrConflict) {
		t.Fatalf("unsafe journal status error = %v", err)
	}
}

func TestManagerReturnsBoundedStatus(t *testing.T) {
	journal := validJournal(updateapply.StatusComplete)
	manager := &Manager{runner: &runnerStub{}, store: journalStoreStub{journal: journal, exists: true}}
	status, err := manager.Status(context.Background())
	if err != nil || status.State != "complete" || status.TargetVersion != "1.2.3" || status.CompletedAt == "" {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func validJournal(status updateapply.Status) updateapply.Journal {
	now := "2026-09-02T10:00:00Z"
	stages := []updateapply.StageID{
		updateapply.StageStopServices, updateapply.StageBackupState, updateapply.StageNativePackages,
		updateapply.StagePayload, updateapply.StageConfiguration, updateapply.StageMigration,
		updateapply.StageStartServices, updateapply.StageHealth,
	}
	result := updateapply.Journal{SchemaVersion: updateapply.JournalSchemaVersion, Status: status,
		CurrentVersion: "1.2.2", CurrentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TargetVersion: "1.2.3", TargetDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		StartedAt: now, UpdatedAt: "2026-09-02T10:01:00Z"}
	if status == updateapply.StatusComplete || status == updateapply.StatusRolledBack ||
		status == updateapply.StatusRollbackFailed {
		result.CompletedAt = "2026-09-02T10:01:00Z"
	}
	for _, stage := range stages {
		result.Stages = append(result.Stages, updateapply.StageState{
			ID: stage, Status: updateapply.StageComplete, Attempts: 1, StartedAt: now, CompletedAt: now,
		})
	}
	return result
}
