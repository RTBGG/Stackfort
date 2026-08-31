// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

// MarkHostingUnixIdentityReconciled records successful identity and rootless
// OCI account-runtime reconciliation. It never changes the allocated username
// or numeric IDs and can backfill the runtime marker on an older reconciled
// identity.
func (r *Repository) MarkHostingUnixIdentityReconciled(
	ctx context.Context,
	params HostingAccountLifecycleParams,
) (HostingAccount, error) {
	requestID, err := validateHostingLifecycleParams(params)
	if err != nil {
		return HostingAccount{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var state string
		var runtimeReconciledAt sql.NullString
		if queryErr := executor.QueryRowContext(ctx, `
			SELECT lifecycle_state, oci_runtime_reconciled_at
			FROM hosting_account_unix_identities WHERE account_id = ?`,
			string(params.AccountID)).Scan(&state, &runtimeReconciledAt); queryErr != nil {
			return queryErr
		}
		if HostingUnixIdentityState(state) == HostingUnixIdentityReconciled && runtimeReconciledAt.Valid {
			return nil
		}
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE hosting_account_unix_identities
			SET lifecycle_state = 'reconciled',
			    reconciled_at = COALESCE(reconciled_at, ?),
			    oci_runtime_reconciled_at = COALESCE(oci_runtime_reconciled_at, ?)
			WHERE account_id = ? AND lifecycle_state IN ('allocated', 'reconciled')
			  AND EXISTS (
			      SELECT 1 FROM hosting_accounts
			      WHERE id = ? AND status IN ('active', 'suspended')
			  )`, formatTime(now), formatTime(now), string(params.AccountID), string(params.AccountID))
		if updateErr != nil {
			return updateErr
		}
		if updateErr = expectAffected(result); updateErr != nil {
			return updateErr
		}
		eventType := "hosting_account.unix_identity_reconciled"
		if HostingUnixIdentityState(state) == HostingUnixIdentityReconciled {
			eventType = "hosting_account.oci_runtime_reconciled"
		}
		return r.appendAccountLifecycleAudit(ctx, executor, params, requestID, eventType, now, nil)
	})
	if err != nil {
		return HostingAccount{}, classifyDatabaseError(err)
	}
	return r.GetHostingAccount(ctx, params.AccountID)
}

// RequestHostingAccountArchive disables the account first and starts the
// archive stage. Deletion cannot be requested from this state.
func (r *Repository) RequestHostingAccountArchive(
	ctx context.Context,
	params HostingAccountLifecycleParams,
) (HostingAccount, error) {
	requestID, err := validateHostingLifecycleParams(params)
	if err != nil {
		return HostingAccount{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE hosting_account_unix_identities
			SET lifecycle_state = 'archive_requested', archive_requested_at = ?
			WHERE account_id = ? AND lifecycle_state IN ('allocated', 'reconciled')`,
			formatTime(now), string(params.AccountID))
		if updateErr != nil {
			return updateErr
		}
		if updateErr = expectAffected(result); updateErr != nil {
			return updateErr
		}
		result, updateErr = executor.ExecContext(ctx, `
			UPDATE hosting_accounts
			SET status = 'archived', archived_at = ?, updated_at = ?
			WHERE id = ? AND status IN ('active', 'suspended')`,
			formatTime(now), formatTime(now), string(params.AccountID))
		if updateErr != nil {
			return updateErr
		}
		if updateErr = expectAffected(result); updateErr != nil {
			return updateErr
		}
		return r.appendAccountLifecycleAudit(ctx, executor, params, requestID,
			"hosting_account.archive_requested", now, nil)
	})
	if err != nil {
		return HostingAccount{}, classifyDatabaseError(err)
	}
	return r.GetHostingAccount(ctx, params.AccountID)
}

// ConfirmHostingAccountArchive records the durable archive artifact. A
// non-empty reference is required before deletion may be requested.
func (r *Repository) ConfirmHostingAccountArchive(
	ctx context.Context,
	params ConfirmHostingAccountArchiveParams,
) (HostingAccount, error) {
	base := HostingAccountLifecycleParams{
		AccountID: params.AccountID, ActorID: params.ActorID,
		OperationID: params.OperationID, RequestID: params.RequestID,
	}
	requestID, err := validateHostingLifecycleParams(base)
	if err != nil {
		return HostingAccount{}, err
	}
	reference, err := validateText(params.ArchiveReference, "archiveReference", 1, 512)
	if err != nil {
		return HostingAccount{}, err
	}
	if !archiveReferencePattern.MatchString(reference) {
		return HostingAccount{}, fmt.Errorf("%w: archiveReference must be an opaque bounded identifier", ErrInvalidInput)
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE hosting_account_unix_identities
			SET lifecycle_state = 'archived', archived_at = ?, archive_reference = ?
			WHERE account_id = ? AND lifecycle_state = 'archive_requested'
			  AND EXISTS (
			      SELECT 1 FROM hosting_accounts WHERE id = ? AND status = 'archived'
			  )`, formatTime(now), reference, string(params.AccountID), string(params.AccountID))
		if updateErr != nil {
			return updateErr
		}
		if updateErr = expectAffected(result); updateErr != nil {
			return updateErr
		}
		return r.appendAccountLifecycleAudit(ctx, executor, base, requestID,
			"hosting_account.archive_confirmed", now, map[string]any{"archiveReference": reference})
	})
	if err != nil {
		return HostingAccount{}, classifyDatabaseError(err)
	}
	return r.GetHostingAccount(ctx, params.AccountID)
}

// RequestHostingAccountDeletion is a distinct post-archive approval stage.
func (r *Repository) RequestHostingAccountDeletion(
	ctx context.Context,
	params HostingAccountLifecycleParams,
) (HostingAccount, error) {
	return r.advanceHostingLifecycle(ctx, params, HostingUnixIdentityArchived,
		HostingUnixIdentityDeletionRequested, "deletion_requested_at",
		"hosting_account.deletion_requested")
}

// ConfirmHostingAccountDeleted retains the account and identity as an audited
// tombstone after the host agent has removed the local user and group.
func (r *Repository) ConfirmHostingAccountDeleted(
	ctx context.Context,
	params HostingAccountLifecycleParams,
) (HostingAccount, error) {
	return r.advanceHostingLifecycle(ctx, params, HostingUnixIdentityDeletionRequested,
		HostingUnixIdentityDeleted, "deleted_at", "hosting_account.deletion_confirmed")
}

func (r *Repository) advanceHostingLifecycle(
	ctx context.Context,
	params HostingAccountLifecycleParams,
	from HostingUnixIdentityState,
	to HostingUnixIdentityState,
	timestampColumn string,
	action string,
) (HostingAccount, error) {
	requestID, err := validateHostingLifecycleParams(params)
	if err != nil {
		return HostingAccount{}, err
	}
	if timestampColumn != "deletion_requested_at" && timestampColumn != "deleted_at" {
		return HostingAccount{}, fmt.Errorf("unsupported hosting lifecycle timestamp column")
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		query := fmt.Sprintf(`
			UPDATE hosting_account_unix_identities
			SET lifecycle_state = ?, %s = ?
			WHERE account_id = ? AND lifecycle_state = ?
			  AND EXISTS (
			      SELECT 1 FROM hosting_accounts WHERE id = ? AND status = 'archived'
			  )`, timestampColumn)
		result, updateErr := executor.ExecContext(ctx, query, string(to), formatTime(now),
			string(params.AccountID), string(from), string(params.AccountID))
		if updateErr != nil {
			return updateErr
		}
		if updateErr = expectAffected(result); updateErr != nil {
			return updateErr
		}
		return r.appendAccountLifecycleAudit(ctx, executor, params, requestID, action, now, nil)
	})
	if err != nil {
		return HostingAccount{}, classifyDatabaseError(err)
	}
	return r.GetHostingAccount(ctx, params.AccountID)
}

func validateHostingLifecycleParams(params HostingAccountLifecycleParams) (string, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return "", err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return "", err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return "", err
	}
	return validateOptionalText(params.RequestID, "requestId", 128)
}

func (r *Repository) appendAccountLifecycleAudit(
	ctx context.Context,
	executor store.Executor,
	params HostingAccountLifecycleParams,
	requestID string,
	action string,
	now time.Time,
	details map[string]any,
) error {
	return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
		ActorID: params.ActorID, Action: action, TargetType: "hosting_account",
		TargetID: string(params.AccountID), AccountID: &params.AccountID,
		RequestID: requestID, OperationID: params.OperationID,
		Result: AuditSuccess, Details: details,
	}, now)
}
