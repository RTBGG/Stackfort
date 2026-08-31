// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

func (r *Repository) CreateDesiredStateRevision(
	ctx context.Context,
	params CreateDesiredStateRevisionParams,
) (DesiredStateRevision, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return DesiredStateRevision{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return DesiredStateRevision{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return DesiredStateRevision{}, err
	}
	documentJSON, err := encodeObject(params.Document, maxDesiredStateBytes)
	if err != nil {
		return DesiredStateRevision{}, err
	}
	document, err := decodeObject(documentJSON)
	if err != nil {
		return DesiredStateRevision{}, err
	}
	reason, err := validateOptionalText(params.Reason, "reason", 500)
	if err != nil {
		return DesiredStateRevision{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return DesiredStateRevision{}, err
	}
	id, err := r.newID()
	if err != nil {
		return DesiredStateRevision{}, err
	}
	now := r.timestamp()
	revision := DesiredStateRevision{
		ID:        id,
		AccountID: params.AccountID,
		Document:  document,
		Reason:    reason,
		CreatedAt: now,
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		if params.OperationID != nil {
			existing, findErr := desiredStateRevisionForOperationTx(
				ctx, executor, params.AccountID, *params.OperationID,
			)
			switch {
			case findErr == nil:
				revision = existing
				return nil
			case !errors.Is(findErr, sql.ErrNoRows):
				return findErr
			}
			var operationExists bool
			if err := executor.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM operations WHERE account_id = ? AND id = ?
				)`, string(params.AccountID), string(*params.OperationID)).Scan(&operationExists); err != nil {
				return err
			}
			if !operationExists {
				return fmt.Errorf("%w: operation does not belong to the account", ErrNotFound)
			}
		}
		if err := executor.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(sequence), 0) + 1
			FROM desired_state_revisions
			WHERE account_id = ?`, string(params.AccountID)).Scan(&revision.Sequence); err != nil {
			return err
		}
		_, err := executor.ExecContext(ctx, `
			INSERT INTO desired_state_revisions (
				id, account_id, sequence, document_json, reason, created_at, created_by_identity_id
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(revision.ID),
			string(revision.AccountID),
			revision.Sequence,
			documentJSON,
			nullableString(reason),
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		if params.OperationID != nil {
			if _, err := executor.ExecContext(ctx, `
				INSERT INTO operation_desired_state_revisions (
					operation_id, account_id, desired_state_revision_id, linked_at
				) VALUES (?, ?, ?, ?)`,
				string(*params.OperationID), string(params.AccountID), string(revision.ID), formatTime(now),
			); err != nil {
				return err
			}
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:     params.ActorID,
			Action:      "desired_state.revised",
			TargetType:  "desired_state_revision",
			TargetID:    string(revision.ID),
			AccountID:   &revision.AccountID,
			RequestID:   requestID,
			OperationID: params.OperationID,
			Result:      AuditSuccess,
			Details: map[string]any{
				"sequence": revision.Sequence,
			},
		}, now)
	})
	if err != nil {
		return DesiredStateRevision{}, classifyDatabaseError(err)
	}
	return revision, nil
}

func (r *Repository) DesiredStateRevisionForOperation(
	ctx context.Context,
	accountID ID,
	operationID ID,
) (DesiredStateRevision, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return DesiredStateRevision{}, err
	}
	if err := validateID(operationID, "operationId"); err != nil {
		return DesiredStateRevision{}, err
	}
	var revision DesiredStateRevision
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var readErr error
		revision, readErr = desiredStateRevisionForOperationTx(ctx, reader, accountID, operationID)
		return readErr
	})
	if err != nil {
		return DesiredStateRevision{}, classifyDatabaseError(err)
	}
	return revision, nil
}

func (r *Repository) LatestDesiredStateRevision(ctx context.Context, accountID ID) (DesiredStateRevision, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return DesiredStateRevision{}, err
	}
	var revision DesiredStateRevision
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var scanErr error
		revision, scanErr = scanDesiredStateRevision(reader.QueryRowContext(ctx, `
			SELECT id, account_id, sequence, document_json, reason, created_at
			FROM desired_state_revisions
			WHERE account_id = ?
			ORDER BY sequence DESC
			LIMIT 1`, string(accountID)))
		return scanErr
	})
	if err != nil {
		return DesiredStateRevision{}, classifyDatabaseError(err)
	}
	return revision, nil
}

func desiredStateRevisionForOperationTx(
	ctx context.Context,
	reader store.Reader,
	accountID ID,
	operationID ID,
) (DesiredStateRevision, error) {
	return scanDesiredStateRevision(reader.QueryRowContext(ctx, `
		SELECT revision.id, revision.account_id, revision.sequence,
		       revision.document_json, revision.reason, revision.created_at
		FROM operation_desired_state_revisions AS link
		JOIN desired_state_revisions AS revision
		  ON revision.account_id = link.account_id
		 AND revision.id = link.desired_state_revision_id
		WHERE link.account_id = ? AND link.operation_id = ?`,
		string(accountID), string(operationID),
	))
}

func scanDesiredStateRevision(row rowScanner) (DesiredStateRevision, error) {
	var revision DesiredStateRevision
	var documentJSON, createdAt string
	var reason sql.NullString
	if err := row.Scan(
		&revision.ID, &revision.AccountID, &revision.Sequence,
		&documentJSON, &reason, &createdAt,
	); err != nil {
		return DesiredStateRevision{}, err
	}
	var err error
	revision.Document, err = decodeObject(documentJSON)
	if err != nil {
		return DesiredStateRevision{}, err
	}
	revision.Reason = reason.String
	revision.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return DesiredStateRevision{}, err
	}
	return revision, nil
}
