// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

const (
	maxOperationEventDetailsBytes = 16 << 10
	defaultOperationEventLimit    = 100
	maximumOperationEventLimit    = 200
	maximumWorkerKinds            = 32
	minimumOperationLease         = 5 * time.Second
	maximumOperationLease         = 5 * time.Minute
)

const operationSelectColumns = `
	id, account_id, actor_identity_id, kind, status, stage,
	progress_percent, retry_class, request_id, idempotency_key,
	payload_json, result_json, error_code, created_at, updated_at,
	started_at, completed_at, max_attempts, attempt_count,
	next_attempt_at, current_attempt_id, worker_instance_id,
	lease_expires_at, cancellation_requested_at,
	cancellation_requested_by_identity_id`

type rowScanner interface {
	Scan(...any) error
}

func (r *Repository) CreateOperation(ctx context.Context, params CreateOperationParams) (Operation, error) {
	operation, payloadJSON, err := r.prepareOperation(params)
	if err != nil {
		return Operation{}, err
	}
	now := operation.CreatedAt
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var createErr error
		operation, _, createErr = r.createOperationTx(ctx, executor, operation, payloadJSON, now)
		return createErr
	})
	if err != nil {
		return Operation{}, classifyDatabaseError(err)
	}
	return operation, nil
}

func (r *Repository) prepareOperation(params CreateOperationParams) (Operation, string, error) {
	if err := validateOptionalID(params.AccountID, "accountId"); err != nil {
		return Operation{}, "", err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Operation{}, "", err
	}
	kind, err := validateAction(params.Kind, "kind", 80)
	if err != nil {
		return Operation{}, "", err
	}
	if params.RetryClass != RetryNone && params.RetryClass != RetrySafe && params.RetryClass != RetryManual {
		return Operation{}, "", fmt.Errorf("%w: unsupported retry class", ErrInvalidInput)
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return Operation{}, "", err
	}
	idempotencyKey, err := validateOptionalText(params.IdempotencyKey, "idempotencyKey", 128)
	if err != nil {
		return Operation{}, "", err
	}
	payloadJSON, payload, err := encodeSafeOperationObject(params.Payload, maxOperationJSONBytes)
	if err != nil {
		return Operation{}, "", err
	}
	maxAttempts, err := normalizeMaxAttempts(params.RetryClass, params.MaxAttempts)
	if err != nil {
		return Operation{}, "", err
	}
	id, err := r.newID()
	if err != nil {
		return Operation{}, "", err
	}
	now := r.timestamp()
	operation := Operation{
		ID:              id,
		AccountID:       params.AccountID,
		ActorID:         params.ActorID,
		Kind:            kind,
		Status:          OperationPending,
		Stage:           "queued",
		ProgressPercent: 0,
		RetryClass:      params.RetryClass,
		RequestID:       requestID,
		IdempotencyKey:  idempotencyKey,
		Payload:         payload,
		MaxAttempts:     maxAttempts,
		NextAttemptAt:   timePointer(now),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return operation, payloadJSON, nil
}

func (r *Repository) createOperationTx(
	ctx context.Context,
	executor store.Executor,
	operation Operation,
	payloadJSON string,
	now time.Time,
) (Operation, bool, error) {
	if operation.IdempotencyKey != "" {
		existing, findErr := findOperationByIdempotencyKeyTx(
			ctx, executor, operation.AccountID, operation.IdempotencyKey,
		)
		switch {
		case findErr == nil:
			if existing.Kind != operation.Kind ||
				existing.RetryClass != operation.RetryClass ||
				existing.MaxAttempts != operation.MaxAttempts ||
				!optionalIDEqual(existing.ActorID, operation.ActorID) ||
				!objectsEqual(existing.Payload, operation.Payload) {
				return Operation{}, false, fmt.Errorf("%w: idempotency key was already used for different operation input", ErrConflict)
			}
			return existing, true, nil
		case !errors.Is(findErr, sql.ErrNoRows):
			return Operation{}, false, findErr
		}
	}

	_, err := executor.ExecContext(ctx, `
			INSERT INTO operations (
				id, account_id, actor_identity_id, kind, status, stage,
				progress_percent, retry_class, request_id, idempotency_key,
				payload_json, created_at, updated_at, max_attempts,
				attempt_count, next_attempt_at
			) VALUES (?, ?, ?, ?, 'pending', 'queued', 0, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		string(operation.ID), nullableID(operation.AccountID), nullableID(operation.ActorID),
		operation.Kind, string(operation.RetryClass), operation.RequestID,
		nullableString(operation.IdempotencyKey), payloadJSON, formatTime(now), formatTime(now),
		operation.MaxAttempts, formatTime(now),
	)
	if err != nil {
		return Operation{}, false, err
	}
	if _, err := r.appendOperationEventTx(ctx, executor, appendOperationEventParams{
		OperationID: operation.ID,
		Type:        OperationEventCreated,
		Stage:       operation.Stage,
		Progress:    operation.ProgressPercent,
		MessageCode: "operation.created",
		Details: map[string]any{
			"kind":        operation.Kind,
			"maxAttempts": operation.MaxAttempts,
			"retryClass":  operation.RetryClass,
		},
	}, now); err != nil {
		return Operation{}, false, err
	}
	if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
		ActorID:     operation.ActorID,
		Action:      "operation.created",
		TargetType:  "operation",
		TargetID:    string(operation.ID),
		AccountID:   operation.AccountID,
		RequestID:   operation.RequestID,
		OperationID: &operation.ID,
		Result:      AuditSuccess,
		Details: map[string]any{
			"kind":        operation.Kind,
			"maxAttempts": operation.MaxAttempts,
			"retryClass":  operation.RetryClass,
		},
	}, now); err != nil {
		return Operation{}, false, err
	}
	return operation, false, nil
}

func (r *Repository) GetOperation(ctx context.Context, scope OperationScope) (Operation, error) {
	if err := validateOperationScope(scope); err != nil {
		return Operation{}, err
	}
	var operation Operation
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		operation, err = loadScopedOperation(ctx, reader, scope)
		return err
	})
	if err != nil {
		return Operation{}, classifyDatabaseError(err)
	}
	return operation, nil
}

func (r *Repository) ListOperationEvents(ctx context.Context, params ListOperationEventsParams) ([]OperationEvent, error) {
	if err := validateOperationScope(params.Scope); err != nil {
		return nil, err
	}
	if params.AfterSequence < 0 {
		return nil, fmt.Errorf("%w: afterSequence must not be negative", ErrInvalidInput)
	}
	limit := params.Limit
	if limit == 0 {
		limit = defaultOperationEventLimit
	}
	if limit < 1 || limit > maximumOperationEventLimit {
		return nil, fmt.Errorf("%w: event limit must be between 1 and %d", ErrInvalidInput, maximumOperationEventLimit)
	}

	var events []OperationEvent
	err := r.state.Read(ctx, func(reader store.Reader) error {
		if _, err := loadScopedOperation(ctx, reader, params.Scope); err != nil {
			return err
		}
		rows, err := reader.QueryContext(ctx, `
			SELECT id, operation_id, sequence, attempt_id, event_type, stage,
			       progress_percent, message_code, details_json, occurred_at
			FROM operation_events
			WHERE operation_id = ? AND sequence > ?
			ORDER BY sequence
			LIMIT ?`, string(params.Scope.OperationID), params.AfterSequence, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			event, err := scanOperationEvent(rows)
			if err != nil {
				return err
			}
			events = append(events, event)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return events, nil
}

func (r *Repository) ListOperationAttempts(ctx context.Context, scope OperationScope) ([]OperationAttempt, error) {
	if err := validateOperationScope(scope); err != nil {
		return nil, err
	}
	var attempts []OperationAttempt
	err := r.state.Read(ctx, func(reader store.Reader) error {
		if _, err := loadScopedOperation(ctx, reader, scope); err != nil {
			return err
		}
		rows, err := reader.QueryContext(ctx, `
			SELECT id, operation_id, attempt_number, worker_instance_id,
			       claimed_at, heartbeat_at, lease_expires_at, completed_at,
			       outcome, error_code
			FROM operation_attempts
			WHERE operation_id = ?
			ORDER BY attempt_number`, string(scope.OperationID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			attempt, err := scanOperationAttempt(rows)
			if err != nil {
				return err
			}
			attempts = append(attempts, attempt)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return attempts, nil
}

func normalizeMaxAttempts(retryClass RetryClass, value int64) (int64, error) {
	if value == 0 {
		if retryClass == RetryNone {
			return 1, nil
		}
		return 3, nil
	}
	if value < 1 || value > 100 {
		return 0, fmt.Errorf("%w: maxAttempts must be between 1 and 100", ErrInvalidInput)
	}
	if retryClass == RetryNone && value != 1 {
		return 0, fmt.Errorf("%w: non-retryable operation must have exactly one attempt", ErrInvalidInput)
	}
	return value, nil
}

func validateOperationScope(scope OperationScope) error {
	if err := validateID(scope.OperationID, "operationId"); err != nil {
		return err
	}
	return validateOptionalID(scope.AccountID, "accountId")
}

func validateLeaseDuration(value time.Duration) error {
	if value < minimumOperationLease || value > maximumOperationLease {
		return fmt.Errorf("%w: lease duration must be between %s and %s", ErrInvalidInput, minimumOperationLease, maximumOperationLease)
	}
	return nil
}

func normalizeWorkerKinds(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > maximumWorkerKinds {
		return nil, fmt.Errorf("%w: worker must support between 1 and %d operation kinds", ErrInvalidInput, maximumWorkerKinds)
	}
	result := make([]string, len(values))
	for index, value := range values {
		normalized, err := validateAction(value, "kind", 80)
		if err != nil {
			return nil, err
		}
		result[index] = normalized
	}
	slices.Sort(result)
	result = slices.Compact(result)
	return result, nil
}

func loadScopedOperation(ctx context.Context, reader store.Reader, scope OperationScope) (Operation, error) {
	if scope.AccountID == nil {
		return scanOperation(reader.QueryRowContext(ctx, `
			SELECT `+operationSelectColumns+`
			FROM operations
			WHERE id = ? AND account_id IS NULL`, string(scope.OperationID)))
	}
	return scanOperation(reader.QueryRowContext(ctx, `
		SELECT `+operationSelectColumns+`
		FROM operations
		WHERE id = ? AND account_id = ?`, string(scope.OperationID), string(*scope.AccountID)))
}

func loadOperationByID(ctx context.Context, reader store.Reader, operationID ID) (Operation, error) {
	return scanOperation(reader.QueryRowContext(ctx, `
		SELECT `+operationSelectColumns+`
		FROM operations
		WHERE id = ?`, string(operationID)))
}

func findOperationByIdempotencyKeyTx(
	ctx context.Context,
	executor store.Executor,
	accountID *ID,
	idempotencyKey string,
) (Operation, error) {
	if accountID == nil {
		return scanOperation(executor.QueryRowContext(ctx, `
			SELECT `+operationSelectColumns+`
			FROM operations
			WHERE account_id IS NULL AND idempotency_key = ?`, idempotencyKey))
	}
	return scanOperation(executor.QueryRowContext(ctx, `
		SELECT `+operationSelectColumns+`
		FROM operations
		WHERE account_id = ? AND idempotency_key = ?`, string(*accountID), idempotencyKey))
}

func scanOperation(row rowScanner) (Operation, error) {
	var operation Operation
	var accountID, actorID, idempotencyKey sql.NullString
	var resultJSON, errorCode sql.NullString
	var status, retryClass, payloadJSON string
	var createdAt, updatedAt string
	var startedAt, completedAt, nextAttemptAt sql.NullString
	var currentAttemptID, workerInstanceID, leaseExpiresAt sql.NullString
	var cancellationAt, cancellationBy sql.NullString
	if err := row.Scan(
		&operation.ID,
		&accountID,
		&actorID,
		&operation.Kind,
		&status,
		&operation.Stage,
		&operation.ProgressPercent,
		&retryClass,
		&operation.RequestID,
		&idempotencyKey,
		&payloadJSON,
		&resultJSON,
		&errorCode,
		&createdAt,
		&updatedAt,
		&startedAt,
		&completedAt,
		&operation.MaxAttempts,
		&operation.AttemptCount,
		&nextAttemptAt,
		&currentAttemptID,
		&workerInstanceID,
		&leaseExpiresAt,
		&cancellationAt,
		&cancellationBy,
	); err != nil {
		return Operation{}, err
	}
	operation.Status = OperationStatus(status)
	operation.RetryClass = RetryClass(retryClass)
	operation.IdempotencyKey = idempotencyKey.String
	operation.ErrorCode = errorCode.String
	operation.AccountID = idFromNull(accountID)
	operation.ActorID = idFromNull(actorID)
	operation.CurrentAttemptID = idFromNull(currentAttemptID)
	operation.WorkerInstanceID = idFromNull(workerInstanceID)
	operation.CancellationRequestedBy = idFromNull(cancellationBy)
	var err error
	if operation.Payload, err = decodeObject(payloadJSON); err != nil {
		return Operation{}, err
	}
	if resultJSON.Valid {
		if operation.Result, err = decodeObject(resultJSON.String); err != nil {
			return Operation{}, err
		}
	}
	if operation.CreatedAt, err = parseTime(createdAt); err != nil {
		return Operation{}, err
	}
	if operation.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Operation{}, err
	}
	if operation.StartedAt, err = parseOptionalTime(startedAt); err != nil {
		return Operation{}, err
	}
	if operation.CompletedAt, err = parseOptionalTime(completedAt); err != nil {
		return Operation{}, err
	}
	if operation.NextAttemptAt, err = parseOptionalTime(nextAttemptAt); err != nil {
		return Operation{}, err
	}
	if operation.LeaseExpiresAt, err = parseOptionalTime(leaseExpiresAt); err != nil {
		return Operation{}, err
	}
	if operation.CancellationRequestedAt, err = parseOptionalTime(cancellationAt); err != nil {
		return Operation{}, err
	}
	return operation, nil
}

func scanOperationAttempt(row rowScanner) (OperationAttempt, error) {
	var attempt OperationAttempt
	var claimedAt, heartbeatAt, leaseExpiresAt string
	var completedAt, errorCode sql.NullString
	var outcome string
	if err := row.Scan(
		&attempt.ID,
		&attempt.OperationID,
		&attempt.AttemptNumber,
		&attempt.WorkerInstanceID,
		&claimedAt,
		&heartbeatAt,
		&leaseExpiresAt,
		&completedAt,
		&outcome,
		&errorCode,
	); err != nil {
		return OperationAttempt{}, err
	}
	attempt.Outcome = OperationAttemptOutcome(outcome)
	attempt.ErrorCode = errorCode.String
	var err error
	if attempt.ClaimedAt, err = parseTime(claimedAt); err != nil {
		return OperationAttempt{}, err
	}
	if attempt.HeartbeatAt, err = parseTime(heartbeatAt); err != nil {
		return OperationAttempt{}, err
	}
	if attempt.LeaseExpiresAt, err = parseTime(leaseExpiresAt); err != nil {
		return OperationAttempt{}, err
	}
	if attempt.CompletedAt, err = parseOptionalTime(completedAt); err != nil {
		return OperationAttempt{}, err
	}
	return attempt, nil
}

func scanOperationEvent(row rowScanner) (OperationEvent, error) {
	var event OperationEvent
	var attemptID sql.NullString
	var eventType, detailsJSON, occurredAt string
	if err := row.Scan(
		&event.ID,
		&event.OperationID,
		&event.Sequence,
		&attemptID,
		&eventType,
		&event.Stage,
		&event.ProgressPercent,
		&event.MessageCode,
		&detailsJSON,
		&occurredAt,
	); err != nil {
		return OperationEvent{}, err
	}
	event.AttemptID = idFromNull(attemptID)
	event.Type = OperationEventType(eventType)
	var err error
	if event.Details, err = decodeObject(detailsJSON); err != nil {
		return OperationEvent{}, err
	}
	if event.OccurredAt, err = parseTime(occurredAt); err != nil {
		return OperationEvent{}, err
	}
	return event, nil
}

type appendOperationEventParams struct {
	OperationID ID
	AttemptID   *ID
	Type        OperationEventType
	Stage       string
	Progress    int64
	MessageCode string
	Details     map[string]any
}

func (r *Repository) appendOperationEventTx(
	ctx context.Context,
	executor store.Executor,
	params appendOperationEventParams,
	now time.Time,
) (OperationEvent, error) {
	if err := validateOperationEventType(params.Type); err != nil {
		return OperationEvent{}, err
	}
	stage, err := validateAction(params.Stage, "stage", 80)
	if err != nil {
		return OperationEvent{}, err
	}
	messageCode, err := validateAction(params.MessageCode, "messageCode", 80)
	if err != nil {
		return OperationEvent{}, err
	}
	if params.Progress < 0 || params.Progress > 100 {
		return OperationEvent{}, fmt.Errorf("%w: progress percent must be between 0 and 100", ErrInvalidInput)
	}
	detailsJSON, details, err := encodeSafeOperationObject(params.Details, maxOperationEventDetailsBytes)
	if err != nil {
		return OperationEvent{}, err
	}
	id, err := r.newID()
	if err != nil {
		return OperationEvent{}, err
	}
	event := OperationEvent{
		ID:              id,
		OperationID:     params.OperationID,
		AttemptID:       params.AttemptID,
		Type:            params.Type,
		Stage:           stage,
		ProgressPercent: params.Progress,
		MessageCode:     messageCode,
		Details:         details,
		OccurredAt:      now,
	}
	if err := executor.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM operation_events
		WHERE operation_id = ?`, string(params.OperationID)).Scan(&event.Sequence); err != nil {
		return OperationEvent{}, err
	}
	_, err = executor.ExecContext(ctx, `
		INSERT INTO operation_events (
			id, operation_id, sequence, attempt_id, event_type, stage,
			progress_percent, message_code, details_json, occurred_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(event.ID),
		string(event.OperationID),
		event.Sequence,
		nullableID(event.AttemptID),
		string(event.Type),
		event.Stage,
		event.ProgressPercent,
		event.MessageCode,
		detailsJSON,
		formatTime(now),
	)
	return event, err
}

func validateOperationEventType(value OperationEventType) error {
	switch value {
	case OperationEventCreated,
		OperationEventClaimed,
		OperationEventProgress,
		OperationEventRetryScheduled,
		OperationEventCancellationRequested,
		OperationEventSucceeded,
		OperationEventFailed,
		OperationEventCancelled,
		OperationEventLeaseExpired:
		return nil
	default:
		return fmt.Errorf("%w: unsupported operation event type", ErrInvalidInput)
	}
}

func encodeSafeOperationObject(value map[string]any, maximum int) (string, map[string]any, error) {
	encoded, err := encodeObject(value, maximum)
	if err != nil {
		return "", nil, err
	}
	decoded, err := decodeObject(encoded)
	if err != nil {
		return "", nil, err
	}
	if err := rejectAuditSecrets(decoded, "operation"); err != nil {
		return "", nil, err
	}
	return encoded, decoded, nil
}

func objectsEqual(left, right map[string]any) bool {
	leftJSON, _, leftErr := encodeSafeOperationObject(left, maxOperationJSONBytes)
	rightJSON, _, rightErr := encodeSafeOperationObject(right, maxOperationJSONBytes)
	return leftErr == nil && rightErr == nil && leftJSON == rightJSON
}

func optionalIDEqual(left, right *ID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func idFromNull(value sql.NullString) *ID {
	if !value.Valid {
		return nil
	}
	id := ID(value.String)
	return &id
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}

func retryDelay(attemptNumber int64) time.Duration {
	delay := 5 * time.Second
	for count := int64(1); count < attemptNumber && delay < maximumOperationLease; count++ {
		delay *= 2
	}
	if delay > maximumOperationLease {
		return maximumOperationLease
	}
	return delay
}

func buildKindPlaceholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}
