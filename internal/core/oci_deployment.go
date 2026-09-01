// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ocideployment"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/store"
)

// AllocateOCIDeploymentSpec returns immutable, plaintext-free deployment
// intent and atomically reserves a stable loopback-only port. The caller can
// persist this value safely in a durable operation.
func (r *Repository) AllocateOCIDeploymentSpec(
	ctx context.Context, accountID, applicationID ID,
) (ocideployment.Spec, error) {
	if validateID(accountID, "accountId") != nil || validateID(applicationID, "applicationId") != nil {
		return ocideployment.Spec{}, ErrInvalidInput
	}
	var result ocideployment.Spec
	err := r.state.Write(ctx, func(executor store.Executor) error {
		port, err := allocateOCILoopbackPortTx(ctx, executor, accountID, applicationID, r.timestamp())
		if err != nil {
			return err
		}
		result, err = ociDeploymentSpecTx(ctx, executor, accountID, applicationID, port,
			[]OCIApplicationStatus{OCIApplicationPending})
		return err
	})
	return result, classifyDatabaseError(err)
}

// CurrentOCIDeploymentSpec reconstructs the host intent for lifecycle replay.
// It never contains environment plaintext.
func (r *Repository) CurrentOCIDeploymentSpec(
	ctx context.Context, accountID, applicationID ID,
) (ocideployment.Spec, error) {
	if validateID(accountID, "accountId") != nil || validateID(applicationID, "applicationId") != nil {
		return ocideployment.Spec{}, ErrInvalidInput
	}
	var result ocideployment.Spec
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var port int64
		if err := reader.QueryRowContext(ctx, `
			SELECT loopback_port FROM oci_deployment_allocations
			WHERE account_id = ? AND application_id = ?`, string(accountID), string(applicationID)).Scan(&port); err != nil {
			return err
		}
		var err error
		result, err = ociDeploymentSpecTx(ctx, reader, accountID, applicationID, port,
			[]OCIApplicationStatus{OCIApplicationPending, OCIApplicationActive, OCIApplicationSuspended, OCIApplicationError, OCIApplicationDeleted})
		return err
	})
	return result, classifyDatabaseError(err)
}

// LoadOCIDeploymentValues is the narrow plaintext boundary used immediately
// before a local agent call. Values are revision/generation fenced and callers
// are responsible for clearing the returned strings as soon as the call ends.
func (r *Repository) LoadOCIDeploymentValues(
	ctx context.Context, accountID, applicationID ID, expected ocideployment.Spec,
) ([]ocideployment.EnvironmentValue, error) {
	current, err := r.CurrentOCIDeploymentSpec(ctx, accountID, applicationID)
	if err != nil {
		return nil, err
	}
	currentDigest, currentErr := ocideployment.SemanticDigest(current)
	expectedDigest, expectedErr := ocideployment.SemanticDigest(expected)
	if currentErr != nil || expectedErr != nil || currentDigest != expectedDigest {
		return nil, fmt.Errorf("%w: OCI deployment revision changed", ErrConflict)
	}
	values := make([]ocideployment.EnvironmentValue, 0, len(expected.EnvironmentReferences))
	for _, reference := range expected.EnvironmentReferences {
		secretID, parseErr := ParseID(reference.ValueID)
		if parseErr != nil {
			clearOCIDeploymentValues(values)
			return nil, ErrInvalidInput
		}
		value, generation, loadErr := r.LoadOCIEnvironmentSecretValue(ctx, accountID, secretID)
		if loadErr != nil || generation != reference.Generation {
			clear(value)
			clearOCIDeploymentValues(values)
			if loadErr != nil {
				return nil, loadErr
			}
			return nil, fmt.Errorf("%w: OCI environment generation changed", ErrConflict)
		}
		values = append(values, ocideployment.EnvironmentValue{
			ValueID: reference.ValueID, Environment: reference.Environment,
			Generation: generation, Value: string(value),
		})
		clear(value)
	}
	if ocideployment.ValidateValues(expected, values) != nil {
		clearOCIDeploymentValues(values)
		return nil, errors.New("stored OCI deployment values are invalid")
	}
	return values, nil
}

func ClearOCIDeploymentValues(values []ocideployment.EnvironmentValue) {
	clearOCIDeploymentValues(values)
}

func clearOCIDeploymentValues(values []ocideployment.EnvironmentValue) {
	for index := range values {
		values[index].Value = ""
	}
	clear(values)
}

func (r *Repository) RecordOCIDeploymentArtifact(
	ctx context.Context, params RecordOCIDeploymentArtifactParams,
) (OCIApplication, OCIDeploymentArtifact, error) {
	params.Result.Changed, params.Result.Reused = false, false
	if err := validateOCIApplicationMutationIDs(params.AccountID, params.ApplicationID, params.ActorID); err != nil ||
		params.ExpectedRevision < 1 || ocideployment.ValidateResult(params.Result) != nil {
		return OCIApplication{}, OCIDeploymentArtifact{}, ErrInvalidInput
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIApplication{}, OCIDeploymentArtifact{}, err
	}
	now := r.timestamp()
	var application OCIApplication
	var artifact OCIDeploymentArtifact
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var port int64
		if err := executor.QueryRowContext(ctx, `SELECT loopback_port FROM oci_deployment_allocations
			WHERE account_id = ? AND application_id = ?`, string(params.AccountID), string(params.ApplicationID)).Scan(&port); err != nil {
			return err
		}
		spec, err := ociDeploymentSpecTx(ctx, executor, params.AccountID, params.ApplicationID, port,
			[]OCIApplicationStatus{OCIApplicationPending, OCIApplicationActive})
		if err != nil {
			return err
		}
		if spec.Revision != params.ExpectedRevision {
			return fmt.Errorf("%w: OCI application revision changed", ErrConflict)
		}
		expected, err := ocideployment.ResultFor(spec, false)
		if err != nil || expected != params.Result {
			return fmt.Errorf("%w: OCI deployment evidence differs from stored intent", ErrConflict)
		}
		application, err = findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		if err != nil {
			return err
		}
		if existing, findErr := findOCIDeploymentArtifactTx(ctx, executor, params.AccountID,
			params.ApplicationID, params.ExpectedRevision, params.Result.DeploymentDigest); findErr == nil {
			artifact = existing
			if application.Status != OCIApplicationActive || application.AppliedRevision == nil ||
				*application.AppliedRevision != application.Revision {
				return fmt.Errorf("%w: OCI deployment evidence has inconsistent application state", ErrConflict)
			}
			return nil
		} else if !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		if application.Status != OCIApplicationPending {
			return fmt.Errorf("%w: application is not deployable", ErrConflict)
		}
		if _, err := executor.ExecContext(ctx, `UPDATE oci_applications
			SET status = 'active', applied_revision = revision, updated_at = ?
			WHERE account_id = ? AND id = ? AND status = 'pending' AND revision = ?`,
			formatTime(now), string(params.AccountID), string(params.ApplicationID), params.ExpectedRevision); err != nil {
			return err
		}
		artifact = OCIDeploymentArtifact{ApplicationID: params.ApplicationID, AccountID: params.AccountID,
			ApplicationRevision: params.ExpectedRevision, Result: params.Result, DeployedAt: now,
			DeployedByIdentityID: params.ActorID}
		if _, err := executor.ExecContext(ctx, `INSERT INTO oci_deployment_artifacts (
			application_id, account_id, application_revision, deployment_digest, quadlet_digest,
			policy_version, unit_name, loopback_port, healthy, active, deployed_at, deployed_by_identity_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 1, ?, ?)`, string(params.ApplicationID), string(params.AccountID),
			params.ExpectedRevision, params.Result.DeploymentDigest, params.Result.QuadletDigest,
			params.Result.PolicyVersion, params.Result.UnitName, params.Result.LoopbackPort,
			formatTime(now), string(params.ActorID)); err != nil {
			return err
		}
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{ActorID: &params.ActorID,
			Action: "oci_application.deployed", TargetType: "oci_application", TargetID: string(params.ApplicationID),
			AccountID: &params.AccountID, RequestID: requestID, Result: AuditSuccess, Details: map[string]any{
				"revision": params.ExpectedRevision, "deploymentDigest": params.Result.DeploymentDigest,
				"quadletDigest": params.Result.QuadletDigest, "loopbackPort": params.Result.LoopbackPort,
			}}, now); err != nil {
			return err
		}
		application, err = findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		return err
	})
	if err != nil {
		return OCIApplication{}, OCIDeploymentArtifact{}, classifyDatabaseError(err)
	}
	return application, artifact, nil
}

func (r *Repository) ChangeOCIApplicationDeploymentStatus(
	ctx context.Context, params ChangeOCIApplicationDeploymentStatusParams,
) (OCIApplication, error) {
	if err := validateOCIApplicationMutationIDs(params.AccountID, params.ApplicationID, params.ActorID); err != nil ||
		validateID(params.OperationID, "operationId") != nil || !validDeploymentTransition(params.Expected, params.Status) {
		return OCIApplication{}, ErrInvalidInput
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIApplication{}, err
	}
	now := r.timestamp()
	var application OCIApplication
	err = r.state.Write(ctx, func(executor store.Executor) error {
		current, err := findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, true)
		if err != nil {
			return err
		}
		if current.Status == params.Status {
			application = current
			return nil
		}
		if current.Status != params.Expected {
			return fmt.Errorf("%w: OCI application lifecycle changed", ErrConflict)
		}
		removedAt := any(nil)
		if params.Status == OCIApplicationDeleted {
			removedAt = formatTime(now)
		}
		if _, err := executor.ExecContext(ctx, `UPDATE oci_applications SET status = ?, updated_at = ?, removed_at = ?
			WHERE account_id = ? AND id = ? AND status = ?`, string(params.Status), formatTime(now), removedAt,
			string(params.AccountID), string(params.ApplicationID), string(params.Expected)); err != nil {
			return err
		}
		action := "oci_application." + string(params.Status)
		if err := r.appendAuditTx(ctx, executor, AppendAuditEventParams{ActorID: &params.ActorID,
			Action: action, TargetType: "oci_application", TargetID: string(params.ApplicationID),
			AccountID: &params.AccountID, RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"operationId": params.OperationID, "from": params.Expected, "to": params.Status}}, now); err != nil {
			return err
		}
		application, err = findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID,
			params.Status == OCIApplicationDeleted)
		return err
	})
	return application, classifyDatabaseError(err)
}

func (r *Repository) EnsureOCIApplicationRemovable(ctx context.Context, accountID, applicationID ID) error {
	if validateID(accountID, "accountId") != nil || validateID(applicationID, "applicationId") != nil {
		return ErrInvalidInput
	}
	var references int64
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM domain_targets
			WHERE account_id = ? AND application_id = ? AND superseded_at IS NULL`,
			string(accountID), string(applicationID)).Scan(&references)
	})
	if err != nil {
		return classifyDatabaseError(err)
	}
	if references != 0 {
		return fmt.Errorf("%w: OCI application is still routed by a domain", ErrConflict)
	}
	return nil
}

func (r *Repository) ListOCIApplicationUpstreams(ctx context.Context, accountID ID) ([]OCIApplicationUpstream, error) {
	if validateID(accountID, "accountId") != nil {
		return nil, ErrInvalidInput
	}
	result := []OCIApplicationUpstream{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `SELECT application.id, allocation.loopback_port
			FROM oci_applications AS application
			JOIN oci_deployment_allocations AS allocation ON allocation.application_id = application.id
				AND allocation.account_id = application.account_id
			WHERE application.account_id = ? AND application.status = 'active'
				AND application.applied_revision = application.revision AND application.removed_at IS NULL
			ORDER BY application.id`, string(accountID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var upstream OCIApplicationUpstream
			if err := rows.Scan(&upstream.ApplicationID, &upstream.LoopbackPort); err != nil {
				return err
			}
			if validateID(upstream.ApplicationID, "applicationId") != nil ||
				upstream.LoopbackPort < ocideployment.MinimumLoopbackPort ||
				upstream.LoopbackPort > ocideployment.MaximumLoopbackPort {
				return errors.New("stored OCI application upstream is invalid")
			}
			result = append(result, upstream)
		}
		return rows.Err()
	})
	return result, classifyDatabaseError(err)
}

func validDeploymentTransition(from, to OCIApplicationStatus) bool {
	return from == OCIApplicationActive && to == OCIApplicationSuspended ||
		from == OCIApplicationSuspended && to == OCIApplicationActive ||
		(from == OCIApplicationActive || from == OCIApplicationSuspended || from == OCIApplicationError) &&
			to == OCIApplicationDeleted
}

func ociDeploymentSpecTx(ctx context.Context, reader store.Reader, accountID, applicationID ID,
	port int64, allowed []OCIApplicationStatus) (ocideployment.Spec, error) {
	application, err := findOCIApplicationTx(ctx, reader, accountID, applicationID,
		slices.Contains(allowed, OCIApplicationDeleted))
	if err != nil {
		return ocideployment.Spec{}, err
	}
	if !slices.Contains(allowed, application.Status) {
		return ocideployment.Spec{}, fmt.Errorf("%w: application state is not deployable", ErrConflict)
	}
	var identity HostingUnixIdentity
	var accountStatus, identityState string
	var uid, gid int64
	var reconciled sql.NullString
	if err := reader.QueryRowContext(ctx, `SELECT account.status, identity.account_id, identity.username,
		identity.uid, identity.gid, identity.home_directory, identity.lifecycle_state,
		identity.oci_runtime_reconciled_at FROM hosting_accounts AS account
		JOIN hosting_account_unix_identities AS identity ON identity.account_id = account.id
		WHERE account.id = ?`, string(accountID)).Scan(&accountStatus, &identity.AccountID, &identity.Username,
		&uid, &gid, &identity.HomeDirectory, &identityState, &reconciled); err != nil {
		return ocideployment.Spec{}, err
	}
	identity.UID, err = hostingUnixNumericID(uid)
	if err != nil {
		return ocideployment.Spec{}, err
	}
	identity.GID, err = hostingUnixNumericID(gid)
	if err != nil || identity.UID != identity.GID || AccountStatus(accountStatus) != AccountActive ||
		HostingUnixIdentityState(identityState) != HostingUnixIdentityReconciled || !reconciled.Valid {
		return ocideployment.Spec{}, fmt.Errorf("%w: account OCI runtime is not host-ready", ErrConflict)
	}
	hostIdentity, err := identity.HostSpec()
	if err != nil {
		return ocideployment.Spec{}, err
	}
	image, err := findOCIImageArtifactTx(ctx, reader, accountID, applicationID, application.Revision)
	if err != nil {
		return ocideployment.Spec{}, fmt.Errorf("%w: OCI image evidence is missing", ErrConflict)
	}
	resourceSpec, err := deploymentResourceSpecTx(ctx, reader, application, hostIdentity)
	if err != nil {
		return ocideployment.Spec{}, err
	}
	resourceDigest, err := ociresources.SemanticDigest(resourceSpec)
	if err != nil {
		return ocideployment.Spec{}, err
	}
	if _, err := findOCIResourceArtifactTx(ctx, reader, accountID, applicationID,
		application.Revision, resourceDigest); err != nil {
		return ocideployment.Spec{}, fmt.Errorf("%w: OCI private-resource evidence is missing", ErrConflict)
	}
	spec, err := ocideployment.Normalize(ocideployment.Spec{Identity: hostIdentity,
		ApplicationID: string(application.ID), Revision: application.Revision,
		ImageDigest: image.Result.ImageDigest, ResourceDigest: resourceDigest,
		InternalPort: application.Spec.InternalPort, LoopbackPort: port, Health: application.Spec.Health,
		EnvironmentReferences: ocideployment.ReferencesFromResources(resourceSpec),
		Volumes:               append([]ociapps.VolumeMount(nil), application.Spec.VolumeMounts...)})
	if err != nil {
		return ocideployment.Spec{}, errors.New("stored OCI deployment intent is invalid")
	}
	return spec, nil
}

func deploymentResourceSpecTx(ctx context.Context, reader store.Reader, application OCIApplication,
	identity hostingidentity.Spec) (ociresources.Spec, error) {
	spec := ociresources.Spec{Identity: identity, ApplicationID: string(application.ID),
		Revision: application.Revision, Volumes: append([]ociapps.VolumeMount(nil), application.Spec.VolumeMounts...)}
	for _, reference := range application.Spec.SecretReferences {
		secretID, err := ParseID(reference.SecretID)
		if err != nil {
			return ociresources.Spec{}, errors.New("stored OCI environment reference is invalid")
		}
		secret, err := findOCIEnvironmentSecretTx(ctx, reader, application.AccountID, secretID, false)
		if err != nil {
			return ociresources.Spec{}, err
		}
		spec.EnvironmentReferences = append(spec.EnvironmentReferences, ociresources.EnvironmentReference{
			SecretID: reference.SecretID, Environment: reference.Environment, Generation: secret.Generation})
	}
	return ociresources.Normalize(spec)
}

func allocateOCILoopbackPortTx(ctx context.Context, executor store.Executor, accountID, applicationID ID,
	now time.Time) (int64, error) {
	var existing int64
	if err := executor.QueryRowContext(ctx, `SELECT loopback_port FROM oci_deployment_allocations
		WHERE account_id = ? AND application_id = ?`, string(accountID), string(applicationID)).Scan(&existing); err == nil {
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	span := int64(ocideployment.MaximumLoopbackPort - ocideployment.MinimumLoopbackPort + 1)
	digest := sha256.Sum256([]byte(applicationID))
	start := int64(binary.BigEndian.Uint32(digest[:4])) % span
	for offset := int64(0); offset < span; offset++ {
		port := int64(ocideployment.MinimumLoopbackPort) + (start+offset)%span
		if _, err := executor.ExecContext(ctx, `INSERT OR IGNORE INTO oci_deployment_allocations
			(application_id, account_id, loopback_port, allocated_at) VALUES (?, ?, ?, ?)`,
			string(applicationID), string(accountID), port, formatTime(now)); err != nil {
			return 0, err
		}
		if err := executor.QueryRowContext(ctx, `SELECT loopback_port FROM oci_deployment_allocations
			WHERE account_id = ? AND application_id = ?`, string(accountID), string(applicationID)).Scan(&existing); err == nil {
			return existing, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	return 0, fmt.Errorf("%w: OCI loopback port range exhausted", ErrConflict)
}

func findOCIDeploymentArtifactTx(ctx context.Context, reader store.Reader, accountID, applicationID ID,
	revision int64, deploymentDigest string) (OCIDeploymentArtifact, error) {
	var artifact OCIDeploymentArtifact
	var deployedAt string
	var healthy, active int64
	err := reader.QueryRowContext(ctx, `SELECT application_id, account_id, application_revision,
		deployment_digest, quadlet_digest, policy_version, unit_name, loopback_port,
		healthy, active, deployed_at, deployed_by_identity_id FROM oci_deployment_artifacts
		WHERE account_id = ? AND application_id = ? AND application_revision = ? AND deployment_digest = ?`,
		string(accountID), string(applicationID), revision, deploymentDigest).Scan(&artifact.ApplicationID,
		&artifact.AccountID, &artifact.ApplicationRevision, &artifact.Result.DeploymentDigest,
		&artifact.Result.QuadletDigest, &artifact.Result.PolicyVersion, &artifact.Result.UnitName,
		&artifact.Result.LoopbackPort, &healthy, &active, &deployedAt, &artifact.DeployedByIdentityID)
	if err != nil {
		return OCIDeploymentArtifact{}, err
	}
	artifact.Result.Healthy, artifact.Result.Active = healthy == 1, active == 1
	artifact.DeployedAt, err = parseTime(deployedAt)
	if err != nil || ocideployment.ValidateResult(artifact.Result) != nil {
		return OCIDeploymentArtifact{}, errors.New("stored OCI deployment artifact is invalid")
	}
	return artifact, nil
}
