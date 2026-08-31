// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/store"
)

var filesystemReasonPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func (r *Repository) GetHostingFilesystemState(ctx context.Context, accountID ID) (HostingFilesystemState, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return HostingFilesystemState{}, err
	}
	var result HostingFilesystemState
	var projectID uint32
	var desiredBytes, desiredInodes, appliedBytes, appliedInodes sql.NullInt64
	var status, capability, reason, updatedAt sql.NullString
	var appliedAt, operationID sql.NullString
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT account_id, project_id, desired_storage_bytes, desired_storage_inodes,
			       applied_storage_bytes, applied_storage_inodes, revision, status,
			       capability_status, reason_code, updated_at, applied_at, last_operation_id
			FROM hosting_account_filesystems WHERE account_id = ?`, string(accountID)).Scan(
			&result.AccountID, &projectID, &desiredBytes, &desiredInodes,
			&appliedBytes, &appliedInodes, &result.Revision, &status,
			&capability, &reason, &updatedAt, &appliedAt, &operationID,
		)
	})
	if err != nil {
		return HostingFilesystemState{}, classifyDatabaseError(err)
	}
	if projectID < hostingidentity.MinimumID || projectID > hostingidentity.MaximumID ||
		!status.Valid || !capability.Valid || !updatedAt.Valid {
		return HostingFilesystemState{}, fmt.Errorf("stored hosting filesystem state is invalid")
	}
	result.ProjectID = projectID
	result.DesiredStorageBytes = optionalInt64(desiredBytes)
	result.DesiredStorageInodes = optionalInt64(desiredInodes)
	result.AppliedStorageBytes = optionalInt64(appliedBytes)
	result.AppliedStorageInodes = optionalInt64(appliedInodes)
	result.Status = HostingFilesystemStatus(status.String)
	result.CapabilityStatus = HostingFilesystemCapabilityStatus(capability.String)
	result.ReasonCode = reason.String
	result.UpdatedAt, err = parseTime(updatedAt.String)
	if err != nil {
		return HostingFilesystemState{}, err
	}
	if appliedAt.Valid {
		value, err := parseTime(appliedAt.String)
		if err != nil {
			return HostingFilesystemState{}, err
		}
		result.AppliedAt = &value
	}
	if operationID.Valid {
		value := ID(operationID.String)
		if err := validateID(value, "stored lastOperationId"); err != nil {
			return HostingFilesystemState{}, err
		}
		result.LastOperationID = &value
	}
	return result, nil
}

func (r *Repository) ConfirmHostingFilesystemApplied(
	ctx context.Context,
	params ConfirmHostingFilesystemAppliedParams,
) (HostingFilesystemState, error) {
	if err := validateFilesystemConfirmation(params.AccountID, params.ExpectedRevision, params.OperationID, params.ActorID); err != nil {
		return HostingFilesystemState{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return HostingFilesystemState{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		result, err := executor.ExecContext(ctx, `
			UPDATE hosting_account_filesystems
			SET applied_storage_bytes = desired_storage_bytes,
			    applied_storage_inodes = desired_storage_inodes,
			    status = 'applied', capability_status = 'available', reason_code = NULL,
			    updated_at = ?, applied_at = ?, last_operation_id = ?
			WHERE account_id = ? AND revision = ?
			  AND EXISTS (
			      SELECT 1 FROM operations
			      WHERE id = ? AND account_id = hosting_account_filesystems.account_id
			  )`,
			formatTime(now), formatTime(now), string(params.OperationID),
			string(params.AccountID), params.ExpectedRevision, string(params.OperationID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: hosting filesystem revision changed", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "hosting_filesystem.applied",
			TargetType: "hosting_account", TargetID: string(params.AccountID),
			AccountID: &params.AccountID, RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"revision": params.ExpectedRevision, "operationId": params.OperationID},
		}, now)
	})
	if err != nil {
		return HostingFilesystemState{}, classifyDatabaseError(err)
	}
	return r.GetHostingFilesystemState(ctx, params.AccountID)
}

func (r *Repository) ConfirmHostingFilesystemBlocked(
	ctx context.Context,
	params ConfirmHostingFilesystemBlockedParams,
) (HostingFilesystemState, error) {
	if err := validateFilesystemConfirmation(params.AccountID, params.ExpectedRevision, params.OperationID, params.ActorID); err != nil {
		return HostingFilesystemState{}, err
	}
	if params.CapabilityStatus != HostingFilesystemCapabilityUnavailable &&
		params.CapabilityStatus != HostingFilesystemCapabilityUnsupported &&
		params.CapabilityStatus != HostingFilesystemCapabilityUnknown {
		return HostingFilesystemState{}, fmt.Errorf("%w: blocked filesystem requires a non-available capability status", ErrInvalidInput)
	}
	if !filesystemReasonPattern.MatchString(params.ReasonCode) {
		return HostingFilesystemState{}, fmt.Errorf("%w: filesystem reasonCode is malformed", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return HostingFilesystemState{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		result, err := executor.ExecContext(ctx, `
			UPDATE hosting_account_filesystems
			SET status = 'blocked', capability_status = ?, reason_code = ?,
			    updated_at = ?, last_operation_id = ?
			WHERE account_id = ? AND revision = ?
			  AND EXISTS (
			      SELECT 1 FROM operations
			      WHERE id = ? AND account_id = hosting_account_filesystems.account_id
			  )`,
			string(params.CapabilityStatus), params.ReasonCode, formatTime(now),
			string(params.OperationID), string(params.AccountID), params.ExpectedRevision,
			string(params.OperationID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: hosting filesystem revision changed", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "hosting_filesystem.blocked",
			TargetType: "hosting_account", TargetID: string(params.AccountID),
			AccountID: &params.AccountID, RequestID: requestID, Result: AuditFailure,
			Details: map[string]any{
				"revision": params.ExpectedRevision, "operationId": params.OperationID,
				"capabilityStatus": params.CapabilityStatus, "reasonCode": params.ReasonCode,
			},
		}, now)
	})
	if err != nil {
		return HostingFilesystemState{}, classifyDatabaseError(err)
	}
	return r.GetHostingFilesystemState(ctx, params.AccountID)
}

func validateFilesystemConfirmation(accountID ID, revision int64, operationID ID, actorID *ID) error {
	if err := validateID(accountID, "accountId"); err != nil {
		return err
	}
	if revision < 1 {
		return fmt.Errorf("%w: expectedRevision must be positive", ErrInvalidInput)
	}
	if err := validateID(operationID, "operationId"); err != nil {
		return err
	}
	return validateOptionalID(actorID, "actorId")
}

func optionalInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
