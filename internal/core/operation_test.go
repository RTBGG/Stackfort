// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func TestOperationIdempotencyReturnsExistingOutcome(t *testing.T) {
	t.Parallel()

	repository, state := newTestRepository(t)
	ctx := context.Background()
	owner := createTestIdentity(t, repository, "operation-idempotency@example.test")
	packageRecord, err := repository.CreatePackage(ctx, CreatePackageParams{
		Name: "Operations", Slug: "operations", Limits: testLimits(5), ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, "Operations", "operations")
	params := CreateOperationParams{
		AccountID:      &account.ID,
		ActorID:        &owner.ID,
		Kind:           "domain.ensure",
		RetryClass:     RetrySafe,
		RequestID:      "operation-request-1",
		IdempotencyKey: "ensure-example-v1",
		Payload: map[string]any{
			"domainId": "example",
			"revision": float64(1),
		},
	}
	first, err := repository.CreateOperation(ctx, params)
	if err != nil {
		t.Fatalf("CreateOperation first: %v", err)
	}
	params.RequestID = "operation-request-replayed"
	replayed, err := repository.CreateOperation(ctx, params)
	if err != nil {
		t.Fatalf("CreateOperation replay: %v", err)
	}
	if replayed.ID != first.ID || replayed.Status != first.Status || replayed.MaxAttempts != 3 {
		t.Fatalf("replayed operation = %#v, first = %#v", replayed, first)
	}
	params.Payload = map[string]any{"domainId": "different"}
	if _, err := repository.CreateOperation(ctx, params); !errors.Is(err, ErrConflict) {
		t.Fatalf("idempotency mismatch error = %v, want ErrConflict", err)
	}

	wrongAccountID, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if _, err := repository.GetOperation(ctx, OperationScope{
		AccountID: &wrongAccountID, OperationID: first.ID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-account GetOperation error = %v, want ErrNotFound", err)
	}
	events, err := repository.ListOperationEvents(ctx, ListOperationEventsParams{
		Scope: OperationScope{AccountID: &account.ID, OperationID: first.ID},
	})
	if err != nil {
		t.Fatalf("ListOperationEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != OperationEventCreated {
		t.Fatalf("idempotent replay events = %#v", events)
	}
	workerID := newTestID(t)
	claimed, err := repository.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerID, Kinds: []string{"domain.ensure"}, LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim idempotent operation: %v", err)
	}
	if _, err := repository.CompleteOperation(ctx, CompleteOperationParams{
		OperationID: first.ID, AttemptID: claimed.Attempt.ID, WorkerInstanceID: workerID,
		Result: map[string]any{"outcome": "applied"},
	}); err != nil {
		t.Fatalf("complete idempotent operation: %v", err)
	}
	params.Payload = map[string]any{"domainId": "example", "revision": float64(1)}
	terminalReplay, err := repository.CreateOperation(ctx, params)
	if err != nil {
		t.Fatalf("CreateOperation terminal replay: %v", err)
	}
	if terminalReplay.ID != first.ID || terminalReplay.Status != OperationSucceeded || terminalReplay.Result["outcome"] != "applied" {
		t.Fatalf("terminal idempotent replay = %#v", terminalReplay)
	}
	var operationCreatedAudits int
	if err := state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM audit_events
			WHERE operation_id = ? AND action = 'operation.created'`, string(first.ID)).Scan(&operationCreatedAudits)
	}); err != nil {
		t.Fatalf("count operation audits: %v", err)
	}
	if operationCreatedAudits != 1 {
		t.Fatalf("operation.created audits = %d, want 1", operationCreatedAudits)
	}

	if _, err := repository.CreateOperation(ctx, CreateOperationParams{
		AccountID:  &account.ID,
		ActorID:    &owner.ID,
		Kind:       "unsafe.payload",
		RetryClass: RetryNone,
		RequestID:  "unsafe-payload",
		Payload:    map[string]any{"password": "must-not-persist"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("secret-bearing operation payload error = %v, want ErrInvalidInput", err)
	}
}

func TestOperationClaimProgressHeartbeatAndCompletion(t *testing.T) {
	t.Parallel()

	repository, state, account, owner := newOperationTestAccount(t, "operation-success")
	clock := newRepositoryClock(repository)
	ctx := context.Background()
	operation := createTestOperation(t, repository, account.ID, owner.ID, "config.apply", RetrySafe, 3, "success")
	workerID := newTestID(t)

	if _, err := repository.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerID, Kinds: []string{"different.kind"}, LeaseDuration: 15 * time.Second,
	}); !errors.Is(err, ErrNoOperationAvailable) {
		t.Fatalf("unsupported-kind claim error = %v, want ErrNoOperationAvailable", err)
	}
	claimed, err := repository.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerID, Kinds: []string{"config.apply"}, LeaseDuration: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("ClaimNextOperation: %v", err)
	}
	if claimed.Operation.ID != operation.ID || claimed.Operation.Status != OperationRunning || claimed.Attempt.AttemptNumber != 1 {
		t.Fatalf("claimed operation = %#v", claimed)
	}
	otherWorkerID := newTestID(t)
	if _, err := repository.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: otherWorkerID, Kinds: []string{"config.apply"}, LeaseDuration: 15 * time.Second,
	}); !errors.Is(err, ErrNoOperationAvailable) {
		t.Fatalf("duplicate claim error = %v, want ErrNoOperationAvailable", err)
	}

	progress, err := repository.CheckpointOperation(ctx, CheckpointOperationParams{
		OperationID:      operation.ID,
		AttemptID:        claimed.Attempt.ID,
		WorkerInstanceID: workerID,
		Stage:            "rendering",
		ProgressPercent:  40,
		MessageCode:      "operation.rendering",
		Details:          map[string]any{"files": float64(3)},
	})
	if err != nil {
		t.Fatalf("CheckpointOperation: %v", err)
	}
	if progress.Stage != "rendering" || progress.ProgressPercent != 40 {
		t.Fatalf("progress operation = %#v", progress)
	}
	if _, err := repository.CheckpointOperation(ctx, CheckpointOperationParams{
		OperationID: operation.ID, AttemptID: claimed.Attempt.ID, WorkerInstanceID: workerID,
		Stage: "rendering", ProgressPercent: 39, MessageCode: "operation.rendering",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("decreasing progress error = %v, want ErrConflict", err)
	}
	if _, err := repository.CheckpointOperation(ctx, CheckpointOperationParams{
		OperationID: operation.ID, AttemptID: claimed.Attempt.ID, WorkerInstanceID: workerID,
		Stage: "rendering", ProgressPercent: 41, MessageCode: "operation.rendering",
		Details: map[string]any{"accessToken": "must-not-persist"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("secret event error = %v, want ErrInvalidInput", err)
	}

	clock.Advance(time.Second)
	heartbeat, err := repository.HeartbeatOperation(ctx, HeartbeatOperationParams{
		OperationID: operation.ID, AttemptID: claimed.Attempt.ID,
		WorkerInstanceID: workerID, LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("HeartbeatOperation: %v", err)
	}
	if heartbeat.LeaseExpiresAt == nil || !heartbeat.LeaseExpiresAt.Equal(clock.Now().Add(30*time.Second)) {
		t.Fatalf("heartbeat lease = %v", heartbeat.LeaseExpiresAt)
	}
	if _, err := repository.HeartbeatOperation(ctx, HeartbeatOperationParams{
		OperationID: operation.ID, AttemptID: claimed.Attempt.ID,
		WorkerInstanceID: otherWorkerID, LeaseDuration: 30 * time.Second,
	}); !errors.Is(err, ErrOperationLeaseLost) {
		t.Fatalf("wrong-worker heartbeat error = %v, want ErrOperationLeaseLost", err)
	}

	completed, err := repository.CompleteOperation(ctx, CompleteOperationParams{
		OperationID: operation.ID, AttemptID: claimed.Attempt.ID, WorkerInstanceID: workerID,
		Result: map[string]any{"appliedRevision": "revision-1"},
	})
	if err != nil {
		t.Fatalf("CompleteOperation: %v", err)
	}
	if completed.Status != OperationSucceeded || completed.ProgressPercent != 100 || completed.Result["appliedRevision"] != "revision-1" {
		t.Fatalf("completed operation = %#v", completed)
	}
	if _, err := repository.CompleteOperation(ctx, CompleteOperationParams{
		OperationID: operation.ID, AttemptID: claimed.Attempt.ID, WorkerInstanceID: workerID,
	}); !errors.Is(err, ErrOperationLeaseLost) {
		t.Fatalf("duplicate completion error = %v, want ErrOperationLeaseLost", err)
	}
	attempts, err := repository.ListOperationAttempts(ctx, OperationScope{AccountID: &account.ID, OperationID: operation.ID})
	if err != nil {
		t.Fatalf("ListOperationAttempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].Outcome != OperationAttemptSucceeded {
		t.Fatalf("attempt history = %#v", attempts)
	}
	events, err := repository.ListOperationEvents(ctx, ListOperationEventsParams{
		Scope: OperationScope{AccountID: &account.ID, OperationID: operation.ID}, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListOperationEvents: %v", err)
	}
	wantTypes := []OperationEventType{OperationEventCreated, OperationEventClaimed, OperationEventProgress, OperationEventSucceeded}
	if len(events) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d: %#v", len(events), len(wantTypes), events)
	}
	for index, want := range wantTypes {
		if events[index].Type != want {
			t.Fatalf("event %d type = %q, want %q", index, events[index].Type, want)
		}
	}
	immutableStatements := []string{
		"DELETE FROM operations WHERE id = ?",
		"UPDATE operations SET request_id = 'rewritten' WHERE id = ?",
		"DELETE FROM operation_events WHERE operation_id = ?",
	}
	for _, statement := range immutableStatements {
		if err := state.Write(ctx, func(executor store.Executor) error {
			_, err := executor.ExecContext(ctx, statement, string(operation.ID))
			return err
		}); err == nil {
			t.Fatalf("operation history accepted mutation %q", statement)
		}
	}
}

func TestOperationCancellationBoundaries(t *testing.T) {
	t.Parallel()

	repository, _, account, owner := newOperationTestAccount(t, "operation-cancel")
	newRepositoryClock(repository)
	ctx := context.Background()

	pending := createTestOperation(t, repository, account.ID, owner.ID, "domain.pending", RetryNone, 1, "pending-cancel")
	cancelled, err := repository.RequestOperationCancellation(ctx, RequestOperationCancellationParams{
		Scope:   OperationScope{AccountID: &account.ID, OperationID: pending.ID},
		ActorID: &owner.ID, RequestID: "cancel-pending",
	})
	if err != nil {
		t.Fatalf("cancel pending: %v", err)
	}
	if cancelled.Status != OperationCancelled || cancelled.CompletedAt == nil {
		t.Fatalf("cancelled pending operation = %#v", cancelled)
	}
	replayed, err := repository.RequestOperationCancellation(ctx, RequestOperationCancellationParams{
		Scope: OperationScope{AccountID: &account.ID, OperationID: pending.ID}, ActorID: &owner.ID,
	})
	if err != nil || replayed.Status != OperationCancelled {
		t.Fatalf("replayed cancellation = %#v, %v", replayed, err)
	}

	running := createTestOperation(t, repository, account.ID, owner.ID, "domain.running", RetrySafe, 3, "running-cancel")
	workerID := newTestID(t)
	claimed, err := repository.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerID, Kinds: []string{"domain.running"}, LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim running: %v", err)
	}
	cancelling, err := repository.RequestOperationCancellation(ctx, RequestOperationCancellationParams{
		Scope:   OperationScope{AccountID: &account.ID, OperationID: running.ID},
		ActorID: &owner.ID, RequestID: "cancel-running",
	})
	if err != nil {
		t.Fatalf("request running cancellation: %v", err)
	}
	if cancelling.Status != OperationCancelling {
		t.Fatalf("cancelling operation = %#v", cancelling)
	}
	if _, err := repository.CheckpointOperation(ctx, CheckpointOperationParams{
		OperationID: running.ID, AttemptID: claimed.Attempt.ID, WorkerInstanceID: workerID,
		Stage: "safe_boundary", ProgressPercent: 10, MessageCode: "operation.safe_boundary",
	}); !errors.Is(err, ErrOperationCancellationRequested) {
		t.Fatalf("checkpoint cancellation error = %v, want ErrOperationCancellationRequested", err)
	}
	if _, err := repository.HeartbeatOperation(ctx, HeartbeatOperationParams{
		OperationID: running.ID, AttemptID: claimed.Attempt.ID,
		WorkerInstanceID: workerID, LeaseDuration: 30 * time.Second,
	}); !errors.Is(err, ErrOperationCancellationRequested) {
		t.Fatalf("heartbeat cancellation error = %v, want ErrOperationCancellationRequested", err)
	}
	cancelled, err = repository.AcknowledgeOperationCancellation(ctx, AcknowledgeOperationCancellationParams{
		OperationID: running.ID, AttemptID: claimed.Attempt.ID, WorkerInstanceID: workerID,
	})
	if err != nil {
		t.Fatalf("AcknowledgeOperationCancellation: %v", err)
	}
	if cancelled.Status != OperationCancelled || cancelled.CurrentAttemptID != nil {
		t.Fatalf("acknowledged cancellation = %#v", cancelled)
	}
}

func TestOperationLeaseRecoveryAndRetryClassification(t *testing.T) {
	t.Parallel()

	repository, state, account, owner := newOperationTestAccount(t, "operation-recovery")
	clock := newRepositoryClock(repository)
	ctx := context.Background()
	workerOne := newTestID(t)
	workerTwo := newTestID(t)

	safe := createTestOperation(t, repository, account.ID, owner.ID, "recover.safe", RetrySafe, 3, "recover-safe")
	firstClaim, err := repository.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerOne, Kinds: []string{"recover.safe"}, LeaseDuration: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim safe: %v", err)
	}
	clock.Advance(6 * time.Second)

	// Simulate a restarted control API using the same durable store. Claiming an
	// empty post-recovery queue must still commit the lease-expiry transition.
	restarted, err := NewRepository(state)
	if err != nil {
		t.Fatalf("NewRepository after restart: %v", err)
	}
	restarted.now = clock.Now
	if _, err := restarted.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerTwo, Kinds: []string{"recover.safe"}, LeaseDuration: 5 * time.Second,
	}); !errors.Is(err, ErrNoOperationAvailable) {
		t.Fatalf("post-recovery early claim error = %v, want ErrNoOperationAvailable", err)
	}
	recovered, err := restarted.GetOperation(ctx, OperationScope{AccountID: &account.ID, OperationID: safe.ID})
	if err != nil {
		t.Fatalf("GetOperation recovered safe: %v", err)
	}
	if recovered.Status != OperationPending || recovered.Stage != "retry_queued" || recovered.NextAttemptAt == nil {
		t.Fatalf("recovered safe operation = %#v", recovered)
	}
	if _, err := repository.CheckpointOperation(ctx, CheckpointOperationParams{
		OperationID: safe.ID, AttemptID: firstClaim.Attempt.ID, WorkerInstanceID: workerOne,
		Stage: "stale", ProgressPercent: 1, MessageCode: "operation.stale",
	}); !errors.Is(err, ErrOperationLeaseLost) {
		t.Fatalf("stale worker error = %v, want ErrOperationLeaseLost", err)
	}

	clock.Advance(5 * time.Second)
	secondClaim, err := restarted.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerTwo, Kinds: []string{"recover.safe"}, LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim recovered safe: %v", err)
	}
	if secondClaim.Attempt.AttemptNumber != 2 {
		t.Fatalf("second attempt number = %d", secondClaim.Attempt.AttemptNumber)
	}
	failedForRetry, err := restarted.FailOperation(ctx, FailOperationParams{
		OperationID: safe.ID, AttemptID: secondClaim.Attempt.ID, WorkerInstanceID: workerTwo,
		ErrorCode: "operation.transient_failure", Retry: true,
	})
	if err != nil {
		t.Fatalf("FailOperation retry: %v", err)
	}
	if failedForRetry.Status != OperationPending || failedForRetry.NextAttemptAt == nil {
		t.Fatalf("retry-scheduled operation = %#v", failedForRetry)
	}
	clock.Advance(10 * time.Second)
	thirdClaim, err := restarted.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerTwo, Kinds: []string{"recover.safe"}, LeaseDuration: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim third attempt: %v", err)
	}
	if thirdClaim.Attempt.AttemptNumber != 3 {
		t.Fatalf("third attempt number = %d", thirdClaim.Attempt.AttemptNumber)
	}
	if _, err := restarted.CompleteOperation(ctx, CompleteOperationParams{
		OperationID: safe.ID, AttemptID: thirdClaim.Attempt.ID, WorkerInstanceID: workerTwo,
	}); err != nil {
		t.Fatalf("complete recovered operation: %v", err)
	}

	manual := createTestOperation(t, restarted, account.ID, owner.ID, "recover.manual", RetryManual, 2, "recover-manual")
	manualClaim, err := restarted.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerOne, Kinds: []string{"recover.manual"}, LeaseDuration: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim manual: %v", err)
	}
	clock.Advance(6 * time.Second)
	if recoveredCount, err := restarted.RecoverExpiredOperations(ctx); err != nil || recoveredCount != 1 {
		t.Fatalf("RecoverExpiredOperations manual = %d, %v", recoveredCount, err)
	}
	manualFailed, err := restarted.GetOperation(ctx, OperationScope{AccountID: &account.ID, OperationID: manual.ID})
	if err != nil {
		t.Fatalf("get manual failed: %v", err)
	}
	if manualFailed.Status != OperationFailed || manualFailed.RetryClass != RetryManual {
		t.Fatalf("manual recovery outcome = %#v", manualFailed)
	}
	manualRetried, err := restarted.RetryOperation(ctx, RetryOperationParams{
		Scope:   OperationScope{AccountID: &account.ID, OperationID: manual.ID},
		ActorID: &owner.ID, RequestID: "retry-manual",
	})
	if err != nil {
		t.Fatalf("RetryOperation manual: %v", err)
	}
	if manualRetried.Status != OperationPending {
		t.Fatalf("manual retry = %#v", manualRetried)
	}
	if _, err := restarted.CheckpointOperation(ctx, CheckpointOperationParams{
		OperationID: manual.ID, AttemptID: manualClaim.Attempt.ID, WorkerInstanceID: workerOne,
		Stage: "stale", ProgressPercent: 1, MessageCode: "operation.stale",
	}); !errors.Is(err, ErrOperationLeaseLost) {
		t.Fatalf("manual stale attempt error = %v", err)
	}

	nonRetryable := createTestOperation(t, restarted, account.ID, owner.ID, "recover.none", RetryNone, 1, "recover-none")
	nonRetryableClaim, err := restarted.ClaimNextOperation(ctx, ClaimOperationParams{
		WorkerInstanceID: workerOne, Kinds: []string{"recover.none"}, LeaseDuration: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("claim non-retryable: %v", err)
	}
	clock.Advance(6 * time.Second)
	if recoveredCount, err := restarted.RecoverExpiredOperations(ctx); err != nil || recoveredCount != 1 {
		t.Fatalf("RecoverExpiredOperations non-retryable = %d, %v", recoveredCount, err)
	}
	nonRetryable, err = restarted.GetOperation(ctx, OperationScope{AccountID: &account.ID, OperationID: nonRetryable.ID})
	if err != nil {
		t.Fatalf("get non-retryable failed: %v", err)
	}
	if nonRetryable.Status != OperationFailed || nonRetryable.ErrorCode != "operation.worker_lease_expired" {
		t.Fatalf("non-retryable recovery outcome = %#v", nonRetryable)
	}
	if _, err := restarted.RetryOperation(ctx, RetryOperationParams{
		Scope: OperationScope{AccountID: &account.ID, OperationID: nonRetryable.ID}, ActorID: &owner.ID,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-retryable retry error = %v, want ErrConflict", err)
	}
	if _, err := restarted.CheckpointOperation(ctx, CheckpointOperationParams{
		OperationID: nonRetryable.ID, AttemptID: nonRetryableClaim.Attempt.ID, WorkerInstanceID: workerOne,
		Stage: "stale", ProgressPercent: 1, MessageCode: "operation.stale",
	}); !errors.Is(err, ErrOperationLeaseLost) {
		t.Fatalf("non-retryable stale attempt error = %v", err)
	}
}

func TestConcurrentWorkersClaimOperationOnce(t *testing.T) {
	t.Parallel()

	repository, _, account, owner := newOperationTestAccount(t, "operation-concurrent")
	newRepositoryClock(repository)
	createTestOperation(t, repository, account.ID, owner.ID, "concurrent.claim", RetrySafe, 3, "concurrent")

	var claimed atomic.Int64
	var waitGroup sync.WaitGroup
	for range 12 {
		workerID := newTestID(t)
		waitGroup.Add(1)
		go func(workerID ID) {
			defer waitGroup.Done()
			_, err := repository.ClaimNextOperation(context.Background(), ClaimOperationParams{
				WorkerInstanceID: workerID,
				Kinds:            []string{"concurrent.claim"},
				LeaseDuration:    30 * time.Second,
			})
			switch {
			case err == nil:
				claimed.Add(1)
			case errors.Is(err, ErrNoOperationAvailable):
			default:
				t.Errorf("ClaimNextOperation: %v", err)
			}
		}(workerID)
	}
	waitGroup.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("successful claims = %d, want 1", claimed.Load())
	}
}

type repositoryClock struct {
	mu    sync.Mutex
	value time.Time
}

func newRepositoryClock(repository *Repository) *repositoryClock {
	clock := &repositoryClock{value: time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)}
	repository.now = clock.Now
	return clock
}

func (clock *repositoryClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.value
}

func (clock *repositoryClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.value = clock.value.Add(duration)
}

func newOperationTestAccount(t *testing.T, slug string) (*Repository, *store.Store, HostingAccount, Identity) {
	t.Helper()
	repository, state := newTestRepository(t)
	owner := createTestIdentity(t, repository, slug+"@example.test")
	packageRecord, err := repository.CreatePackage(context.Background(), CreatePackageParams{
		Name: slug, Slug: slug, Limits: testLimits(10), ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	account := createTestAccount(t, repository, owner.ID, packageRecord.ID, slug, slug)
	return repository, state, account, owner
}

func createTestOperation(
	t *testing.T,
	repository *Repository,
	accountID ID,
	ownerID ID,
	kind string,
	retryClass RetryClass,
	maxAttempts int64,
	idempotencyKey string,
) Operation {
	t.Helper()
	operation, err := repository.CreateOperation(context.Background(), CreateOperationParams{
		AccountID: &accountID, ActorID: &ownerID, Kind: kind, RetryClass: retryClass,
		RequestID: kind + "-request", IdempotencyKey: idempotencyKey,
		Payload: map[string]any{"kind": kind}, MaxAttempts: maxAttempts,
	})
	if err != nil {
		t.Fatalf("CreateOperation(%q): %v", kind, err)
	}
	return operation
}

func newTestID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	return id
}
