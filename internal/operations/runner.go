// SPDX-License-Identifier: AGPL-3.0-or-later

// Package operations runs typed handlers against the durable core operation
// state machine. It never persists or returns raw handler error text.
package operations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

const (
	defaultLeaseDuration     = 30 * time.Second
	defaultHeartbeatInterval = 10 * time.Second
)

type Repository interface {
	ClaimNextOperation(context.Context, core.ClaimOperationParams) (core.ClaimedOperation, error)
	HeartbeatOperation(context.Context, core.HeartbeatOperationParams) (core.Operation, error)
	CheckpointOperation(context.Context, core.CheckpointOperationParams) (core.Operation, error)
	CompleteOperation(context.Context, core.CompleteOperationParams) (core.Operation, error)
	FailOperation(context.Context, core.FailOperationParams) (core.Operation, error)
	AcknowledgeOperationCancellation(context.Context, core.AcknowledgeOperationCancellationParams) (core.Operation, error)
}

type ProgressReporter interface {
	Checkpoint(
		ctx context.Context,
		stage string,
		progressPercent int64,
		messageCode string,
		details map[string]any,
	) error
}

type Handler interface {
	Run(context.Context, core.ClaimedOperation, ProgressReporter) (map[string]any, error)
}

type HandlerFunc func(context.Context, core.ClaimedOperation, ProgressReporter) (map[string]any, error)

func (function HandlerFunc) Run(
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (map[string]any, error) {
	return function(ctx, claimed, reporter)
}

// Failure is a handler's stable, non-sensitive failure classification. Raw
// error messages must stay in bounded internal diagnostics, not persisted job
// state or user-visible progress.
type Failure struct {
	Code      string
	Retryable bool
}

func (failure *Failure) Error() string {
	return failure.Code
}

// RunError reports only a stable code and operation ID.
type RunError struct {
	OperationID core.ID
	Code        string
}

func (runError *RunError) Error() string {
	return fmt.Sprintf("operation %s failed with code %s", runError.OperationID, runError.Code)
}

type RunnerOptions struct {
	WorkerInstanceID  core.ID
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
}

type Runner struct {
	repository        Repository
	workerInstanceID  core.ID
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	handlers          map[string]Handler
	kinds             []string
}

func NewRunner(repository Repository, handlers map[string]Handler, options RunnerOptions) (*Runner, error) {
	if repository == nil {
		return nil, errors.New("operation runner requires a repository")
	}
	if len(handlers) == 0 {
		return nil, errors.New("operation runner requires at least one handler")
	}
	workerInstanceID := options.WorkerInstanceID
	if workerInstanceID == "" {
		var err error
		workerInstanceID, err = core.NewID()
		if err != nil {
			return nil, err
		}
	} else if _, err := core.ParseID(string(workerInstanceID)); err != nil {
		return nil, fmt.Errorf("worker instance ID: %w", err)
	}
	leaseDuration := options.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = defaultLeaseDuration
	}
	heartbeatInterval := options.HeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = defaultHeartbeatInterval
	}
	if leaseDuration < 5*time.Second || leaseDuration > 5*time.Minute {
		return nil, errors.New("operation lease must be between 5 seconds and 5 minutes")
	}
	if heartbeatInterval <= 0 || heartbeatInterval >= leaseDuration/2 {
		return nil, errors.New("heartbeat interval must be positive and less than half the lease duration")
	}

	handlerCopy := maps.Clone(handlers)
	kinds := slices.Sorted(maps.Keys(handlerCopy))
	for _, kind := range kinds {
		if kind == "" || handlerCopy[kind] == nil {
			return nil, errors.New("operation handler kind and implementation must be non-empty")
		}
	}
	return &Runner{
		repository:        repository,
		workerInstanceID:  workerInstanceID,
		leaseDuration:     leaseDuration,
		heartbeatInterval: heartbeatInterval,
		handlers:          handlerCopy,
		kinds:             kinds,
	}, nil
}

// RunOnce claims and handles at most one operation. A parent-context shutdown
// deliberately leaves the attempt leased; restart recovery decides whether it
// is safe to replay after the lease expires.
func (runner *Runner) RunOnce(ctx context.Context) error {
	claimed, err := runner.repository.ClaimNextOperation(ctx, core.ClaimOperationParams{
		WorkerInstanceID: runner.workerInstanceID,
		Kinds:            runner.kinds,
		LeaseDuration:    runner.leaseDuration,
	})
	if err != nil {
		return err
	}
	handler := runner.handlers[claimed.Operation.Kind]
	handlerContext, cancelHandler := context.WithCancelCause(ctx)
	defer cancelHandler(nil)
	heartbeatStop := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go runner.heartbeat(handlerContext, cancelHandler, claimed, heartbeatStop, heartbeatDone)

	reporter := &repositoryReporter{repository: runner.repository, claimed: claimed}
	result, handlerErr := callHandler(handler, handlerContext, claimed, reporter)
	close(heartbeatStop)
	heartbeatErr := <-heartbeatDone

	if errors.Is(heartbeatErr, core.ErrOperationLeaseLost) {
		return core.ErrOperationLeaseLost
	}
	userCancellation := errors.Is(heartbeatErr, core.ErrOperationCancellationRequested) ||
		errors.Is(handlerErr, core.ErrOperationCancellationRequested) ||
		errors.Is(context.Cause(handlerContext), core.ErrOperationCancellationRequested)
	if ctx.Err() != nil && !userCancellation {
		return ctx.Err()
	}
	if userCancellation {
		if handlerErr != nil &&
			!errors.Is(handlerErr, core.ErrOperationCancellationRequested) &&
			!errors.Is(handlerErr, context.Canceled) {
			return runner.failClaim(ctx, claimed, &Failure{Code: "operation.cancellation_cleanup_failed"})
		}
		_, err := runner.repository.AcknowledgeOperationCancellation(ctx, core.AcknowledgeOperationCancellationParams{
			OperationID:      claimed.Operation.ID,
			AttemptID:        claimed.Attempt.ID,
			WorkerInstanceID: runner.workerInstanceID,
		})
		return err
	}
	if heartbeatErr != nil {
		return heartbeatErr
	}
	if handlerErr != nil {
		return runner.failClaim(ctx, claimed, handlerErr)
	}
	_, err = runner.repository.CompleteOperation(ctx, core.CompleteOperationParams{
		OperationID:      claimed.Operation.ID,
		AttemptID:        claimed.Attempt.ID,
		WorkerInstanceID: runner.workerInstanceID,
		Result:           result,
	})
	if errors.Is(err, core.ErrOperationCancellationRequested) {
		_, err = runner.repository.AcknowledgeOperationCancellation(ctx, core.AcknowledgeOperationCancellationParams{
			OperationID:      claimed.Operation.ID,
			AttemptID:        claimed.Attempt.ID,
			WorkerInstanceID: runner.workerInstanceID,
		})
	}
	return err
}

func (runner *Runner) heartbeat(
	ctx context.Context,
	cancelHandler context.CancelCauseFunc,
	claimed core.ClaimedOperation,
	stop <-chan struct{},
	done chan<- error,
) {
	ticker := time.NewTicker(runner.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			done <- nil
			return
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			_, err := runner.repository.HeartbeatOperation(ctx, core.HeartbeatOperationParams{
				OperationID:      claimed.Operation.ID,
				AttemptID:        claimed.Attempt.ID,
				WorkerInstanceID: runner.workerInstanceID,
				LeaseDuration:    runner.leaseDuration,
			})
			if err != nil {
				cancelHandler(err)
				done <- err
				return
			}
		}
	}
}

func (runner *Runner) failClaim(ctx context.Context, claimed core.ClaimedOperation, handlerErr error) error {
	failure := &Failure{Code: "operation.handler_failed"}
	if !errors.As(handlerErr, &failure) {
		failure = &Failure{Code: "operation.handler_failed"}
	}
	_, err := runner.repository.FailOperation(ctx, core.FailOperationParams{
		OperationID:      claimed.Operation.ID,
		AttemptID:        claimed.Attempt.ID,
		WorkerInstanceID: runner.workerInstanceID,
		ErrorCode:        failure.Code,
		Retry:            failure.Retryable,
	})
	if err != nil {
		return err
	}
	return &RunError{OperationID: claimed.Operation.ID, Code: failure.Code}
}

func callHandler(
	handler Handler,
	ctx context.Context,
	claimed core.ClaimedOperation,
	reporter ProgressReporter,
) (result map[string]any, returnErr error) {
	defer func() {
		if recover() != nil {
			result = nil
			returnErr = &Failure{Code: "operation.handler_panic"}
		}
	}()
	return handler.Run(ctx, claimed, reporter)
}

type repositoryReporter struct {
	repository Repository
	claimed    core.ClaimedOperation
}

func (reporter *repositoryReporter) Checkpoint(
	ctx context.Context,
	stage string,
	progressPercent int64,
	messageCode string,
	details map[string]any,
) error {
	_, err := reporter.repository.CheckpointOperation(ctx, core.CheckpointOperationParams{
		OperationID:      reporter.claimed.Operation.ID,
		AttemptID:        reporter.claimed.Attempt.ID,
		WorkerInstanceID: reporter.claimed.Attempt.WorkerInstanceID,
		Stage:            stage,
		ProgressPercent:  progressPercent,
		MessageCode:      messageCode,
		Details:          details,
	})
	return err
}
