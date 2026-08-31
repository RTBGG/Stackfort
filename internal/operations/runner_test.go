// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

func TestRunnerCompletesHandlerWithCheckpoint(t *testing.T) {
	t.Parallel()

	workerID := testID(t)
	repository := newFakeRepository(t, workerID, "test.success")
	runner, err := NewRunner(repository, map[string]Handler{
		"test.success": HandlerFunc(func(
			ctx context.Context,
			_ core.ClaimedOperation,
			reporter ProgressReporter,
		) (map[string]any, error) {
			if err := reporter.Checkpoint(ctx, "working", 50, "operation.working", map[string]any{"items": 2}); err != nil {
				return nil, err
			}
			return map[string]any{"done": true}, nil
		}),
	}, runnerOptions(workerID))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if repository.checkpoint == nil || repository.checkpoint.Stage != "working" {
		t.Fatalf("checkpoint = %#v", repository.checkpoint)
	}
	if repository.completed == nil || repository.completed.Result["done"] != true {
		t.Fatalf("completion = %#v", repository.completed)
	}
}

func TestRunnerPersistsOnlyStableFailureClassification(t *testing.T) {
	t.Parallel()

	workerID := testID(t)
	repository := newFakeRepository(t, workerID, "test.failure")
	runner, err := NewRunner(repository, map[string]Handler{
		"test.failure": HandlerFunc(func(context.Context, core.ClaimedOperation, ProgressReporter) (map[string]any, error) {
			return nil, &Failure{Code: "operation.temporary_backend_failure", Retryable: true}
		}),
	}, runnerOptions(workerID))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	err = runner.RunOnce(context.Background())
	var runError *RunError
	if !errors.As(err, &runError) || runError.Code != "operation.temporary_backend_failure" {
		t.Fatalf("RunOnce error = %#v", err)
	}
	if repository.failed == nil || repository.failed.ErrorCode != runError.Code || !repository.failed.Retry {
		t.Fatalf("failure transition = %#v", repository.failed)
	}
}

func TestRunnerAcknowledgesCancellationAtCheckpoint(t *testing.T) {
	t.Parallel()

	workerID := testID(t)
	repository := newFakeRepository(t, workerID, "test.cancel")
	repository.checkpointError = core.ErrOperationCancellationRequested
	runner, err := NewRunner(repository, map[string]Handler{
		"test.cancel": HandlerFunc(func(
			ctx context.Context,
			_ core.ClaimedOperation,
			reporter ProgressReporter,
		) (map[string]any, error) {
			if err := reporter.Checkpoint(ctx, "boundary", 20, "operation.boundary", nil); err != nil {
				return nil, err
			}
			return nil, nil
		}),
	}, runnerOptions(workerID))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if repository.acknowledged == nil {
		t.Fatal("cancellation was not acknowledged")
	}
	if repository.completed != nil || repository.failed != nil {
		t.Fatalf("cancelled handler also completed/failed: %#v / %#v", repository.completed, repository.failed)
	}
}

func TestRunnerConvertsPanicWithoutPersistingPanicText(t *testing.T) {
	t.Parallel()

	workerID := testID(t)
	repository := newFakeRepository(t, workerID, "test.panic")
	runner, err := NewRunner(repository, map[string]Handler{
		"test.panic": HandlerFunc(func(context.Context, core.ClaimedOperation, ProgressReporter) (map[string]any, error) {
			panic("sensitive panic detail")
		}),
	}, runnerOptions(workerID))
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	err = runner.RunOnce(context.Background())
	var runError *RunError
	if !errors.As(err, &runError) || runError.Code != "operation.handler_panic" {
		t.Fatalf("RunOnce panic error = %#v", err)
	}
	if repository.failed == nil || repository.failed.ErrorCode != "operation.handler_panic" {
		t.Fatalf("panic failure transition = %#v", repository.failed)
	}
}

func runnerOptions(workerID core.ID) RunnerOptions {
	return RunnerOptions{
		WorkerInstanceID:  workerID,
		LeaseDuration:     5 * time.Second,
		HeartbeatInterval: time.Second,
	}
}

type fakeRepository struct {
	claimed         core.ClaimedOperation
	checkpoint      *core.CheckpointOperationParams
	checkpointError error
	completed       *core.CompleteOperationParams
	failed          *core.FailOperationParams
	acknowledged    *core.AcknowledgeOperationCancellationParams
}

func newFakeRepository(t *testing.T, workerID core.ID, kind string) *fakeRepository {
	t.Helper()
	operationID := testID(t)
	attemptID := testID(t)
	return &fakeRepository{claimed: core.ClaimedOperation{
		Operation: core.Operation{ID: operationID, Kind: kind, Status: core.OperationRunning},
		Attempt: core.OperationAttempt{
			ID: attemptID, OperationID: operationID, WorkerInstanceID: workerID,
			Outcome: core.OperationAttemptRunning,
		},
	}}
}

func (repository *fakeRepository) ClaimNextOperation(
	context.Context,
	core.ClaimOperationParams,
) (core.ClaimedOperation, error) {
	return repository.claimed, nil
}

func (repository *fakeRepository) HeartbeatOperation(
	context.Context,
	core.HeartbeatOperationParams,
) (core.Operation, error) {
	return repository.claimed.Operation, nil
}

func (repository *fakeRepository) CheckpointOperation(
	_ context.Context,
	params core.CheckpointOperationParams,
) (core.Operation, error) {
	repository.checkpoint = &params
	return repository.claimed.Operation, repository.checkpointError
}

func (repository *fakeRepository) CompleteOperation(
	_ context.Context,
	params core.CompleteOperationParams,
) (core.Operation, error) {
	repository.completed = &params
	return repository.claimed.Operation, nil
}

func (repository *fakeRepository) FailOperation(
	_ context.Context,
	params core.FailOperationParams,
) (core.Operation, error) {
	repository.failed = &params
	return repository.claimed.Operation, nil
}

func (repository *fakeRepository) AcknowledgeOperationCancellation(
	_ context.Context,
	params core.AcknowledgeOperationCancellationParams,
) (core.Operation, error) {
	repository.acknowledged = &params
	return repository.claimed.Operation, nil
}

func testID(t *testing.T) core.ID {
	t.Helper()
	id, err := core.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	return id
}
