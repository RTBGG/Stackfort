// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

const (
	defaultAdminListLimit = 50
	maximumAdminListLimit = 200
)

// ListPackages returns every package with its current immutable revision.
func (r *Repository) ListPackages(ctx context.Context) ([]Package, error) {
	packages := make([]Package, 0)
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT p.id, p.name, p.slug, p.status, p.current_revision,
			       r.limits_json, p.created_at, p.updated_at
			FROM packages AS p
			JOIN package_revisions AS r
			  ON r.package_id = p.id AND r.revision = p.current_revision
			ORDER BY p.name COLLATE NOCASE, p.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Package
			var status, limitsJSON, createdAt, updatedAt string
			if err := rows.Scan(
				&item.ID, &item.Name, &item.Slug, &status, &item.CurrentRevision,
				&limitsJSON, &createdAt, &updatedAt,
			); err != nil {
				return err
			}
			item.Status = PackageStatus(status)
			if item.Limits, err = decodeLimits(limitsJSON); err != nil {
				return err
			}
			if item.CreatedAt, err = parseTime(createdAt); err != nil {
				return err
			}
			if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
				return err
			}
			packages = append(packages, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return packages, nil
}

// ListHostingAccountSummaries returns account metadata and the assigned
// package revision without exposing privileged Unix identity details.
func (r *Repository) ListHostingAccountSummaries(ctx context.Context) ([]HostingAccountSummary, error) {
	accounts := make([]HostingAccountSummary, 0)
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT h.id, h.name, h.slug, h.status, h.current_package_assignment_id,
			       a.package_id, p.name, a.package_revision,
			       (h.status = 'active' AND u.lifecycle_state = 'reconciled' AND
			        u.oci_runtime_reconciled_at IS NOT NULL AND
			        f.status = 'applied' AND f.capability_status = 'available' AND
			        r.status = 'applied' AND r.capability_status = 'available'),
			       h.created_at, h.updated_at
			FROM hosting_accounts AS h
			JOIN account_package_assignments AS a ON a.id = h.current_package_assignment_id
			JOIN packages AS p ON p.id = a.package_id
			JOIN hosting_account_unix_identities AS u ON u.account_id = h.id
			JOIN hosting_account_filesystems AS f ON f.account_id = h.id
			JOIN hosting_account_resources AS r ON r.account_id = h.id
			ORDER BY h.name COLLATE NOCASE, h.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item HostingAccountSummary
			var status, createdAt, updatedAt string
			if err := rows.Scan(
				&item.ID, &item.Name, &item.Slug, &status, &item.CurrentPackageAssignmentID,
				&item.PackageID, &item.PackageName, &item.PackageRevision, &item.HostReady,
				&createdAt, &updatedAt,
			); err != nil {
				return err
			}
			item.Status = AccountStatus(status)
			if item.CreatedAt, err = parseTime(createdAt); err != nil {
				return err
			}
			if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
				return err
			}
			accounts = append(accounts, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return accounts, nil
}

// ListRecentOperations returns the newest operations across account and global
// scopes for the platform administrator overview.
func (r *Repository) ListRecentOperations(ctx context.Context, limit int) ([]Operation, error) {
	limit, err := normalizeAdminListLimit(limit)
	if err != nil {
		return nil, err
	}
	operations := make([]Operation, 0)
	err = r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT `+operationSelectColumns+`
			FROM operations
			ORDER BY created_at DESC, id DESC
			LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			operation, err := scanOperation(rows)
			if err != nil {
				return err
			}
			operations = append(operations, operation)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return operations, nil
}

// ListRecentAuditEvents returns the newest verified-schema audit records. Hash
// bytes remain internal; callers receive only the structured event fields.
func (r *Repository) ListRecentAuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	limit, err := normalizeAdminListLimit(limit)
	if err != nil {
		return nil, err
	}
	events := make([]AuditEvent, 0)
	err = r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT sequence, id, occurred_at, actor_identity_id, session_id,
			       source_address, action, target_type, target_id, account_id,
			       request_id, operation_id, result, details_json
			FROM audit_events
			ORDER BY sequence DESC
			LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var event AuditEvent
			var occurredAt, result, detailsJSON string
			var actorID, sessionID, sourceAddress, targetID sql.NullString
			var accountID, requestID, operationID sql.NullString
			if err := rows.Scan(
				&event.Sequence, &event.ID, &occurredAt, &actorID, &sessionID,
				&sourceAddress, &event.Action, &event.TargetType, &targetID, &accountID,
				&requestID, &operationID, &result, &detailsJSON,
			); err != nil {
				return err
			}
			event.ActorID = idFromNull(actorID)
			event.SessionID = idFromNull(sessionID)
			event.AccountID = idFromNull(accountID)
			event.OperationID = idFromNull(operationID)
			event.SourceAddress = sourceAddress.String
			event.TargetID = targetID.String
			event.RequestID = requestID.String
			event.Result = AuditResult(result)
			if event.OccurredAt, err = parseTime(occurredAt); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(detailsJSON), &event.Details); err != nil {
				return fmt.Errorf("decode stored audit details: %w", err)
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

func normalizeAdminListLimit(limit int) (int, error) {
	if limit == 0 {
		return defaultAdminListLimit, nil
	}
	if limit < 1 || limit > maximumAdminListLimit {
		return 0, fmt.Errorf("%w: list limit must be between 1 and %d", ErrInvalidInput, maximumAdminListLimit)
	}
	return limit, nil
}
