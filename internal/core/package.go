// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

func (r *Repository) CreatePackage(ctx context.Context, params CreatePackageParams) (Package, error) {
	name, err := validateText(params.Name, "name", 1, 120)
	if err != nil {
		return Package{}, err
	}
	slug, err := validateSlug(params.Slug)
	if err != nil {
		return Package{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Package{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Package{}, err
	}
	limitsJSON, limits, err := encodeLimits(params.Limits)
	if err != nil {
		return Package{}, err
	}
	id, err := r.newID()
	if err != nil {
		return Package{}, err
	}
	now := r.timestamp()
	result := Package{
		ID:              id,
		Name:            name,
		Slug:            slug,
		Status:          PackageActive,
		CurrentRevision: 1,
		Limits:          limits,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			INSERT INTO packages (
				id, name, slug, status, current_revision, created_at, updated_at, created_by_identity_id
			) VALUES (?, ?, ?, 'active', 1, ?, ?, ?)`,
			string(result.ID),
			result.Name,
			result.Slug,
			formatTime(now),
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO package_revisions (
				package_id, revision, limits_json, created_at, created_by_identity_id
			) VALUES (?, 1, ?, ?, ?)`,
			string(result.ID),
			limitsJSON,
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "package.created",
			TargetType: "package",
			TargetID:   string(result.ID),
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"revision": 1,
			},
		}, now)
	})
	if err != nil {
		return Package{}, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) UpdatePackage(ctx context.Context, params UpdatePackageParams) (Package, error) {
	if err := validateID(params.PackageID, "packageId"); err != nil {
		return Package{}, err
	}
	if params.ExpectedRevision < 1 {
		return Package{}, fmt.Errorf("%w: expectedRevision must be positive", ErrInvalidInput)
	}
	name, err := validateText(params.Name, "name", 1, 120)
	if err != nil {
		return Package{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Package{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Package{}, err
	}
	limitsJSON, limits, err := encodeLimits(params.Limits)
	if err != nil {
		return Package{}, err
	}
	now := r.timestamp()
	var result Package

	err = r.state.Write(ctx, func(executor store.Executor) error {
		var status string
		var createdAt string
		if err := executor.QueryRowContext(ctx, `
			SELECT name, slug, status, current_revision, created_at
			FROM packages
			WHERE id = ?`, string(params.PackageID)).Scan(
			&result.Name,
			&result.Slug,
			&status,
			&result.CurrentRevision,
			&createdAt,
		); err != nil {
			return err
		}
		if result.CurrentRevision != params.ExpectedRevision {
			return fmt.Errorf("%w: package revision is %d, expected %d", ErrConflict, result.CurrentRevision, params.ExpectedRevision)
		}
		if PackageStatus(status) != PackageActive {
			return fmt.Errorf("%w: archived package cannot be updated", ErrConflict)
		}
		created, err := parseTime(createdAt)
		if err != nil {
			return err
		}
		newRevision := result.CurrentRevision + 1
		_, err = executor.ExecContext(ctx, `
			INSERT INTO package_revisions (
				package_id, revision, limits_json, created_at, created_by_identity_id
			) VALUES (?, ?, ?, ?, ?)`,
			string(params.PackageID),
			newRevision,
			limitsJSON,
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			UPDATE packages
			SET name = ?, current_revision = ?, updated_at = ?
			WHERE id = ? AND current_revision = ?`,
			name,
			newRevision,
			formatTime(now),
			string(params.PackageID),
			params.ExpectedRevision,
		)
		if err != nil {
			return err
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "package.updated",
			TargetType: "package",
			TargetID:   string(params.PackageID),
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"previousRevision": params.ExpectedRevision,
				"revision":         newRevision,
			},
		}, now); err != nil {
			return err
		}
		result.ID = params.PackageID
		result.Name = name
		result.Status = PackageActive
		result.CurrentRevision = newRevision
		result.Limits = limits
		result.CreatedAt = created
		result.UpdatedAt = now
		return nil
	})
	if err != nil {
		return Package{}, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) GetPackage(ctx context.Context, packageID ID) (Package, error) {
	if err := validateID(packageID, "packageId"); err != nil {
		return Package{}, err
	}

	var result Package
	var status, limitsJSON, createdAt, updatedAt string
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT p.id, p.name, p.slug, p.status, p.current_revision,
			       r.limits_json, p.created_at, p.updated_at
			FROM packages AS p
			JOIN package_revisions AS r
			  ON r.package_id = p.id AND r.revision = p.current_revision
			WHERE p.id = ?`, string(packageID)).Scan(
			&result.ID,
			&result.Name,
			&result.Slug,
			&status,
			&result.CurrentRevision,
			&limitsJSON,
			&createdAt,
			&updatedAt,
		)
	})
	if err != nil {
		return Package{}, classifyDatabaseError(err)
	}
	result.Status = PackageStatus(status)
	result.Limits, err = decodeLimits(limitsJSON)
	if err != nil {
		return Package{}, err
	}
	result.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Package{}, err
	}
	result.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Package{}, err
	}
	return result, nil
}

func currentPackageRevision(ctx context.Context, executor store.Executor, packageID ID) (int64, string, error) {
	var revision int64
	var limitsJSON string
	var status string
	err := executor.QueryRowContext(ctx, `
		SELECT p.current_revision, r.limits_json, p.status
		FROM packages AS p
		JOIN package_revisions AS r
		  ON r.package_id = p.id AND r.revision = p.current_revision
		WHERE p.id = ?`, string(packageID)).Scan(&revision, &limitsJSON, &status)
	if err != nil {
		return 0, "", err
	}
	if PackageStatus(status) != PackageActive {
		return 0, "", fmt.Errorf("%w: archived package cannot be assigned", ErrConflict)
	}
	return revision, limitsJSON, nil
}

func expectAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New("unexpected affected row count")
	}
	return nil
}
