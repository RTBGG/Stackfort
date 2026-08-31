// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

func (r *Repository) GetHostingResourceState(ctx context.Context, accountID ID) (HostingResourceState, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return HostingResourceState{}, err
	}
	var result HostingResourceState
	var desiredCPUQuota, desiredCPUWeight, desiredMemory, desiredSwap, desiredProcesses sql.NullInt64
	var appliedCPUQuota, appliedCPUWeight, appliedMemory, appliedSwap, appliedProcesses sql.NullInt64
	var status, capability, reason, updatedAt sql.NullString
	var appliedAt, operationID sql.NullString
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT account_id,
			       desired_cpu_quota_percent, desired_cpu_weight, desired_memory_bytes,
			       desired_swap_bytes, desired_process_limit,
			       applied_cpu_quota_percent, applied_cpu_weight, applied_memory_bytes,
			       applied_swap_bytes, applied_process_limit,
			       revision, status, capability_status, reason_code,
			       updated_at, applied_at, last_operation_id
			FROM hosting_account_resources WHERE account_id = ?`, string(accountID)).Scan(
			&result.AccountID,
			&desiredCPUQuota, &desiredCPUWeight, &desiredMemory, &desiredSwap, &desiredProcesses,
			&appliedCPUQuota, &appliedCPUWeight, &appliedMemory, &appliedSwap, &appliedProcesses,
			&result.Revision, &status, &capability, &reason, &updatedAt, &appliedAt, &operationID,
		)
	})
	if err != nil {
		return HostingResourceState{}, classifyDatabaseError(err)
	}
	if !status.Valid || !capability.Valid || !updatedAt.Valid {
		return HostingResourceState{}, fmt.Errorf("stored hosting resource state is invalid")
	}
	result.DesiredCPUQuotaPercent = optionalInt64(desiredCPUQuota)
	result.DesiredCPUWeight = optionalInt64(desiredCPUWeight)
	result.DesiredMemoryBytes = optionalInt64(desiredMemory)
	result.DesiredSwapBytes = optionalInt64(desiredSwap)
	result.DesiredProcessLimit = optionalInt64(desiredProcesses)
	result.AppliedCPUQuotaPercent = optionalInt64(appliedCPUQuota)
	result.AppliedCPUWeight = optionalInt64(appliedCPUWeight)
	result.AppliedMemoryBytes = optionalInt64(appliedMemory)
	result.AppliedSwapBytes = optionalInt64(appliedSwap)
	result.AppliedProcessLimit = optionalInt64(appliedProcesses)
	result.Status = HostingResourceStatus(status.String)
	result.CapabilityStatus = HostingResourceCapabilityStatus(capability.String)
	result.ReasonCode = reason.String
	result.UpdatedAt, err = parseTime(updatedAt.String)
	if err != nil {
		return HostingResourceState{}, err
	}
	if appliedAt.Valid {
		value, err := parseTime(appliedAt.String)
		if err != nil {
			return HostingResourceState{}, err
		}
		result.AppliedAt = &value
	}
	if operationID.Valid {
		value := ID(operationID.String)
		if err := validateID(value, "stored lastOperationId"); err != nil {
			return HostingResourceState{}, err
		}
		result.LastOperationID = &value
	}
	return result, nil
}

func (r *Repository) ConfirmHostingResourcesApplied(
	ctx context.Context,
	params ConfirmHostingResourcesAppliedParams,
) (HostingResourceState, error) {
	if err := validateResourceConfirmation(params.AccountID, params.ExpectedRevision, params.OperationID, params.ActorID); err != nil {
		return HostingResourceState{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return HostingResourceState{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		result, err := executor.ExecContext(ctx, `
			UPDATE hosting_account_resources
			SET applied_cpu_quota_percent = desired_cpu_quota_percent,
			    applied_cpu_weight = desired_cpu_weight,
			    applied_memory_bytes = desired_memory_bytes,
			    applied_swap_bytes = desired_swap_bytes,
			    applied_process_limit = desired_process_limit,
			    status = 'applied', capability_status = 'available', reason_code = NULL,
			    updated_at = ?, applied_at = ?, last_operation_id = ?
			WHERE account_id = ? AND revision = ?
			  AND EXISTS (
			      SELECT 1 FROM operations
			      WHERE id = ? AND account_id = hosting_account_resources.account_id
			  )`,
			formatTime(now), formatTime(now), string(params.OperationID),
			string(params.AccountID), params.ExpectedRevision, string(params.OperationID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: hosting resource revision changed", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "hosting_resources.applied",
			TargetType: "hosting_account", TargetID: string(params.AccountID),
			AccountID: &params.AccountID, RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"revision": params.ExpectedRevision, "operationId": params.OperationID},
		}, now)
	})
	if err != nil {
		return HostingResourceState{}, classifyDatabaseError(err)
	}
	return r.GetHostingResourceState(ctx, params.AccountID)
}

func (r *Repository) ConfirmHostingResourcesBlocked(
	ctx context.Context,
	params ConfirmHostingResourcesBlockedParams,
) (HostingResourceState, error) {
	if err := validateResourceConfirmation(params.AccountID, params.ExpectedRevision, params.OperationID, params.ActorID); err != nil {
		return HostingResourceState{}, err
	}
	if params.CapabilityStatus != HostingResourceCapabilityUnavailable &&
		params.CapabilityStatus != HostingResourceCapabilityUnsupported &&
		params.CapabilityStatus != HostingResourceCapabilityUnknown {
		return HostingResourceState{}, fmt.Errorf("%w: blocked resources require a non-available capability status", ErrInvalidInput)
	}
	if !filesystemReasonPattern.MatchString(params.ReasonCode) {
		return HostingResourceState{}, fmt.Errorf("%w: resource reasonCode is malformed", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return HostingResourceState{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		result, err := executor.ExecContext(ctx, `
			UPDATE hosting_account_resources
			SET status = 'blocked', capability_status = ?, reason_code = ?,
			    updated_at = ?, last_operation_id = ?
			WHERE account_id = ? AND revision = ?
			  AND EXISTS (
			      SELECT 1 FROM operations
			      WHERE id = ? AND account_id = hosting_account_resources.account_id
			  )`,
			string(params.CapabilityStatus), params.ReasonCode, formatTime(now),
			string(params.OperationID), string(params.AccountID), params.ExpectedRevision,
			string(params.OperationID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: hosting resource revision changed", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "hosting_resources.blocked",
			TargetType: "hosting_account", TargetID: string(params.AccountID),
			AccountID: &params.AccountID, RequestID: requestID, Result: AuditFailure,
			Details: map[string]any{
				"revision": params.ExpectedRevision, "operationId": params.OperationID,
				"capabilityStatus": params.CapabilityStatus, "reasonCode": params.ReasonCode,
			},
		}, now)
	})
	if err != nil {
		return HostingResourceState{}, classifyDatabaseError(err)
	}
	return r.GetHostingResourceState(ctx, params.AccountID)
}

func validateResourceConfirmation(accountID ID, revision int64, operationID ID, actorID *ID) error {
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
