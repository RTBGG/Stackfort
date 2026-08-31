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
	"unicode"

	"github.com/RTBGG/stackfort/internal/scheduledjobs"
	"github.com/RTBGG/stackfort/internal/store"
)

const scheduledJobOperationKind = "scheduled_job.lifecycle.apply"

func (r *Repository) PrepareScheduledJobCreate(
	ctx context.Context, params PrepareScheduledJobCreateParams,
) (ScheduledJobMutation, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateID(params.ActorID, "actorId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	name, err := validateScheduledJobName(params.Name)
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	accountID, actorID := params.AccountID, params.ActorID
	operation, payloadJSON, err := r.prepareOperation(CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: scheduledJobOperationKind,
		RetryClass: RetrySafe, RequestID: params.RequestID, IdempotencyKey: params.IdempotencyKey,
		Payload: map[string]any{
			"action": "create", "name": name, "runtime": params.Runtime,
			"scriptPath": params.ScriptPath, "phpVersion": params.PHPVersion,
			"schedule": params.Schedule, "enabled": params.Enabled,
		}, MaxAttempts: 3,
	})
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	definition := scheduledjobs.Definition{
		ID: string(operation.ID), Runtime: params.Runtime, ScriptPath: params.ScriptPath,
		PHPVersion: params.PHPVersion, Schedule: params.Schedule, Enabled: params.Enabled,
	}
	if scheduledjobs.ValidateDefinition(definition) != nil {
		return ScheduledJobMutation{}, fmt.Errorf("%w: scheduled job definition is invalid", ErrInvalidInput)
	}
	now := operation.CreatedAt
	mutation := ScheduledJobMutation{}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		created, replayed, createErr := r.createOperationTx(ctx, executor, operation, payloadJSON, now)
		if createErr != nil {
			return createErr
		}
		if replayed {
			var loadErr error
			mutation, loadErr = loadScheduledJobMutationTx(ctx, executor, params.AccountID, created.ID)
			return loadErr
		}
		limits, limitsErr := currentPackageLimitsTx(ctx, executor, params.AccountID)
		if limitsErr != nil {
			return limitsErr
		}
		if err := validateScheduledJobPackage(limits, definition); err != nil {
			return err
		}
		var count int64
		if err := executor.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM scheduled_jobs WHERE account_id = ? AND removed_at IS NULL`,
			string(params.AccountID)).Scan(&count); err != nil {
			return err
		}
		if limits.MaxScheduledJobs == 0 || count >= limits.MaxScheduledJobs || count >= scheduledjobs.MaximumJobsPerAccount {
			return fmt.Errorf("%w: package scheduled job limit reached", ErrConflict)
		}
		job := ScheduledJob{
			ID: operation.ID, AccountID: params.AccountID, Name: name,
			Runtime: definition.Runtime, ScriptPath: definition.ScriptPath, PHPVersion: definition.PHPVersion,
			Schedule: definition.Schedule, Enabled: definition.Enabled, Status: ScheduledJobPending,
			Revision: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertScheduledJobTx(ctx, executor, job); err != nil {
			return err
		}
		if err := insertScheduledJobMutationTx(
			ctx, executor, operation.ID, params.AccountID, job.ID, ScheduledJobMutationCreate, 1, now,
		); err != nil {
			return err
		}
		mutation = ScheduledJobMutation{
			Operation: created, Job: job, Action: ScheduledJobMutationCreate, DesiredRevision: 1,
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "scheduled_job.create_prepared",
			TargetType: "scheduled_job", TargetID: string(job.ID), AccountID: &params.AccountID,
			RequestID: created.RequestID, OperationID: &created.ID, Result: AuditSuccess,
			Details: map[string]any{"runtime": job.Runtime, "scheduleKind": job.Schedule.Kind, "enabled": job.Enabled},
		}, now)
	})
	if err != nil {
		return ScheduledJobMutation{}, classifyDatabaseError(err)
	}
	return mutation, nil
}

func (r *Repository) PrepareScheduledJobUpdate(
	ctx context.Context, params PrepareScheduledJobUpdateParams,
) (ScheduledJobMutation, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateID(params.JobID, "jobId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateID(params.ActorID, "actorId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if params.ExpectedRevision < 1 {
		return ScheduledJobMutation{}, fmt.Errorf("%w: expectedRevision must be positive", ErrInvalidInput)
	}
	name, err := validateScheduledJobName(params.Name)
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	definition := scheduledjobs.Definition{
		ID: string(params.JobID), Runtime: params.Runtime, ScriptPath: params.ScriptPath,
		PHPVersion: params.PHPVersion, Schedule: params.Schedule, Enabled: params.Enabled,
	}
	if scheduledjobs.ValidateDefinition(definition) != nil {
		return ScheduledJobMutation{}, fmt.Errorf("%w: scheduled job definition is invalid", ErrInvalidInput)
	}
	accountID, actorID := params.AccountID, params.ActorID
	operation, payloadJSON, err := r.prepareOperation(CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: scheduledJobOperationKind,
		RetryClass: RetrySafe, RequestID: params.RequestID, IdempotencyKey: params.IdempotencyKey,
		Payload: map[string]any{
			"action": "update", "jobId": params.JobID, "expectedRevision": params.ExpectedRevision,
			"name": name, "runtime": params.Runtime, "scriptPath": params.ScriptPath,
			"phpVersion": params.PHPVersion, "schedule": params.Schedule, "enabled": params.Enabled,
		}, MaxAttempts: 3,
	})
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	now := operation.CreatedAt
	mutation := ScheduledJobMutation{}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		created, replayed, createErr := r.createOperationTx(ctx, executor, operation, payloadJSON, now)
		if createErr != nil {
			return createErr
		}
		if replayed {
			var loadErr error
			mutation, loadErr = loadScheduledJobMutationTx(ctx, executor, params.AccountID, created.ID)
			return loadErr
		}
		limits, limitsErr := currentPackageLimitsTx(ctx, executor, params.AccountID)
		if limitsErr != nil {
			return limitsErr
		}
		if err := validateScheduledJobPackage(limits, definition); err != nil {
			return err
		}
		current, findErr := findScheduledJobTx(ctx, executor, params.AccountID, params.JobID, false)
		if findErr != nil {
			return findErr
		}
		if current.Revision != params.ExpectedRevision ||
			(current.Status != ScheduledJobActive && current.Status != ScheduledJobDisabled) {
			return fmt.Errorf("%w: scheduled job revision or state changed", ErrConflict)
		}
		nextRevision := current.Revision + 1
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE scheduled_jobs
			SET name = ?, runtime = ?, script_path = ?, php_version = ?,
			    schedule_kind = ?, interval_minutes = ?, hour_utc = ?, minute_utc = ?, weekday = ?,
			    enabled = ?, status = 'pending', revision = ?, updated_at = ?
			WHERE account_id = ? AND id = ? AND revision = ? AND status IN ('active', 'disabled')`,
			name, string(definition.Runtime), definition.ScriptPath, nullableString(definition.PHPVersion),
			string(definition.Schedule.Kind), definition.Schedule.IntervalMinutes,
			definition.Schedule.HourUTC, definition.Schedule.MinuteUTC, string(definition.Schedule.Weekday),
			definition.Enabled, nextRevision, formatTime(now), string(params.AccountID), string(params.JobID),
			params.ExpectedRevision)
		if updateErr != nil {
			return updateErr
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: scheduled job changed concurrently", ErrConflict)
		}
		if err := insertScheduledJobMutationTx(
			ctx, executor, created.ID, params.AccountID, params.JobID,
			ScheduledJobMutationUpdate, nextRevision, now,
		); err != nil {
			return err
		}
		job, loadErr := findScheduledJobTx(ctx, executor, params.AccountID, params.JobID, false)
		if loadErr != nil {
			return loadErr
		}
		mutation = ScheduledJobMutation{
			Operation: created, Job: job, Action: ScheduledJobMutationUpdate, DesiredRevision: nextRevision,
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "scheduled_job.update_prepared",
			TargetType: "scheduled_job", TargetID: string(job.ID), AccountID: &params.AccountID,
			RequestID: created.RequestID, OperationID: &created.ID, Result: AuditSuccess,
			Details: map[string]any{"revision": nextRevision, "runtime": job.Runtime, "scheduleKind": job.Schedule.Kind, "enabled": job.Enabled},
		}, now)
	})
	if err != nil {
		return ScheduledJobMutation{}, classifyDatabaseError(err)
	}
	return mutation, nil
}

func (r *Repository) PrepareScheduledJobDelete(
	ctx context.Context, params PrepareScheduledJobDeleteParams,
) (ScheduledJobMutation, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateID(params.JobID, "jobId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateID(params.ActorID, "actorId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if params.ExpectedRevision < 1 {
		return ScheduledJobMutation{}, fmt.Errorf("%w: expectedRevision must be positive", ErrInvalidInput)
	}
	accountID, actorID := params.AccountID, params.ActorID
	operation, payloadJSON, err := r.prepareOperation(CreateOperationParams{
		AccountID: &accountID, ActorID: &actorID, Kind: scheduledJobOperationKind,
		RetryClass: RetrySafe, RequestID: params.RequestID, IdempotencyKey: params.IdempotencyKey,
		Payload: map[string]any{
			"action": "delete", "jobId": params.JobID, "expectedRevision": params.ExpectedRevision,
		}, MaxAttempts: 3,
	})
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	now := operation.CreatedAt
	mutation := ScheduledJobMutation{}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		created, replayed, createErr := r.createOperationTx(ctx, executor, operation, payloadJSON, now)
		if createErr != nil {
			return createErr
		}
		if replayed {
			var loadErr error
			mutation, loadErr = loadScheduledJobMutationTx(ctx, executor, params.AccountID, created.ID)
			return loadErr
		}
		current, findErr := findScheduledJobTx(ctx, executor, params.AccountID, params.JobID, false)
		if findErr != nil {
			return findErr
		}
		if current.Revision != params.ExpectedRevision ||
			(current.Status != ScheduledJobActive && current.Status != ScheduledJobDisabled) {
			return fmt.Errorf("%w: scheduled job revision or state changed", ErrConflict)
		}
		nextRevision := current.Revision + 1
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE scheduled_jobs SET status = 'deleting', revision = ?, updated_at = ?
			WHERE account_id = ? AND id = ? AND revision = ? AND status IN ('active', 'disabled')`,
			nextRevision, formatTime(now), string(params.AccountID), string(params.JobID), params.ExpectedRevision)
		if updateErr != nil {
			return updateErr
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: scheduled job changed concurrently", ErrConflict)
		}
		if err := insertScheduledJobMutationTx(
			ctx, executor, created.ID, params.AccountID, params.JobID,
			ScheduledJobMutationDelete, nextRevision, now,
		); err != nil {
			return err
		}
		job, loadErr := findScheduledJobTx(ctx, executor, params.AccountID, params.JobID, false)
		if loadErr != nil {
			return loadErr
		}
		mutation = ScheduledJobMutation{
			Operation: created, Job: job, Action: ScheduledJobMutationDelete, DesiredRevision: nextRevision,
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "scheduled_job.delete_prepared",
			TargetType: "scheduled_job", TargetID: string(job.ID), AccountID: &params.AccountID,
			RequestID: created.RequestID, OperationID: &created.ID, Result: AuditSuccess,
			Details: map[string]any{"revision": nextRevision},
		}, now)
	})
	if err != nil {
		return ScheduledJobMutation{}, classifyDatabaseError(err)
	}
	return mutation, nil
}

func (r *Repository) LoadScheduledJobMutation(
	ctx context.Context, accountID, operationID ID,
) (ScheduledJobMutation, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateID(operationID, "operationId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	var mutation ScheduledJobMutation
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		mutation, err = loadScheduledJobMutationTx(ctx, reader, accountID, operationID)
		return err
	})
	if err != nil {
		return ScheduledJobMutation{}, classifyDatabaseError(err)
	}
	return mutation, nil
}

func (r *Repository) CompleteScheduledJobMutation(
	ctx context.Context, params CompleteScheduledJobMutationParams,
) (ScheduledJobMutation, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateID(params.OperationID, "operationId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return ScheduledJobMutation{}, err
	}
	requestID, err := validateText(params.RequestID, "requestId", 1, 128)
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	now := r.timestamp()
	var mutation ScheduledJobMutation
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var loadErr error
		mutation, loadErr = loadScheduledJobMutationTx(ctx, executor, params.AccountID, params.OperationID)
		if loadErr != nil {
			return loadErr
		}
		if mutation.AppliedAt != nil {
			return nil
		}
		if mutation.Job.Revision != mutation.DesiredRevision {
			return fmt.Errorf("%w: scheduled job desired revision is stale", ErrConflict)
		}
		status := ScheduledJobDisabled
		removedAt := any(nil)
		if mutation.Action == ScheduledJobMutationDelete {
			if mutation.Job.Status != ScheduledJobDeleting {
				return fmt.Errorf("%w: scheduled job is not deleting", ErrConflict)
			}
			status, removedAt = ScheduledJobDeleted, formatTime(now)
		} else {
			if mutation.Job.Status != ScheduledJobPending {
				return fmt.Errorf("%w: scheduled job is not pending", ErrConflict)
			}
			if mutation.Job.Enabled {
				status = ScheduledJobActive
			}
		}
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE scheduled_jobs
			SET status = ?, applied_revision = ?, updated_at = ?, removed_at = ?
			WHERE account_id = ? AND id = ? AND revision = ?`,
			string(status), mutation.DesiredRevision, formatTime(now), removedAt,
			string(params.AccountID), string(mutation.Job.ID), mutation.DesiredRevision)
		if updateErr != nil {
			return updateErr
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: scheduled job completion lost its fence", ErrConflict)
		}
		result, updateErr = executor.ExecContext(ctx, `
			UPDATE scheduled_job_mutations SET applied_at = ?
			WHERE account_id = ? AND operation_id = ? AND applied_at IS NULL`,
			formatTime(now), string(params.AccountID), string(params.OperationID))
		if updateErr != nil {
			return updateErr
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: scheduled job mutation was already completed", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "scheduled_job.applied", TargetType: "scheduled_job",
			TargetID: string(mutation.Job.ID), AccountID: &params.AccountID,
			RequestID: requestID, OperationID: &params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"action": mutation.Action, "revision": mutation.DesiredRevision, "status": status},
		}, now)
	})
	if err != nil {
		return ScheduledJobMutation{}, classifyDatabaseError(err)
	}
	return r.LoadScheduledJobMutation(ctx, params.AccountID, params.OperationID)
}

func (r *Repository) ListScheduledJobs(ctx context.Context, accountID ID) ([]ScheduledJob, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return nil, err
	}
	jobs := []ScheduledJob{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, scheduledJobSelect+`
			WHERE account_id = ? AND removed_at IS NULL ORDER BY created_at, id`, string(accountID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			job, scanErr := scanScheduledJob(rows)
			if scanErr != nil {
				return scanErr
			}
			jobs = append(jobs, job)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return jobs, nil
}

func (r *Repository) GetScheduledJob(ctx context.Context, accountID, jobID ID) (ScheduledJob, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return ScheduledJob{}, err
	}
	if err := validateID(jobID, "jobId"); err != nil {
		return ScheduledJob{}, err
	}
	var job ScheduledJob
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		job, err = findScheduledJobTx(ctx, reader, accountID, jobID, false)
		return err
	})
	if err != nil {
		return ScheduledJob{}, classifyDatabaseError(err)
	}
	return job, nil
}

const scheduledJobSelect = `SELECT id, account_id, name, runtime, script_path, php_version,
	schedule_kind, interval_minutes, hour_utc, minute_utc, weekday,
	enabled, status, revision, applied_revision, created_at, updated_at, removed_at
	FROM scheduled_jobs `

func loadScheduledJobMutationTx(
	ctx context.Context, reader store.Reader, accountID, operationID ID,
) (ScheduledJobMutation, error) {
	var jobID ID
	var action string
	var desiredRevision int64
	var appliedAt sql.NullString
	if err := reader.QueryRowContext(ctx, `
		SELECT job_id, action, desired_revision, applied_at
		FROM scheduled_job_mutations WHERE account_id = ? AND operation_id = ?`,
		string(accountID), string(operationID)).Scan(&jobID, &action, &desiredRevision, &appliedAt); err != nil {
		return ScheduledJobMutation{}, err
	}
	operation, err := loadScopedOperation(ctx, reader, OperationScope{AccountID: &accountID, OperationID: operationID})
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	job, err := findScheduledJobTx(ctx, reader, accountID, jobID, true)
	if err != nil {
		return ScheduledJobMutation{}, err
	}
	mutation := ScheduledJobMutation{
		Operation: operation, Job: job, Action: ScheduledJobMutationAction(action), DesiredRevision: desiredRevision,
	}
	if mutation.Action != ScheduledJobMutationCreate && mutation.Action != ScheduledJobMutationUpdate &&
		mutation.Action != ScheduledJobMutationDelete {
		return ScheduledJobMutation{}, errors.New("stored scheduled job mutation action is invalid")
	}
	if mutation.AppliedAt, err = parseOptionalTime(appliedAt); err != nil {
		return ScheduledJobMutation{}, err
	}
	return mutation, nil
}

func findScheduledJobTx(
	ctx context.Context, reader store.Reader, accountID, jobID ID, includeRemoved bool,
) (ScheduledJob, error) {
	query := scheduledJobSelect + ` WHERE account_id = ? AND id = ?`
	if !includeRemoved {
		query += " AND removed_at IS NULL"
	}
	return scanScheduledJob(reader.QueryRowContext(ctx, query, string(accountID), string(jobID)))
}

func scanScheduledJob(scanner rowScanner) (ScheduledJob, error) {
	var job ScheduledJob
	var runtime, scheduleKind, weekday, status string
	var phpVersion sql.NullString
	var enabled bool
	var appliedRevision sql.NullInt64
	var createdAt, updatedAt string
	var removedAt sql.NullString
	if err := scanner.Scan(
		&job.ID, &job.AccountID, &job.Name, &runtime, &job.ScriptPath, &phpVersion,
		&scheduleKind, &job.Schedule.IntervalMinutes, &job.Schedule.HourUTC, &job.Schedule.MinuteUTC, &weekday,
		&enabled, &status, &job.Revision, &appliedRevision, &createdAt, &updatedAt, &removedAt,
	); err != nil {
		return ScheduledJob{}, err
	}
	job.Runtime, job.PHPVersion, job.Schedule.Kind = scheduledjobs.Runtime(runtime), phpVersion.String, scheduledjobs.ScheduleKind(scheduleKind)
	job.Schedule.Weekday, job.Enabled, job.Status = scheduledjobs.Weekday(weekday), enabled, ScheduledJobStatus(status)
	definition := scheduledjobs.Definition{
		ID: string(job.ID), Runtime: job.Runtime, ScriptPath: job.ScriptPath, PHPVersion: job.PHPVersion,
		Schedule: job.Schedule, Enabled: job.Enabled,
	}
	if scheduledjobs.ValidateDefinition(definition) != nil || job.Revision < 1 ||
		(job.Status != ScheduledJobPending && job.Status != ScheduledJobActive && job.Status != ScheduledJobDisabled &&
			job.Status != ScheduledJobDeleting && job.Status != ScheduledJobError && job.Status != ScheduledJobDeleted) {
		return ScheduledJob{}, errors.New("stored scheduled job is invalid")
	}
	if appliedRevision.Valid {
		job.AppliedRevision = &appliedRevision.Int64
	}
	var err error
	if job.CreatedAt, err = parseTime(createdAt); err != nil {
		return ScheduledJob{}, err
	}
	if job.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ScheduledJob{}, err
	}
	if job.RemovedAt, err = parseOptionalTime(removedAt); err != nil {
		return ScheduledJob{}, err
	}
	return job, nil
}

func insertScheduledJobTx(ctx context.Context, executor store.Executor, job ScheduledJob) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO scheduled_jobs (
			id, account_id, name, runtime, script_path, php_version,
			schedule_kind, interval_minutes, hour_utc, minute_utc, weekday,
			enabled, status, revision, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', 1, ?, ?)`,
		string(job.ID), string(job.AccountID), job.Name, string(job.Runtime), job.ScriptPath,
		nullableString(job.PHPVersion), string(job.Schedule.Kind), job.Schedule.IntervalMinutes,
		job.Schedule.HourUTC, job.Schedule.MinuteUTC, string(job.Schedule.Weekday), job.Enabled,
		formatTime(job.CreatedAt), formatTime(job.UpdatedAt))
	return err
}

func insertScheduledJobMutationTx(
	ctx context.Context, executor store.Executor, operationID, accountID, jobID ID,
	action ScheduledJobMutationAction, desiredRevision int64, now time.Time,
) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO scheduled_job_mutations (
			operation_id, account_id, job_id, action, desired_revision, created_at
		) VALUES (?, ?, ?, ?, ?, ?)`, string(operationID), string(accountID), string(jobID),
		string(action), desiredRevision, formatTime(now))
	return err
}

func validateScheduledJobPackage(limits PackageLimits, definition scheduledjobs.Definition) error {
	if definition.Runtime == scheduledjobs.RuntimePHP && !slices.Contains(limits.AllowedPHPVersions, definition.PHPVersion) {
		return fmt.Errorf("%w: PHP version is not allowed by the package", ErrConflict)
	}
	return nil
}

func validateScheduledJobName(value string) (string, error) {
	name, err := validateText(value, "name", 1, 80)
	if err != nil {
		return "", err
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: scheduled job name contains control characters", ErrInvalidInput)
	}
	return name, nil
}
