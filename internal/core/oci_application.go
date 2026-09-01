// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/store"
)

func (r *Repository) CreateOCIApplication(
	ctx context.Context, params CreateOCIApplicationParams,
) (OCIApplication, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return OCIApplication{}, err
	}
	if err := validateID(params.ActorID, "actorId"); err != nil {
		return OCIApplication{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIApplication{}, err
	}
	name, err := validateOCIApplicationName(params.Name)
	if err != nil {
		return OCIApplication{}, err
	}
	slug, err := validateSlug(params.Slug)
	if err != nil {
		return OCIApplication{}, err
	}
	spec, err := ociapps.Normalize(params.Spec)
	if err != nil {
		return OCIApplication{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	applicationID, err := r.newID()
	if err != nil {
		return OCIApplication{}, err
	}
	now := r.timestamp()
	application := OCIApplication{
		ID: applicationID, AccountID: params.AccountID, Name: name, Slug: slug,
		Spec: spec, Status: OCIApplicationDraft, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		limits, limitsErr := currentPackageLimitsTx(ctx, executor, params.AccountID)
		if limitsErr != nil {
			return limitsErr
		}
		if !limits.Features.OCIApplications || limits.MaxOCIApplications == 0 {
			return fmt.Errorf("%w: OCI applications are not enabled by the account package", ErrConflict)
		}
		var count int64
		if err := executor.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM oci_applications
			WHERE account_id = ? AND removed_at IS NULL`, string(params.AccountID)).Scan(&count); err != nil {
			return err
		}
		if count >= limits.MaxOCIApplications || count >= ociapps.MaximumApplicationsPerAccount {
			return fmt.Errorf("%w: package OCI application limit reached", ErrConflict)
		}
		if err := validateOCIApplicationResourceReferencesTx(ctx, executor, params.AccountID, spec); err != nil {
			return err
		}
		if err := insertOCIApplicationTx(ctx, executor, application); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_application.draft_created",
			TargetType: "oci_application", TargetID: string(application.ID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{
				"sourceKind":   application.Spec.Source.Kind,
				"internalPort": application.Spec.InternalPort,
				"healthKind":   application.Spec.Health.Kind,
			},
		}, now)
	})
	if err != nil {
		return OCIApplication{}, classifyDatabaseError(err)
	}
	return application, nil
}

func (r *Repository) UpdateOCIApplicationDraft(
	ctx context.Context, params UpdateOCIApplicationDraftParams,
) (OCIApplication, error) {
	if err := validateOCIApplicationMutationIDs(params.AccountID, params.ApplicationID, params.ActorID); err != nil {
		return OCIApplication{}, err
	}
	if params.ExpectedRevision < 1 {
		return OCIApplication{}, fmt.Errorf("%w: expectedRevision must be positive", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIApplication{}, err
	}
	name, err := validateOCIApplicationName(params.Name)
	if err != nil {
		return OCIApplication{}, err
	}
	slug, err := validateSlug(params.Slug)
	if err != nil {
		return OCIApplication{}, err
	}
	spec, err := ociapps.Normalize(params.Spec)
	if err != nil {
		return OCIApplication{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	now := r.timestamp()
	var application OCIApplication
	err = r.state.Write(ctx, func(executor store.Executor) error {
		limits, limitsErr := currentPackageLimitsTx(ctx, executor, params.AccountID)
		if limitsErr != nil {
			return limitsErr
		}
		if !limits.Features.OCIApplications {
			return fmt.Errorf("%w: OCI applications are not enabled by the account package", ErrConflict)
		}
		current, findErr := findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		if findErr != nil {
			return findErr
		}
		if current.Status != OCIApplicationDraft || current.Revision != params.ExpectedRevision {
			return fmt.Errorf("%w: OCI application draft revision or state changed", ErrConflict)
		}
		if err := validateOCIApplicationResourceReferencesTx(ctx, executor, params.AccountID, spec); err != nil {
			return err
		}
		nextRevision := current.Revision + 1
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE oci_applications
			SET name = ?, slug = ?, source_kind = ?, image_reference = ?, build_context = ?,
			    containerfile_path = ?, internal_port = ?, health_kind = ?, health_path = ?,
			    health_interval_seconds = ?, health_timeout_seconds = ?, health_retries = ?,
			    revision = ?, updated_at = ?
			WHERE account_id = ? AND id = ? AND status = 'draft' AND revision = ?`,
			name, slug, string(spec.Source.Kind), nullableString(spec.Source.ImageReference),
			nullableString(spec.Source.BuildContext), nullableString(spec.Source.ContainerfilePath),
			spec.InternalPort, string(spec.Health.Kind), nullableString(spec.Health.Path),
			spec.Health.IntervalSeconds, spec.Health.TimeoutSeconds, spec.Health.Retries,
			nextRevision, formatTime(now), string(params.AccountID), string(params.ApplicationID), params.ExpectedRevision)
		if updateErr != nil {
			return updateErr
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: OCI application changed concurrently", ErrConflict)
		}
		if err := replaceOCIApplicationResourceReferencesTx(
			ctx, executor, params.AccountID, params.ApplicationID, spec,
		); err != nil {
			return err
		}
		application, findErr = findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		if findErr != nil {
			return findErr
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_application.draft_updated",
			TargetType: "oci_application", TargetID: string(params.ApplicationID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{
				"revision": nextRevision, "sourceKind": spec.Source.Kind,
				"internalPort": spec.InternalPort, "healthKind": spec.Health.Kind,
			},
		}, now)
	})
	if err != nil {
		return OCIApplication{}, classifyDatabaseError(err)
	}
	return application, nil
}

func (r *Repository) RemoveOCIApplicationDraft(
	ctx context.Context, params RemoveOCIApplicationDraftParams,
) error {
	if err := validateOCIApplicationMutationIDs(params.AccountID, params.ApplicationID, params.ActorID); err != nil {
		return err
	}
	if params.ExpectedRevision < 1 {
		return fmt.Errorf("%w: expectedRevision must be positive", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, limitsErr := currentPackageLimitsTx(ctx, executor, params.AccountID); limitsErr != nil {
			return limitsErr
		}
		current, findErr := findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		if findErr != nil {
			return findErr
		}
		if current.Status != OCIApplicationDraft || current.Revision != params.ExpectedRevision {
			return fmt.Errorf("%w: only the current OCI application draft may be removed", ErrConflict)
		}
		if err := deleteOCIApplicationResourceReferencesTx(ctx, executor, params.ApplicationID); err != nil {
			return err
		}
		nextRevision := current.Revision + 1
		result, updateErr := executor.ExecContext(ctx, `
			UPDATE oci_applications
			SET status = 'deleted', revision = ?, updated_at = ?, removed_at = ?
			WHERE account_id = ? AND id = ? AND status = 'draft' AND revision = ?`,
			nextRevision, formatTime(now), formatTime(now), string(params.AccountID),
			string(params.ApplicationID), params.ExpectedRevision)
		if updateErr != nil {
			return updateErr
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: OCI application changed concurrently", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_application.draft_removed",
			TargetType: "oci_application", TargetID: string(params.ApplicationID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{"revision": nextRevision},
		}, now)
	})
	return classifyDatabaseError(err)
}

func (r *Repository) ListOCIApplications(ctx context.Context, accountID ID) ([]OCIApplication, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return nil, err
	}
	applications := []OCIApplication{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, ociApplicationSelect+`
			WHERE account_id = ? AND removed_at IS NULL ORDER BY created_at, id`, string(accountID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			application, scanErr := scanOCIApplication(rows)
			if scanErr != nil {
				return scanErr
			}
			applications = append(applications, application)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for index := range applications {
			if err := loadOCIApplicationResourceReferencesTx(ctx, reader, &applications[index]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	return applications, nil
}

func (r *Repository) GetOCIApplication(ctx context.Context, accountID, applicationID ID) (OCIApplication, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return OCIApplication{}, err
	}
	if err := validateID(applicationID, "applicationId"); err != nil {
		return OCIApplication{}, err
	}
	var application OCIApplication
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		application, err = findOCIApplicationTx(ctx, reader, accountID, applicationID, false)
		return err
	})
	if err != nil {
		return OCIApplication{}, classifyDatabaseError(err)
	}
	return application, nil
}

const ociApplicationSelect = `SELECT id, account_id, name, slug, source_kind,
	image_reference, build_context, containerfile_path, internal_port,
	health_kind, health_path, health_interval_seconds, health_timeout_seconds, health_retries,
	status, revision, applied_revision, created_at, updated_at, removed_at
	FROM oci_applications `

func findOCIApplicationTx(
	ctx context.Context, reader store.Reader, accountID, applicationID ID, includeRemoved bool,
) (OCIApplication, error) {
	query := ociApplicationSelect + ` WHERE account_id = ? AND id = ?`
	if !includeRemoved {
		query += ` AND removed_at IS NULL`
	}
	application, err := scanOCIApplication(reader.QueryRowContext(ctx, query, string(accountID), string(applicationID)))
	if err != nil {
		return OCIApplication{}, err
	}
	if err := loadOCIApplicationResourceReferencesTx(ctx, reader, &application); err != nil {
		return OCIApplication{}, err
	}
	return application, nil
}

func scanOCIApplication(scanner rowScanner) (OCIApplication, error) {
	var application OCIApplication
	var sourceKind, healthKind, status string
	var imageReference, buildContext, containerfilePath, healthPath sql.NullString
	var appliedRevision sql.NullInt64
	var createdAt, updatedAt string
	var removedAt sql.NullString
	if err := scanner.Scan(
		&application.ID, &application.AccountID, &application.Name, &application.Slug, &sourceKind,
		&imageReference, &buildContext, &containerfilePath, &application.Spec.InternalPort,
		&healthKind, &healthPath, &application.Spec.Health.IntervalSeconds,
		&application.Spec.Health.TimeoutSeconds, &application.Spec.Health.Retries,
		&status, &application.Revision, &appliedRevision, &createdAt, &updatedAt, &removedAt,
	); err != nil {
		return OCIApplication{}, err
	}
	application.Spec.Source = ociapps.Source{
		Kind: ociapps.SourceKind(sourceKind), ImageReference: imageReference.String,
		BuildContext: buildContext.String, ContainerfilePath: containerfilePath.String,
	}
	application.Spec.Health.Kind, application.Spec.Health.Path = ociapps.HealthKind(healthKind), healthPath.String
	application.Status = OCIApplicationStatus(status)
	if normalized, err := ociapps.Normalize(application.Spec); err != nil || !reflect.DeepEqual(normalized, application.Spec) ||
		!validOCIApplicationStatus(application.Status) || application.Revision < 1 {
		return OCIApplication{}, errors.New("stored OCI application is invalid")
	}
	if appliedRevision.Valid {
		application.AppliedRevision = &appliedRevision.Int64
	}
	var err error
	if application.CreatedAt, err = parseTime(createdAt); err != nil {
		return OCIApplication{}, err
	}
	if application.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return OCIApplication{}, err
	}
	if application.RemovedAt, err = parseOptionalTime(removedAt); err != nil {
		return OCIApplication{}, err
	}
	return application, nil
}

func insertOCIApplicationTx(ctx context.Context, executor store.Executor, application OCIApplication) error {
	_, err := executor.ExecContext(ctx, `
		INSERT INTO oci_applications (
			id, account_id, name, slug, source_kind, image_reference, build_context, containerfile_path,
			internal_port, health_kind, health_path, health_interval_seconds,
			health_timeout_seconds, health_retries, status, revision, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', 1, ?, ?)`,
		string(application.ID), string(application.AccountID), application.Name, application.Slug,
		string(application.Spec.Source.Kind), nullableString(application.Spec.Source.ImageReference),
		nullableString(application.Spec.Source.BuildContext), nullableString(application.Spec.Source.ContainerfilePath),
		application.Spec.InternalPort, string(application.Spec.Health.Kind), nullableString(application.Spec.Health.Path),
		application.Spec.Health.IntervalSeconds, application.Spec.Health.TimeoutSeconds, application.Spec.Health.Retries,
		formatTime(application.CreatedAt), formatTime(application.UpdatedAt))
	if err != nil {
		return err
	}
	return insertOCIApplicationResourceReferencesTx(ctx, executor, application.AccountID, application.ID, application.Spec)
}

func validateOCIApplicationResourceReferencesTx(
	ctx context.Context, reader store.Reader, accountID ID, spec ociapps.Spec,
) error {
	for _, reference := range spec.SecretReferences {
		secretID, err := ParseID(reference.SecretID)
		if err != nil {
			return fmt.Errorf("%w: OCI environment reference is invalid", ErrInvalidInput)
		}
		if _, err := findOCIEnvironmentSecretTx(ctx, reader, accountID, secretID, false); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: OCI environment value is unavailable for this account", ErrConflict)
			}
			return err
		}
	}
	for _, mount := range spec.VolumeMounts {
		volumeID, err := ParseID(mount.VolumeID)
		if err != nil {
			return fmt.Errorf("%w: OCI volume reference is invalid", ErrInvalidInput)
		}
		if _, err := findOCIVolumeTx(ctx, reader, accountID, volumeID, false); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: OCI volume is unavailable for this account", ErrConflict)
			}
			return err
		}
	}
	return nil
}

func replaceOCIApplicationResourceReferencesTx(
	ctx context.Context, executor store.Executor, accountID, applicationID ID, spec ociapps.Spec,
) error {
	if err := deleteOCIApplicationResourceReferencesTx(ctx, executor, applicationID); err != nil {
		return err
	}
	return insertOCIApplicationResourceReferencesTx(ctx, executor, accountID, applicationID, spec)
}

func deleteOCIApplicationResourceReferencesTx(
	ctx context.Context, executor store.Executor, applicationID ID,
) error {
	if _, err := executor.ExecContext(ctx, `DELETE FROM oci_application_secret_references WHERE application_id = ?`,
		string(applicationID)); err != nil {
		return err
	}
	_, err := executor.ExecContext(ctx, `DELETE FROM oci_application_volume_mounts WHERE application_id = ?`,
		string(applicationID))
	return err
}

func insertOCIApplicationResourceReferencesTx(
	ctx context.Context, executor store.Executor, accountID, applicationID ID, spec ociapps.Spec,
) error {
	for _, reference := range spec.SecretReferences {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO oci_application_secret_references (
				application_id, account_id, environment_name, secret_id
			) VALUES (?, ?, ?, ?)`, string(applicationID), string(accountID),
			reference.Environment, reference.SecretID); err != nil {
			return err
		}
	}
	for _, mount := range spec.VolumeMounts {
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO oci_application_volume_mounts (
				application_id, account_id, container_path, volume_id, read_only
			) VALUES (?, ?, ?, ?, ?)`, string(applicationID), string(accountID),
			mount.ContainerPath, mount.VolumeID, mount.ReadOnly); err != nil {
			return err
		}
	}
	return nil
}

func loadOCIApplicationResourceReferencesTx(
	ctx context.Context, reader store.Reader, application *OCIApplication,
) error {
	secretRows, err := reader.QueryContext(ctx, `
		SELECT secret_id, environment_name FROM oci_application_secret_references
		WHERE account_id = ? AND application_id = ? ORDER BY environment_name, secret_id`,
		string(application.AccountID), string(application.ID))
	if err != nil {
		return err
	}
	for secretRows.Next() {
		var reference ociapps.EnvironmentSecretReference
		if err := secretRows.Scan(&reference.SecretID, &reference.Environment); err != nil {
			_ = secretRows.Close()
			return err
		}
		application.Spec.SecretReferences = append(application.Spec.SecretReferences, reference)
	}
	if err := secretRows.Err(); err != nil {
		_ = secretRows.Close()
		return err
	}
	if err := secretRows.Close(); err != nil {
		return err
	}
	volumeRows, err := reader.QueryContext(ctx, `
		SELECT volume_id, container_path, read_only FROM oci_application_volume_mounts
		WHERE account_id = ? AND application_id = ? ORDER BY container_path, volume_id`,
		string(application.AccountID), string(application.ID))
	if err != nil {
		return err
	}
	defer volumeRows.Close()
	for volumeRows.Next() {
		var mount ociapps.VolumeMount
		if err := volumeRows.Scan(&mount.VolumeID, &mount.ContainerPath, &mount.ReadOnly); err != nil {
			return err
		}
		application.Spec.VolumeMounts = append(application.Spec.VolumeMounts, mount)
	}
	return volumeRows.Err()
}

func validateOCIApplicationMutationIDs(accountID, applicationID, actorID ID) error {
	if err := validateID(accountID, "accountId"); err != nil {
		return err
	}
	if err := validateID(applicationID, "applicationId"); err != nil {
		return err
	}
	return validateID(actorID, "actorId")
}

func validateOCIApplicationName(value string) (string, error) {
	name, err := validateText(value, "name", 1, 80)
	if err != nil {
		return "", err
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: OCI application name contains control characters", ErrInvalidInput)
	}
	return name, nil
}

func validOCIApplicationStatus(status OCIApplicationStatus) bool {
	return status == OCIApplicationDraft || status == OCIApplicationPending || status == OCIApplicationActive ||
		status == OCIApplicationSuspended || status == OCIApplicationError || status == OCIApplicationDeleting ||
		status == OCIApplicationDeleted
}
