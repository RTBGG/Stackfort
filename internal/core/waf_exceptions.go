// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

func (r *Repository) CreateDomainWAFException(
	ctx context.Context,
	params CreateDomainWAFExceptionParams,
) (DomainWAFException, error) {
	if err := validateWAFExceptionIDs(params.AccountID, params.DomainID, params.ExceptionID, params.OperationID, params.ActorID); err != nil {
		return DomainWAFException{}, err
	}
	if params.ExceptionID != params.OperationID {
		return DomainWAFException{}, fmt.Errorf("%w: exception creation must use the operation ID", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return DomainWAFException{}, err
	}
	if err := wafconfig.ValidateExceptionScope(params.RuleID, params.RequestPath, params.Parameter); err != nil {
		return DomainWAFException{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	now := r.timestamp().UTC()
	if !params.ExpiresAt.Equal(params.ExpiresAt.UTC()) || params.ExpiresAt.Before(now.Add(time.Second)) ||
		params.ExpiresAt.After(now.Add(wafconfig.MaximumExceptionTTL)) {
		return DomainWAFException{}, fmt.Errorf("%w: invalid WAF exception expiry", ErrInvalidInput)
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		var creationOperation string
		replayErr := executor.QueryRowContext(ctx, `
			SELECT creation_operation_id
			FROM domain_waf_exceptions
			WHERE account_id = ? AND id = ?`, string(params.AccountID), string(params.ExceptionID)).Scan(&creationOperation)
		if replayErr == nil {
			if creationOperation == string(params.OperationID) {
				return nil
			}
			return fmt.Errorf("%w: WAF exception ID already exists", ErrConflict)
		}
		if !errors.Is(replayErr, sql.ErrNoRows) {
			return replayErr
		}
		limits, err := mutableAccountLimits(ctx, executor, params.AccountID)
		if err != nil {
			return err
		}
		if !limits.Features.WAFExceptions {
			return fmt.Errorf("%w: WAF exceptions are not enabled by the account package", ErrConflict)
		}
		var mode string
		if err := executor.QueryRowContext(ctx, `
			SELECT waf.mode
			FROM domains AS domain
			JOIN domain_waf_policies AS waf
			  ON waf.account_id = domain.account_id AND waf.domain_id = domain.id
			WHERE domain.account_id = ? AND domain.id = ? AND domain.removed_at IS NULL`,
			string(params.AccountID), string(params.DomainID)).Scan(&mode); err != nil {
			return err
		}
		if WAFMode(mode) == WAFModeOff {
			return fmt.Errorf("%w: WAF exceptions require an enabled domain WAF policy", ErrConflict)
		}
		var activeCount, duplicateCount int64
		if err := executor.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(SUM(
				CASE WHEN rule_id = ? AND request_path = ? AND parameter_name = ? THEN 1 ELSE 0 END
			), 0)
			FROM domain_waf_exceptions
			WHERE account_id = ? AND domain_id = ? AND removed_at IS NULL AND expires_at > ?`,
			params.RuleID, params.RequestPath, params.Parameter,
			string(params.AccountID), string(params.DomainID), formatTime(now),
		).Scan(&activeCount, &duplicateCount); err != nil {
			return err
		}
		if activeCount >= wafconfig.MaximumExceptions {
			return fmt.Errorf("%w: WAF exception limit reached", ErrConflict)
		}
		if duplicateCount != 0 {
			return fmt.Errorf("%w: an active WAF exception already has this scope", ErrConflict)
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO domain_waf_exceptions (
				id, account_id, domain_id, rule_id, request_path, parameter_name,
				expires_at, created_at, created_by_identity_id, creation_operation_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(params.ExceptionID), string(params.AccountID), string(params.DomainID),
			params.RuleID, params.RequestPath, params.Parameter, formatTime(params.ExpiresAt),
			formatTime(now), nullableID(params.ActorID), string(params.OperationID),
		); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "domain.waf_exception.created",
			TargetType: "domain_waf_exception", TargetID: string(params.ExceptionID),
			AccountID: &params.AccountID, RequestID: requestID, OperationID: &params.OperationID,
			Result: AuditSuccess, Details: map[string]any{
				"domainId": string(params.DomainID), "ruleId": params.RuleID,
				"hasExactPath": params.RequestPath != "", "parameter": params.Parameter,
				"expiresAt": formatTime(params.ExpiresAt),
			},
		}, now)
	})
	if err != nil {
		return DomainWAFException{}, classifyDatabaseError(err)
	}
	return r.getDomainWAFException(ctx, params.AccountID, params.DomainID, params.ExceptionID, false)
}

func (r *Repository) RemoveDomainWAFException(ctx context.Context, params RemoveDomainWAFExceptionParams) error {
	if err := validateWAFExceptionIDs(params.AccountID, params.DomainID, params.ExceptionID, params.OperationID, params.ActorID); err != nil {
		return err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	now := r.timestamp().UTC()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := mutableAccountLimits(ctx, executor, params.AccountID); err != nil {
			return err
		}
		var removedAt, removalOperation sql.NullString
		if err := executor.QueryRowContext(ctx, `
			SELECT removed_at, removal_operation_id
			FROM domain_waf_exceptions
			WHERE account_id = ? AND domain_id = ? AND id = ?`,
			string(params.AccountID), string(params.DomainID), string(params.ExceptionID),
		).Scan(&removedAt, &removalOperation); err != nil {
			return err
		}
		if removedAt.Valid {
			if removalOperation.String == string(params.OperationID) {
				return nil
			}
			return fmt.Errorf("%w: WAF exception is already removed", ErrConflict)
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE domain_waf_exceptions
			SET removed_at = ?, removed_by_identity_id = ?, removal_operation_id = ?
			WHERE account_id = ? AND domain_id = ? AND id = ? AND removed_at IS NULL`,
			formatTime(now), nullableID(params.ActorID), string(params.OperationID),
			string(params.AccountID), string(params.DomainID), string(params.ExceptionID),
		); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "domain.waf_exception.removed",
			TargetType: "domain_waf_exception", TargetID: string(params.ExceptionID),
			AccountID: &params.AccountID, RequestID: requestID, OperationID: &params.OperationID,
			Result: AuditSuccess, Details: map[string]any{"domainId": string(params.DomainID)},
		}, now)
	})
	return classifyDatabaseError(err)
}

func (r *Repository) ListDomainWAFExceptions(
	ctx context.Context,
	accountID, domainID ID,
) ([]DomainWAFException, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return nil, err
	}
	if err := validateID(domainID, "domainId"); err != nil {
		return nil, err
	}
	now := formatTime(r.timestamp().UTC())
	result := make([]DomainWAFException, 0)
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT id, account_id, domain_id, rule_id, request_path, parameter_name, expires_at, created_at
			FROM domain_waf_exceptions
			WHERE account_id = ? AND domain_id = ? AND removed_at IS NULL AND expires_at > ?
			ORDER BY expires_at, id`, string(accountID), string(domainID), now)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanDomainWAFException(rows)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) getDomainWAFException(ctx context.Context, accountID, domainID, exceptionID ID, activeOnly bool) (DomainWAFException, error) {
	query := `SELECT id, account_id, domain_id, rule_id, request_path, parameter_name, expires_at, created_at
		FROM domain_waf_exceptions WHERE account_id = ? AND domain_id = ? AND id = ?`
	args := []any{string(accountID), string(domainID), string(exceptionID)}
	if activeOnly {
		query += ` AND removed_at IS NULL AND expires_at > ?`
		args = append(args, formatTime(r.timestamp().UTC()))
	}
	var result DomainWAFException
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var expiresAt, createdAt string
		if err := reader.QueryRowContext(ctx, query, args...).Scan(
			&result.ID, &result.AccountID, &result.DomainID, &result.RuleID,
			&result.RequestPath, &result.Parameter, &expiresAt, &createdAt,
		); err != nil {
			return err
		}
		var err error
		if result.ExpiresAt, err = parseTime(expiresAt); err != nil {
			return err
		}
		result.CreatedAt, err = parseTime(createdAt)
		return err
	})
	if err != nil {
		return DomainWAFException{}, classifyDatabaseError(err)
	}
	return result, nil
}

type wafExceptionScanner interface{ Scan(...any) error }

func scanDomainWAFException(scanner wafExceptionScanner) (DomainWAFException, error) {
	var item DomainWAFException
	var expiresAt, createdAt string
	if err := scanner.Scan(
		&item.ID, &item.AccountID, &item.DomainID, &item.RuleID,
		&item.RequestPath, &item.Parameter, &expiresAt, &createdAt,
	); err != nil {
		return DomainWAFException{}, err
	}
	var err error
	if item.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return DomainWAFException{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return DomainWAFException{}, err
	}
	return item, nil
}

func validateWAFExceptionIDs(accountID, domainID, exceptionID, operationID ID, actorID *ID) error {
	for name, value := range map[string]ID{
		"accountId": accountID, "domainId": domainID, "exceptionId": exceptionID, "operationId": operationID,
	} {
		if err := validateID(value, name); err != nil {
			return err
		}
	}
	return validateOptionalID(actorID, "actorId")
}
