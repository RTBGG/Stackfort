// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

// RecordAppliedStateRevision atomically replaces an account's current applied
// revision. It records only a digest of the rendered configuration; secrets and
// generated configuration content belong outside the control-plane database.
func (r *Repository) RecordAppliedStateRevision(
	ctx context.Context,
	params RecordAppliedStateRevisionParams,
) (AppliedStateRevision, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return AppliedStateRevision{}, err
	}
	if err := validateID(params.DesiredStateRevisionID, "desiredStateRevisionId"); err != nil {
		return AppliedStateRevision{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return AppliedStateRevision{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return AppliedStateRevision{}, err
	}
	if len(params.ConfigDigest) != sha256.Size {
		return AppliedStateRevision{}, fmt.Errorf("%w: config digest must be a SHA-256 digest", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return AppliedStateRevision{}, err
	}
	id, err := r.newID()
	if err != nil {
		return AppliedStateRevision{}, err
	}
	now := r.timestamp()
	revision := AppliedStateRevision{
		ID:                     id,
		AccountID:              params.AccountID,
		DesiredStateRevisionID: params.DesiredStateRevisionID,
		OperationID:            params.OperationID,
		ConfigDigest:           append([]byte(nil), params.ConfigDigest...),
		Status:                 AppliedStateActive,
		AppliedAt:              now,
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		var accountExists bool
		if err := executor.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM hosting_accounts WHERE id = ?)`,
			string(params.AccountID),
		).Scan(&accountExists); err != nil {
			return err
		}
		if !accountExists {
			return fmt.Errorf("%w: hosting account does not exist", ErrNotFound)
		}
		var desiredExists bool
		if err := executor.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM desired_state_revisions
				WHERE account_id = ? AND id = ?
			)`, string(params.AccountID), string(params.DesiredStateRevisionID)).Scan(&desiredExists); err != nil {
			return err
		}
		if !desiredExists {
			return fmt.Errorf("%w: desired-state revision does not belong to the account", ErrNotFound)
		}
		if params.OperationID != nil {
			var operationExists bool
			if err := executor.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM operations
					WHERE account_id = ? AND id = ?
				)`, string(params.AccountID), string(*params.OperationID)).Scan(&operationExists); err != nil {
				return err
			}
			if !operationExists {
				return fmt.Errorf("%w: operation does not belong to the account", ErrNotFound)
			}

			var existing AppliedStateRevision
			var existingOperationID, existingSupersededAt sql.NullString
			var existingStatus, existingAppliedAt string
			err := executor.QueryRowContext(ctx, `
				SELECT id, account_id, desired_state_revision_id, operation_id,
				       config_digest, status, applied_at, superseded_at
				FROM applied_state_revisions
				WHERE account_id = ? AND operation_id = ?`,
				string(params.AccountID), string(*params.OperationID),
			).Scan(
				&existing.ID,
				&existing.AccountID,
				&existing.DesiredStateRevisionID,
				&existingOperationID,
				&existing.ConfigDigest,
				&existingStatus,
				&existingAppliedAt,
				&existingSupersededAt,
			)
			switch {
			case err == nil:
				if existing.DesiredStateRevisionID != params.DesiredStateRevisionID ||
					!bytes.Equal(existing.ConfigDigest, params.ConfigDigest) {
					return fmt.Errorf("%w: operation was already recorded with different applied state", ErrConflict)
				}
				if existingStatus != string(AppliedStateActive) {
					return fmt.Errorf("%w: operation's applied state is no longer active", ErrConflict)
				}
				if !existingOperationID.Valid || existingOperationID.String != string(*params.OperationID) {
					return errors.New("stored applied-state operation correlation is invalid")
				}
				existing.OperationID = params.OperationID
				existing.Status = AppliedStateStatus(existingStatus)
				existing.AppliedAt, err = parseTime(existingAppliedAt)
				if err != nil {
					return err
				}
				existing.SupersededAt, err = parseOptionalTime(existingSupersededAt)
				if err != nil {
					return err
				}
				revision = existing
				return nil
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
		}

		if _, err := executor.ExecContext(ctx, `
			UPDATE applied_state_revisions
			SET status = 'superseded', superseded_at = ?
			WHERE account_id = ? AND status = 'active'`,
			formatTime(now),
			string(params.AccountID),
		); err != nil {
			return err
		}
		_, err := executor.ExecContext(ctx, `
			INSERT INTO applied_state_revisions (
				id, account_id, desired_state_revision_id, operation_id,
				config_digest, status, applied_at
			) VALUES (?, ?, ?, ?, ?, 'active', ?)`,
			string(revision.ID),
			string(revision.AccountID),
			string(revision.DesiredStateRevisionID),
			nullableID(revision.OperationID),
			revision.ConfigDigest,
			formatTime(now),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:     params.ActorID,
			Action:      "applied_state.recorded",
			TargetType:  "applied_state_revision",
			TargetID:    string(revision.ID),
			AccountID:   &revision.AccountID,
			RequestID:   requestID,
			OperationID: revision.OperationID,
			Result:      AuditSuccess,
			Details: map[string]any{
				"desiredStateRevisionId": revision.DesiredStateRevisionID,
			},
		}, now)
	})
	if err != nil {
		return AppliedStateRevision{}, classifyDatabaseError(err)
	}
	return revision, nil
}

func (r *Repository) CurrentAppliedStateRevision(ctx context.Context, accountID ID) (AppliedStateRevision, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return AppliedStateRevision{}, err
	}
	var revision AppliedStateRevision
	var operationID, supersededAt sql.NullString
	var status, appliedAt string
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT id, account_id, desired_state_revision_id, operation_id,
			       config_digest, status, applied_at, superseded_at
			FROM applied_state_revisions
			WHERE account_id = ? AND status = 'active'`, string(accountID)).Scan(
			&revision.ID,
			&revision.AccountID,
			&revision.DesiredStateRevisionID,
			&operationID,
			&revision.ConfigDigest,
			&status,
			&appliedAt,
			&supersededAt,
		)
	})
	if err != nil {
		return AppliedStateRevision{}, classifyDatabaseError(err)
	}
	revision.Status = AppliedStateStatus(status)
	if operationID.Valid {
		value := ID(operationID.String)
		revision.OperationID = &value
	}
	if revision.AppliedAt, err = parseTime(appliedAt); err != nil {
		return AppliedStateRevision{}, err
	}
	if revision.SupersededAt, err = parseOptionalTime(supersededAt); err != nil {
		return AppliedStateRevision{}, err
	}
	return revision, nil
}
