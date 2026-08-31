// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

func (r *Repository) ClaimNextOperation(ctx context.Context, params ClaimOperationParams) (ClaimedOperation, error) {
	if err := validateID(params.WorkerInstanceID, "workerInstanceId"); err != nil {
		return ClaimedOperation{}, err
	}
	kinds, err := normalizeWorkerKinds(params.Kinds)
	if err != nil {
		return ClaimedOperation{}, err
	}
	if err := validateLeaseDuration(params.LeaseDuration); err != nil {
		return ClaimedOperation{}, err
	}
	now := r.timestamp()
	leaseExpiresAt := now.Add(params.LeaseDuration)
	var claimed ClaimedOperation
	noOperationAvailable := false

	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.recoverExpiredOperationsTx(ctx, executor, now); err != nil {
			return err
		}
		query := `
			SELECT id
			FROM operations
			WHERE status = 'pending'
			  AND julianday(next_attempt_at) <= julianday(?)
			  AND attempt_count < max_attempts
			  AND kind IN (` + buildKindPlaceholders(len(kinds)) + `)
			ORDER BY julianday(next_attempt_at), created_at, id
			LIMIT 1`
		arguments := make([]any, 0, len(kinds)+1)
		arguments = append(arguments, formatTime(now))
		for _, kind := range kinds {
			arguments = append(arguments, kind)
		}
		var operationID ID
		if err := executor.QueryRowContext(ctx, query, arguments...).Scan(&operationID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				noOperationAvailable = true
				return nil
			}
			return err
		}
		operation, err := loadOperationByID(ctx, executor, operationID)
		if err != nil {
			return err
		}
		attemptID, err := r.newID()
		if err != nil {
			return err
		}
		attempt := OperationAttempt{
			ID:               attemptID,
			OperationID:      operation.ID,
			AttemptNumber:    operation.AttemptCount + 1,
			WorkerInstanceID: params.WorkerInstanceID,
			ClaimedAt:        now,
			HeartbeatAt:      now,
			LeaseExpiresAt:   leaseExpiresAt,
			Outcome:          OperationAttemptRunning,
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO operation_attempts (
				id, operation_id, attempt_number, worker_instance_id,
				claimed_at, heartbeat_at, lease_expires_at, outcome
			) VALUES (?, ?, ?, ?, ?, ?, ?, 'running')`,
			string(attempt.ID),
			string(attempt.OperationID),
			attempt.AttemptNumber,
			string(attempt.WorkerInstanceID),
			formatTime(now),
			formatTime(now),
			formatTime(leaseExpiresAt),
		)
		if err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET status = 'running',
			    attempt_count = ?,
			    current_attempt_id = ?,
			    worker_instance_id = ?,
			    lease_expires_at = ?,
			    next_attempt_at = NULL,
			    started_at = COALESCE(started_at, ?),
			    updated_at = ?
			WHERE id = ? AND status = 'pending'`,
			attempt.AttemptNumber,
			string(attempt.ID),
			string(attempt.WorkerInstanceID),
			formatTime(leaseExpiresAt),
			formatTime(now),
			formatTime(now),
			string(operation.ID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return err
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   &attempt.ID,
			Type:        OperationEventClaimed,
			Stage:       operation.Stage,
			Progress:    operation.ProgressPercent,
			MessageCode: "operation.claimed",
			Details: map[string]any{
				"attempt": attempt.AttemptNumber,
			},
		}, now); err != nil {
			return err
		}
		operation, err = loadOperationByID(ctx, executor, operation.ID)
		if err != nil {
			return err
		}
		claimed = ClaimedOperation{Operation: operation, Attempt: attempt}
		return nil
	})
	if err != nil {
		return ClaimedOperation{}, classifyDatabaseError(err)
	}
	if noOperationAvailable {
		return ClaimedOperation{}, ErrNoOperationAvailable
	}
	return claimed, nil
}

func (r *Repository) HeartbeatOperation(ctx context.Context, params HeartbeatOperationParams) (Operation, error) {
	if err := validateAttemptIdentity(params.OperationID, params.AttemptID, params.WorkerInstanceID); err != nil {
		return Operation{}, err
	}
	if err := validateLeaseDuration(params.LeaseDuration); err != nil {
		return Operation{}, err
	}
	now := r.timestamp()
	leaseExpiresAt := now.Add(params.LeaseDuration)
	var operation Operation
	cancellationRequested := false
	err := r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		operation, err = loadOperationByID(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		if err := validateActiveAttempt(operation, params.AttemptID, params.WorkerInstanceID, now); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE operation_attempts
			SET heartbeat_at = ?, lease_expires_at = ?
			WHERE id = ? AND operation_id = ? AND worker_instance_id = ? AND outcome = 'running'`,
			formatTime(now),
			formatTime(leaseExpiresAt),
			string(params.AttemptID),
			string(params.OperationID),
			string(params.WorkerInstanceID),
		); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET lease_expires_at = ?, updated_at = ?
			WHERE id = ? AND current_attempt_id = ? AND worker_instance_id = ?
			  AND status IN ('running', 'cancelling')`,
			formatTime(leaseExpiresAt),
			formatTime(now),
			string(params.OperationID),
			string(params.AttemptID),
			string(params.WorkerInstanceID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return ErrOperationLeaseLost
		}
		operation.LeaseExpiresAt = timePointer(leaseExpiresAt)
		operation.UpdatedAt = now
		cancellationRequested = operation.Status == OperationCancelling
		return nil
	})
	if err != nil {
		return Operation{}, classifyOperationTransitionError(err)
	}
	if cancellationRequested {
		return operation, ErrOperationCancellationRequested
	}
	return operation, nil
}

func (r *Repository) CheckpointOperation(ctx context.Context, params CheckpointOperationParams) (Operation, error) {
	if err := validateAttemptIdentity(params.OperationID, params.AttemptID, params.WorkerInstanceID); err != nil {
		return Operation{}, err
	}
	stage, err := validateAction(params.Stage, "stage", 80)
	if err != nil {
		return Operation{}, err
	}
	messageCode, err := validateAction(params.MessageCode, "messageCode", 80)
	if err != nil {
		return Operation{}, err
	}
	if params.ProgressPercent < 0 || params.ProgressPercent > 99 {
		return Operation{}, fmt.Errorf("%w: running progress must be between 0 and 99", ErrInvalidInput)
	}
	if _, _, err := encodeSafeOperationObject(params.Details, maxOperationEventDetailsBytes); err != nil {
		return Operation{}, err
	}
	now := r.timestamp()
	var operation Operation
	cancellationRequested := false
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		operation, err = loadOperationByID(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		if err := validateActiveAttempt(operation, params.AttemptID, params.WorkerInstanceID, now); err != nil {
			return err
		}
		if operation.Status == OperationCancelling {
			cancellationRequested = true
			return nil
		}
		if params.ProgressPercent < operation.ProgressPercent {
			return fmt.Errorf("%w: operation progress cannot decrease", ErrConflict)
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET stage = ?, progress_percent = ?, updated_at = ?
			WHERE id = ? AND current_attempt_id = ? AND worker_instance_id = ? AND status = 'running'`,
			stage,
			params.ProgressPercent,
			formatTime(now),
			string(params.OperationID),
			string(params.AttemptID),
			string(params.WorkerInstanceID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return ErrOperationLeaseLost
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   &params.AttemptID,
			Type:        OperationEventProgress,
			Stage:       stage,
			Progress:    params.ProgressPercent,
			MessageCode: messageCode,
			Details:     params.Details,
		}, now); err != nil {
			return err
		}
		operation.Stage = stage
		operation.ProgressPercent = params.ProgressPercent
		operation.UpdatedAt = now
		return nil
	})
	if err != nil {
		return Operation{}, classifyOperationTransitionError(err)
	}
	if cancellationRequested {
		return operation, ErrOperationCancellationRequested
	}
	return operation, nil
}

func (r *Repository) CompleteOperation(ctx context.Context, params CompleteOperationParams) (Operation, error) {
	if err := validateAttemptIdentity(params.OperationID, params.AttemptID, params.WorkerInstanceID); err != nil {
		return Operation{}, err
	}
	resultJSON, resultObject, err := encodeSafeOperationObject(params.Result, maxOperationJSONBytes)
	if err != nil {
		return Operation{}, err
	}
	now := r.timestamp()
	var operation Operation
	cancellationRequested := false
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		operation, err = loadOperationByID(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		if err := validateActiveAttempt(operation, params.AttemptID, params.WorkerInstanceID, now); err != nil {
			return err
		}
		if operation.Status == OperationCancelling {
			cancellationRequested = true
			return nil
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE operation_attempts
			SET outcome = 'succeeded', completed_at = ?
			WHERE id = ? AND operation_id = ? AND worker_instance_id = ? AND outcome = 'running'`,
			formatTime(now),
			string(params.AttemptID),
			string(params.OperationID),
			string(params.WorkerInstanceID),
		); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET status = 'succeeded', stage = 'completed', progress_percent = 100,
			    result_json = ?, error_code = NULL, current_attempt_id = NULL,
			    worker_instance_id = NULL, lease_expires_at = NULL,
			    next_attempt_at = NULL, completed_at = ?, updated_at = ?
			WHERE id = ? AND current_attempt_id = ? AND worker_instance_id = ? AND status = 'running'`,
			resultJSON,
			formatTime(now),
			formatTime(now),
			string(params.OperationID),
			string(params.AttemptID),
			string(params.WorkerInstanceID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return ErrOperationLeaseLost
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   &params.AttemptID,
			Type:        OperationEventSucceeded,
			Stage:       "completed",
			Progress:    100,
			MessageCode: "operation.succeeded",
			Details:     map[string]any{},
		}, now); err != nil {
			return err
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			Action:      "operation.succeeded",
			TargetType:  "operation",
			TargetID:    string(operation.ID),
			AccountID:   operation.AccountID,
			RequestID:   operation.RequestID,
			OperationID: &operation.ID,
			Result:      AuditSuccess,
			Details: map[string]any{
				"attempts": operation.AttemptCount,
				"kind":     operation.Kind,
			},
		}, now); err != nil {
			return err
		}
		operation.Status = OperationSucceeded
		operation.Stage = "completed"
		operation.ProgressPercent = 100
		operation.Result = resultObject
		operation.CurrentAttemptID = nil
		operation.WorkerInstanceID = nil
		operation.LeaseExpiresAt = nil
		operation.CompletedAt = timePointer(now)
		operation.UpdatedAt = now
		return nil
	})
	if err != nil {
		return Operation{}, classifyOperationTransitionError(err)
	}
	if cancellationRequested {
		return operation, ErrOperationCancellationRequested
	}
	return operation, nil
}

func (r *Repository) FailOperation(ctx context.Context, params FailOperationParams) (Operation, error) {
	if err := validateAttemptIdentity(params.OperationID, params.AttemptID, params.WorkerInstanceID); err != nil {
		return Operation{}, err
	}
	errorCode, err := validateAction(params.ErrorCode, "errorCode", 80)
	if err != nil {
		return Operation{}, err
	}
	resultJSON, resultObject, err := encodeSafeOperationObject(params.Result, maxOperationJSONBytes)
	if err != nil {
		return Operation{}, err
	}
	now := r.timestamp()
	var operation Operation
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		operation, err = loadOperationByID(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		if err := validateActiveAttempt(operation, params.AttemptID, params.WorkerInstanceID, now); err != nil {
			return err
		}
		if params.Retry && (operation.Status != OperationRunning || operation.RetryClass != RetrySafe) {
			return fmt.Errorf("%w: only a running safe-retry operation may be retried automatically", ErrInvalidInput)
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE operation_attempts
			SET outcome = 'failed', error_code = ?, completed_at = ?
			WHERE id = ? AND operation_id = ? AND worker_instance_id = ? AND outcome = 'running'`,
			errorCode,
			formatTime(now),
			string(params.AttemptID),
			string(params.OperationID),
			string(params.WorkerInstanceID),
		); err != nil {
			return err
		}
		shouldRetry := params.Retry && operation.AttemptCount < operation.MaxAttempts
		if shouldRetry {
			nextAttemptAt := now.Add(retryDelay(operation.AttemptCount))
			result, err := executor.ExecContext(ctx, `
				UPDATE operations
				SET status = 'pending', stage = 'retry_queued',
				    result_json = NULL, error_code = NULL,
				    current_attempt_id = NULL, worker_instance_id = NULL,
				    lease_expires_at = NULL, next_attempt_at = ?,
				    completed_at = NULL, updated_at = ?
				WHERE id = ? AND current_attempt_id = ? AND worker_instance_id = ? AND status = 'running'`,
				formatTime(nextAttemptAt),
				formatTime(now),
				string(params.OperationID),
				string(params.AttemptID),
				string(params.WorkerInstanceID),
			)
			if err != nil {
				return err
			}
			if err := expectAffected(result); err != nil {
				return ErrOperationLeaseLost
			}
			if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
				OperationID: operation.ID,
				AttemptID:   &params.AttemptID,
				Type:        OperationEventRetryScheduled,
				Stage:       "retry_queued",
				Progress:    operation.ProgressPercent,
				MessageCode: "operation.retry_scheduled",
				Details: map[string]any{
					"attempt":     operation.AttemptCount,
					"errorCode":   errorCode,
					"nextAttempt": formatTime(nextAttemptAt),
				},
			}, now); err != nil {
				return err
			}
			operation.Status = OperationPending
			operation.Stage = "retry_queued"
			operation.CurrentAttemptID = nil
			operation.WorkerInstanceID = nil
			operation.LeaseExpiresAt = nil
			operation.NextAttemptAt = timePointer(nextAttemptAt)
			operation.UpdatedAt = now
			return nil
		}

		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET status = 'failed', stage = 'failed', result_json = ?, error_code = ?,
			    current_attempt_id = NULL, worker_instance_id = NULL,
			    lease_expires_at = NULL, next_attempt_at = NULL,
			    completed_at = ?, updated_at = ?
			WHERE id = ? AND current_attempt_id = ? AND worker_instance_id = ?
			  AND status IN ('running', 'cancelling')`,
			resultJSON,
			errorCode,
			formatTime(now),
			formatTime(now),
			string(params.OperationID),
			string(params.AttemptID),
			string(params.WorkerInstanceID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return ErrOperationLeaseLost
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   &params.AttemptID,
			Type:        OperationEventFailed,
			Stage:       "failed",
			Progress:    operation.ProgressPercent,
			MessageCode: "operation.failed",
			Details:     map[string]any{"errorCode": errorCode},
		}, now); err != nil {
			return err
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			Action:      "operation.failed",
			TargetType:  "operation",
			TargetID:    string(operation.ID),
			AccountID:   operation.AccountID,
			RequestID:   operation.RequestID,
			OperationID: &operation.ID,
			Result:      AuditFailure,
			Details: map[string]any{
				"attempts":  operation.AttemptCount,
				"errorCode": errorCode,
				"kind":      operation.Kind,
			},
		}, now); err != nil {
			return err
		}
		operation.Status = OperationFailed
		operation.Stage = "failed"
		operation.Result = resultObject
		operation.ErrorCode = errorCode
		operation.CurrentAttemptID = nil
		operation.WorkerInstanceID = nil
		operation.LeaseExpiresAt = nil
		operation.CompletedAt = timePointer(now)
		operation.UpdatedAt = now
		return nil
	})
	if err != nil {
		return Operation{}, classifyOperationTransitionError(err)
	}
	return operation, nil
}

func (r *Repository) RequestOperationCancellation(
	ctx context.Context,
	params RequestOperationCancellationParams,
) (Operation, error) {
	if err := validateOperationScope(params.Scope); err != nil {
		return Operation{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Operation{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Operation{}, err
	}
	now := r.timestamp()
	var operation Operation
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		operation, err = loadScopedOperation(ctx, executor, params.Scope)
		if err != nil {
			return err
		}
		switch operation.Status {
		case OperationCancelling, OperationCancelled:
			return nil
		case OperationSucceeded, OperationFailed:
			return fmt.Errorf("%w: completed operation cannot be cancelled", ErrConflict)
		case OperationPending, OperationRunning:
		default:
			return fmt.Errorf("%w: operation is not cancellable", ErrConflict)
		}
		newStatus := OperationCancelling
		completedAt := any(nil)
		nextAttemptAt := any(nil)
		stage := operation.Stage
		if operation.Status == OperationPending {
			newStatus = OperationCancelled
			completedAt = formatTime(now)
			stage = "cancelled"
		} else {
			nextAttemptAt = nil
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET status = ?, stage = ?, next_attempt_at = ?,
			    cancellation_requested_at = ?,
			    cancellation_requested_by_identity_id = ?,
			    completed_at = ?, updated_at = ?
			WHERE id = ? AND status = ?`,
			string(newStatus),
			stage,
			nextAttemptAt,
			formatTime(now),
			nullableID(params.ActorID),
			completedAt,
			formatTime(now),
			string(operation.ID),
			string(operation.Status),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return err
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   operation.CurrentAttemptID,
			Type:        OperationEventCancellationRequested,
			Stage:       stage,
			Progress:    operation.ProgressPercent,
			MessageCode: "operation.cancellation_requested",
			Details:     map[string]any{},
		}, now); err != nil {
			return err
		}
		if newStatus == OperationCancelled {
			if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
				OperationID: operation.ID,
				Type:        OperationEventCancelled,
				Stage:       "cancelled",
				Progress:    operation.ProgressPercent,
				MessageCode: "operation.cancelled",
				Details:     map[string]any{"beforeStart": true},
			}, now); err != nil {
				return err
			}
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:     params.ActorID,
			Action:      "operation.cancellation_requested",
			TargetType:  "operation",
			TargetID:    string(operation.ID),
			AccountID:   operation.AccountID,
			RequestID:   requestID,
			OperationID: &operation.ID,
			Result:      AuditSuccess,
			Details: map[string]any{
				"immediate": newStatus == OperationCancelled,
				"kind":      operation.Kind,
			},
		}, now); err != nil {
			return err
		}
		operation.Status = newStatus
		operation.Stage = stage
		operation.CancellationRequestedAt = timePointer(now)
		operation.CancellationRequestedBy = params.ActorID
		operation.UpdatedAt = now
		if newStatus == OperationCancelled {
			operation.NextAttemptAt = nil
			operation.CompletedAt = timePointer(now)
		}
		return nil
	})
	if err != nil {
		return Operation{}, classifyOperationTransitionError(err)
	}
	return operation, nil
}

func (r *Repository) AcknowledgeOperationCancellation(
	ctx context.Context,
	params AcknowledgeOperationCancellationParams,
) (Operation, error) {
	if err := validateAttemptIdentity(params.OperationID, params.AttemptID, params.WorkerInstanceID); err != nil {
		return Operation{}, err
	}
	now := r.timestamp()
	var operation Operation
	err := r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		operation, err = loadOperationByID(ctx, executor, params.OperationID)
		if err != nil {
			return err
		}
		if err := validateActiveAttempt(operation, params.AttemptID, params.WorkerInstanceID, now); err != nil {
			return err
		}
		if operation.Status != OperationCancelling {
			return fmt.Errorf("%w: operation has no cancellation request", ErrConflict)
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE operation_attempts
			SET outcome = 'cancelled', completed_at = ?
			WHERE id = ? AND operation_id = ? AND worker_instance_id = ? AND outcome = 'running'`,
			formatTime(now),
			string(params.AttemptID),
			string(params.OperationID),
			string(params.WorkerInstanceID),
		); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET status = 'cancelled', stage = 'cancelled', result_json = NULL,
			    error_code = NULL, current_attempt_id = NULL,
			    worker_instance_id = NULL, lease_expires_at = NULL,
			    next_attempt_at = NULL, completed_at = ?, updated_at = ?
			WHERE id = ? AND current_attempt_id = ? AND worker_instance_id = ? AND status = 'cancelling'`,
			formatTime(now),
			formatTime(now),
			string(params.OperationID),
			string(params.AttemptID),
			string(params.WorkerInstanceID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return ErrOperationLeaseLost
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   &params.AttemptID,
			Type:        OperationEventCancelled,
			Stage:       "cancelled",
			Progress:    operation.ProgressPercent,
			MessageCode: "operation.cancelled",
			Details:     map[string]any{},
		}, now); err != nil {
			return err
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			Action:      "operation.cancelled",
			TargetType:  "operation",
			TargetID:    string(operation.ID),
			AccountID:   operation.AccountID,
			RequestID:   operation.RequestID,
			OperationID: &operation.ID,
			Result:      AuditSuccess,
			Details:     map[string]any{"kind": operation.Kind},
		}, now); err != nil {
			return err
		}
		operation.Status = OperationCancelled
		operation.Stage = "cancelled"
		operation.CurrentAttemptID = nil
		operation.WorkerInstanceID = nil
		operation.LeaseExpiresAt = nil
		operation.CompletedAt = timePointer(now)
		operation.UpdatedAt = now
		return nil
	})
	if err != nil {
		return Operation{}, classifyOperationTransitionError(err)
	}
	return operation, nil
}

func (r *Repository) RetryOperation(ctx context.Context, params RetryOperationParams) (Operation, error) {
	if err := validateOperationScope(params.Scope); err != nil {
		return Operation{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Operation{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Operation{}, err
	}
	now := r.timestamp()
	var operation Operation
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		operation, err = loadScopedOperation(ctx, executor, params.Scope)
		if err != nil {
			return err
		}
		if operation.Status != OperationFailed {
			return fmt.Errorf("%w: only a failed operation can be retried", ErrConflict)
		}
		if operation.RetryClass == RetryNone {
			return fmt.Errorf("%w: operation is classified as non-retryable", ErrConflict)
		}
		if operation.AttemptCount >= operation.MaxAttempts {
			return fmt.Errorf("%w: operation has exhausted its attempt limit", ErrConflict)
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET status = 'pending', stage = 'retry_queued', result_json = NULL,
			    error_code = NULL, current_attempt_id = NULL,
			    worker_instance_id = NULL, lease_expires_at = NULL,
			    next_attempt_at = ?, cancellation_requested_at = NULL,
			    cancellation_requested_by_identity_id = NULL,
			    completed_at = NULL, updated_at = ?
			WHERE id = ? AND status = 'failed'`,
			formatTime(now),
			formatTime(now),
			string(operation.ID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return err
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			Type:        OperationEventRetryScheduled,
			Stage:       "retry_queued",
			Progress:    operation.ProgressPercent,
			MessageCode: "operation.manual_retry_scheduled",
			Details:     map[string]any{"attemptsUsed": operation.AttemptCount},
		}, now); err != nil {
			return err
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:     params.ActorID,
			Action:      "operation.retry_scheduled",
			TargetType:  "operation",
			TargetID:    string(operation.ID),
			AccountID:   operation.AccountID,
			RequestID:   requestID,
			OperationID: &operation.ID,
			Result:      AuditSuccess,
			Details: map[string]any{
				"attemptsUsed": operation.AttemptCount,
				"kind":         operation.Kind,
				"retryClass":   operation.RetryClass,
			},
		}, now); err != nil {
			return err
		}
		operation.Status = OperationPending
		operation.Stage = "retry_queued"
		operation.Result = nil
		operation.ErrorCode = ""
		operation.NextAttemptAt = timePointer(now)
		operation.CancellationRequestedAt = nil
		operation.CancellationRequestedBy = nil
		operation.CompletedAt = nil
		operation.UpdatedAt = now
		return nil
	})
	if err != nil {
		return Operation{}, classifyOperationTransitionError(err)
	}
	return operation, nil
}

// RecoverExpiredOperations fences attempts whose worker stopped heartbeating.
// Safe retries are rescheduled; other classes fail for explicit review.
func (r *Repository) RecoverExpiredOperations(ctx context.Context) (int, error) {
	now := r.timestamp()
	var recovered int
	err := r.state.Write(ctx, func(executor store.Executor) error {
		var err error
		recovered, err = r.recoverExpiredOperationsTx(ctx, executor, now)
		return err
	})
	if err != nil {
		return 0, classifyOperationTransitionError(err)
	}
	return recovered, nil
}

func (r *Repository) recoverExpiredOperationsTx(
	ctx context.Context,
	executor store.Executor,
	now time.Time,
) (int, error) {
	rows, err := executor.QueryContext(ctx, `
		SELECT id
		FROM operations
		WHERE status IN ('running', 'cancelling')
		  AND julianday(lease_expires_at) <= julianday(?)
		ORDER BY julianday(lease_expires_at), id`, formatTime(now))
	if err != nil {
		return 0, err
	}
	var operationIDs []ID
	for rows.Next() {
		var operationID ID
		if err := rows.Scan(&operationID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		operationIDs = append(operationIDs, operationID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	for _, operationID := range operationIDs {
		operation, err := loadOperationByID(ctx, executor, operationID)
		if err != nil {
			return 0, err
		}
		if operation.CurrentAttemptID == nil {
			return 0, fmt.Errorf("operation %s has an active status without an attempt", operation.ID)
		}
		attemptID := *operation.CurrentAttemptID
		if operation.Status == OperationCancelling {
			if _, err := executor.ExecContext(ctx, `
				UPDATE operation_attempts
				SET outcome = 'cancelled', completed_at = ?
				WHERE id = ? AND operation_id = ? AND outcome = 'running'`,
				formatTime(now), string(attemptID), string(operation.ID)); err != nil {
				return 0, err
			}
			if _, err := executor.ExecContext(ctx, `
				UPDATE operations
				SET status = 'cancelled', stage = 'cancelled',
				    current_attempt_id = NULL, worker_instance_id = NULL,
				    lease_expires_at = NULL, next_attempt_at = NULL,
				    completed_at = ?, updated_at = ?
				WHERE id = ? AND status = 'cancelling'`,
				formatTime(now), formatTime(now), string(operation.ID)); err != nil {
				return 0, err
			}
			if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
				OperationID: operation.ID,
				AttemptID:   &attemptID,
				Type:        OperationEventCancelled,
				Stage:       "cancelled",
				Progress:    operation.ProgressPercent,
				MessageCode: "operation.cancelled_after_worker_loss",
				Details:     map[string]any{},
			}, now); err != nil {
				return 0, err
			}
			if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
				Action:      "operation.cancelled",
				TargetType:  "operation",
				TargetID:    string(operation.ID),
				AccountID:   operation.AccountID,
				RequestID:   operation.RequestID,
				OperationID: &operation.ID,
				Result:      AuditSuccess,
				Details:     map[string]any{"reason": "worker_lease_expired"},
			}, now); err != nil {
				return 0, err
			}
			continue
		}

		const leaseErrorCode = "operation.worker_lease_expired"
		if _, err := executor.ExecContext(ctx, `
			UPDATE operation_attempts
			SET outcome = 'lease_expired', error_code = ?, completed_at = ?
			WHERE id = ? AND operation_id = ? AND outcome = 'running'`,
			leaseErrorCode, formatTime(now), string(attemptID), string(operation.ID)); err != nil {
			return 0, err
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   &attemptID,
			Type:        OperationEventLeaseExpired,
			Stage:       operation.Stage,
			Progress:    operation.ProgressPercent,
			MessageCode: "operation.worker_lease_expired",
			Details:     map[string]any{"attempt": operation.AttemptCount},
		}, now); err != nil {
			return 0, err
		}

		if operation.RetryClass == RetrySafe && operation.AttemptCount < operation.MaxAttempts {
			nextAttemptAt := now.Add(retryDelay(operation.AttemptCount))
			if _, err := executor.ExecContext(ctx, `
				UPDATE operations
				SET status = 'pending', stage = 'retry_queued',
				    current_attempt_id = NULL, worker_instance_id = NULL,
				    lease_expires_at = NULL, next_attempt_at = ?, updated_at = ?
				WHERE id = ? AND status = 'running'`,
				formatTime(nextAttemptAt), formatTime(now), string(operation.ID)); err != nil {
				return 0, err
			}
			if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
				OperationID: operation.ID,
				AttemptID:   &attemptID,
				Type:        OperationEventRetryScheduled,
				Stage:       "retry_queued",
				Progress:    operation.ProgressPercent,
				MessageCode: "operation.retry_scheduled_after_worker_loss",
				Details:     map[string]any{"nextAttempt": formatTime(nextAttemptAt)},
			}, now); err != nil {
				return 0, err
			}
			continue
		}

		if _, err := executor.ExecContext(ctx, `
			UPDATE operations
			SET status = 'failed', stage = 'failed', error_code = ?,
			    current_attempt_id = NULL, worker_instance_id = NULL,
			    lease_expires_at = NULL, next_attempt_at = NULL,
			    completed_at = ?, updated_at = ?
			WHERE id = ? AND status = 'running'`,
			leaseErrorCode, formatTime(now), formatTime(now), string(operation.ID)); err != nil {
			return 0, err
		}
		if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
			OperationID: operation.ID,
			AttemptID:   &attemptID,
			Type:        OperationEventFailed,
			Stage:       "failed",
			Progress:    operation.ProgressPercent,
			MessageCode: "operation.failed_after_worker_loss",
			Details: map[string]any{
				"retryClass": operation.RetryClass,
			},
		}, now); err != nil {
			return 0, err
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			Action:      "operation.failed",
			TargetType:  "operation",
			TargetID:    string(operation.ID),
			AccountID:   operation.AccountID,
			RequestID:   operation.RequestID,
			OperationID: &operation.ID,
			Result:      AuditFailure,
			Details: map[string]any{
				"errorCode":  leaseErrorCode,
				"retryClass": operation.RetryClass,
			},
		}, now); err != nil {
			return 0, err
		}
	}
	return len(operationIDs), nil
}

func validateAttemptIdentity(operationID, attemptID, workerInstanceID ID) error {
	if err := validateID(operationID, "operationId"); err != nil {
		return err
	}
	if err := validateID(attemptID, "attemptId"); err != nil {
		return err
	}
	return validateID(workerInstanceID, "workerInstanceId")
}

func validateActiveAttempt(operation Operation, attemptID, workerInstanceID ID, now time.Time) error {
	if operation.Status != OperationRunning && operation.Status != OperationCancelling {
		return ErrOperationLeaseLost
	}
	if operation.CurrentAttemptID == nil || *operation.CurrentAttemptID != attemptID ||
		operation.WorkerInstanceID == nil || *operation.WorkerInstanceID != workerInstanceID ||
		operation.LeaseExpiresAt == nil || !operation.LeaseExpiresAt.After(now) {
		return ErrOperationLeaseLost
	}
	return nil
}

func classifyOperationTransitionError(err error) error {
	if err == nil || errors.Is(err, ErrOperationLeaseLost) || errors.Is(err, ErrOperationCancellationRequested) {
		return err
	}
	return classifyDatabaseError(err)
}
