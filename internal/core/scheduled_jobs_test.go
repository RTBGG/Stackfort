// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

func TestScheduledJobLifecycleIsRevisionFencedReplaySafeAndTenantScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, _ := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "jobs-owner@example.test")
	account := createManagedDatabaseTestAccount(t, repository, owner, "jobs-account")
	other := createManagedDatabaseTestAccount(t, repository, owner, "jobs-other")

	create := PrepareScheduledJobCreateParams{
		AccountID: account.ID, Name: "Refresh cache", Runtime: scheduledjobs.RuntimeShell,
		ScriptPath: "jobs/refresh.sh", Schedule: scheduledjobs.Schedule{
			Kind: scheduledjobs.ScheduleInterval, IntervalMinutes: 15,
		}, Enabled: true, ActorID: owner.ID, RequestID: "jobs-create", IdempotencyKey: "jobs-create",
	}
	prepared, err := repository.PrepareScheduledJobCreate(ctx, create)
	if err != nil {
		t.Fatalf("PrepareScheduledJobCreate: %v", err)
	}
	if prepared.Job.Status != ScheduledJobPending || prepared.Job.Revision != 1 ||
		prepared.Job.ID != prepared.Operation.ID || prepared.Action != ScheduledJobMutationCreate {
		t.Fatalf("prepared job = %#v", prepared)
	}
	replay, err := repository.PrepareScheduledJobCreate(ctx, create)
	if err != nil || replay.Operation.ID != prepared.Operation.ID || replay.Job.ID != prepared.Job.ID {
		t.Fatalf("create replay = %#v, %v", replay, err)
	}
	if _, err := repository.GetScheduledJob(ctx, other.ID, prepared.Job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account read error = %v", err)
	}

	applied, err := repository.CompleteScheduledJobMutation(ctx, CompleteScheduledJobMutationParams{
		AccountID: account.ID, OperationID: prepared.Operation.ID, ActorID: &owner.ID, RequestID: "jobs-create-applied",
	})
	if err != nil || applied.Job.Status != ScheduledJobActive || applied.Job.AppliedRevision == nil || *applied.Job.AppliedRevision != 1 {
		t.Fatalf("create completion = %#v, %v", applied, err)
	}
	if _, err := repository.CompleteScheduledJobMutation(ctx, CompleteScheduledJobMutationParams{
		AccountID: account.ID, OperationID: prepared.Operation.ID, ActorID: &owner.ID, RequestID: "jobs-create-applied-replay",
	}); err != nil {
		t.Fatalf("completion replay: %v", err)
	}

	update := PrepareScheduledJobUpdateParams{
		AccountID: account.ID, JobID: prepared.Job.ID, ExpectedRevision: 1,
		Name: "Warm application", Runtime: scheduledjobs.RuntimePHP, ScriptPath: "jobs/warm.php", PHPVersion: "8.4",
		Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleWeekly, Weekday: scheduledjobs.Monday, HourUTC: 2, MinuteUTC: 30},
		Enabled:  false, ActorID: owner.ID, RequestID: "jobs-update", IdempotencyKey: "jobs-update",
	}
	updated, err := repository.PrepareScheduledJobUpdate(ctx, update)
	if err != nil || updated.Job.Status != ScheduledJobPending || updated.Job.Revision != 2 || updated.Job.Enabled {
		t.Fatalf("prepared update = %#v, %v", updated, err)
	}
	stale := update
	stale.RequestID, stale.IdempotencyKey = "jobs-stale", "jobs-stale"
	if _, err := repository.PrepareScheduledJobUpdate(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	updated, err = repository.CompleteScheduledJobMutation(ctx, CompleteScheduledJobMutationParams{
		AccountID: account.ID, OperationID: updated.Operation.ID, ActorID: &owner.ID, RequestID: "jobs-update-applied",
	})
	if err != nil || updated.Job.Status != ScheduledJobDisabled || updated.Job.AppliedRevision == nil || *updated.Job.AppliedRevision != 2 {
		t.Fatalf("update completion = %#v, %v", updated, err)
	}

	deleted, err := repository.PrepareScheduledJobDelete(ctx, PrepareScheduledJobDeleteParams{
		AccountID: account.ID, JobID: prepared.Job.ID, ExpectedRevision: 2,
		ActorID: owner.ID, RequestID: "jobs-delete", IdempotencyKey: "jobs-delete",
	})
	if err != nil || deleted.Job.Status != ScheduledJobDeleting || deleted.Job.Revision != 3 {
		t.Fatalf("prepared delete = %#v, %v", deleted, err)
	}
	deleted, err = repository.CompleteScheduledJobMutation(ctx, CompleteScheduledJobMutationParams{
		AccountID: account.ID, OperationID: deleted.Operation.ID, ActorID: &owner.ID, RequestID: "jobs-delete-applied",
	})
	if err != nil || deleted.Job.Status != ScheduledJobDeleted || deleted.Job.RemovedAt == nil {
		t.Fatalf("delete completion = %#v, %v", deleted, err)
	}
	jobs, err := repository.ListScheduledJobs(ctx, account.ID)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs after delete = %#v, %v", jobs, err)
	}
}

func TestScheduledJobsEnforcePackageLimitAndPHPAllowlist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, _ := newManagedDatabaseTestRepository(t)
	owner := createTestIdentity(t, repository, "jobs-limits@example.test")
	account := createManagedDatabaseTestAccount(t, repository, owner, "jobs-limits")
	phpAccount := createManagedDatabaseTestAccount(t, repository, owner, "jobs-php-limit")
	for index, path := range []string{"jobs/one.sh", "jobs/two.sh", "jobs/three.sh"} {
		request := "job-limit-" + string(rune('a'+index))
		mutation, err := repository.PrepareScheduledJobCreate(ctx, PrepareScheduledJobCreateParams{
			AccountID: account.ID, Name: request, Runtime: scheduledjobs.RuntimeShell, ScriptPath: path,
			Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleHourly, MinuteUTC: uint8(index)},
			ActorID:  owner.ID, RequestID: request, IdempotencyKey: request,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repository.CompleteScheduledJobMutation(ctx, CompleteScheduledJobMutationParams{
			AccountID: account.ID, OperationID: mutation.Operation.ID, ActorID: &owner.ID, RequestID: request + "-applied",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.PrepareScheduledJobCreate(ctx, PrepareScheduledJobCreateParams{
		AccountID: account.ID, Name: "overflow", Runtime: scheduledjobs.RuntimeShell, ScriptPath: "jobs/four.sh",
		Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleDaily, HourUTC: 1},
		ActorID:  owner.ID, RequestID: "job-overflow", IdempotencyKey: "job-overflow",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("job limit error = %v", err)
	}
	if _, err := repository.PrepareScheduledJobCreate(ctx, PrepareScheduledJobCreateParams{
		AccountID: phpAccount.ID, Name: "unsupported PHP", Runtime: scheduledjobs.RuntimePHP,
		ScriptPath: "jobs/new.php", PHPVersion: "8.3",
		Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleDaily, HourUTC: 1},
		ActorID:  owner.ID, RequestID: "job-php", IdempotencyKey: "job-php",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("PHP allowlist error = %v", err)
	}
}
