// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociresources"
	"github.com/RTBGG/stackfort/internal/store"
)

// OCIResourcePrepareSpec reconstructs a metadata-only host intent from the
// image-approved application revision. Environment plaintext never crosses
// this boundary.
func (r *Repository) OCIResourcePrepareSpec(
	ctx context.Context, accountID, applicationID ID,
) (ociresources.Spec, error) {
	if validateID(accountID, "accountId") != nil || validateID(applicationID, "applicationId") != nil {
		return ociresources.Spec{}, ErrInvalidInput
	}
	var spec ociresources.Spec
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		spec, err = ociResourcePrepareSpecTx(ctx, reader, accountID, applicationID)
		return err
	})
	return spec, classifyDatabaseError(err)
}

func (r *Repository) RecordOCIResourceArtifact(
	ctx context.Context, params RecordOCIResourceArtifactParams,
) (OCIApplication, OCIResourceArtifact, error) {
	params.Result.Changed, params.Result.Reused = false, false
	if err := validateOCIApplicationMutationIDs(params.AccountID, params.ApplicationID, params.ActorID); err != nil {
		return OCIApplication{}, OCIResourceArtifact{}, err
	}
	if params.ExpectedRevision < 1 || ociresources.ValidateResult(params.Result) != nil {
		return OCIApplication{}, OCIResourceArtifact{}, fmt.Errorf("%w: OCI private-resource result is invalid", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIApplication{}, OCIResourceArtifact{}, err
	}
	now := r.timestamp()
	var application OCIApplication
	var artifact OCIResourceArtifact
	err = r.state.Write(ctx, func(executor store.Executor) error {
		spec, err := ociResourcePrepareSpecTx(ctx, executor, params.AccountID, params.ApplicationID)
		if err != nil {
			return err
		}
		if spec.Revision != params.ExpectedRevision {
			return fmt.Errorf("%w: OCI application revision changed", ErrConflict)
		}
		expected, err := ociresources.ResultFor(spec, false)
		if err != nil || expected != params.Result {
			return fmt.Errorf("%w: OCI private-resource evidence differs from stored intent", ErrConflict)
		}
		application, err = findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		if err != nil {
			return err
		}
		if existing, findErr := findOCIResourceArtifactTx(
			ctx, executor, params.AccountID, params.ApplicationID, params.ExpectedRevision,
			params.Result.ResourceDigest,
		); findErr == nil {
			if existing.Result != params.Result {
				return fmt.Errorf("%w: OCI private-resource artifact differs from recorded result", ErrConflict)
			}
			artifact = existing
			return nil
		} else if !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		artifact = OCIResourceArtifact{
			ApplicationID: application.ID, AccountID: application.AccountID,
			ApplicationRevision: application.Revision, Result: params.Result,
			PreparedAt: now, PreparedByIdentityID: params.ActorID,
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO oci_resource_artifacts (
				application_id, account_id, application_revision, resource_digest,
				policy_version, network_name, secret_count, volume_count,
				prepared_at, prepared_by_identity_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(artifact.ApplicationID), string(artifact.AccountID), artifact.ApplicationRevision,
			artifact.Result.ResourceDigest, artifact.Result.PolicyVersion, artifact.Result.NetworkName,
			artifact.Result.EnvironmentReferenceCount, artifact.Result.VolumeCount, formatTime(now), string(params.ActorID)); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_application.resources_prepared",
			TargetType: "oci_application", TargetID: string(params.ApplicationID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{
				"revision": params.ExpectedRevision, "resourceDigest": params.Result.ResourceDigest,
				"policyVersion": params.Result.PolicyVersion, "networkName": params.Result.NetworkName,
				"environmentReferenceCount": params.Result.EnvironmentReferenceCount, "volumeCount": params.Result.VolumeCount,
			},
		}, now)
	})
	if err != nil {
		return OCIApplication{}, OCIResourceArtifact{}, classifyDatabaseError(err)
	}
	return application, artifact, nil
}

func (r *Repository) GetOCIResourceArtifact(
	ctx context.Context, accountID, applicationID ID, revision int64, resourceDigest string,
) (OCIResourceArtifact, error) {
	if validateID(accountID, "accountId") != nil || validateID(applicationID, "applicationId") != nil ||
		revision < 1 || ociresources.ValidateResult(ociresources.Result{
		ResourceDigest: resourceDigest, PolicyVersion: ociresources.PolicyVersion,
		NetworkName: ociresources.NetworkName,
	}) != nil {
		return OCIResourceArtifact{}, fmt.Errorf("%w: OCI private-resource artifact key is invalid", ErrInvalidInput)
	}
	var artifact OCIResourceArtifact
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		artifact, err = findOCIResourceArtifactTx(ctx, reader, accountID, applicationID, revision, resourceDigest)
		return err
	})
	return artifact, classifyDatabaseError(err)
}

func ociResourcePrepareSpecTx(
	ctx context.Context, reader store.Reader, accountID, applicationID ID,
) (ociresources.Spec, error) {
	var identity HostingUnixIdentity
	var accountStatus, identityState string
	var uid, gid int64
	var ociRuntimeReconciledAt sql.NullString
	err := reader.QueryRowContext(ctx, `
		SELECT account.status, identity.account_id, identity.username, identity.uid, identity.gid,
		       identity.home_directory, identity.lifecycle_state, identity.oci_runtime_reconciled_at
		FROM hosting_accounts AS account
		JOIN hosting_account_unix_identities AS identity ON identity.account_id = account.id
		WHERE account.id = ?`, string(accountID)).Scan(
		&accountStatus, &identity.AccountID, &identity.Username, &uid, &gid,
		&identity.HomeDirectory, &identityState, &ociRuntimeReconciledAt,
	)
	if err != nil {
		return ociresources.Spec{}, err
	}
	identity.UID, err = hostingUnixNumericID(uid)
	if err != nil {
		return ociresources.Spec{}, err
	}
	identity.GID, err = hostingUnixNumericID(gid)
	if err != nil || identity.GID != identity.UID || AccountStatus(accountStatus) != AccountActive ||
		HostingUnixIdentityState(identityState) != HostingUnixIdentityReconciled || !ociRuntimeReconciledAt.Valid {
		return ociresources.Spec{}, fmt.Errorf("%w: account OCI runtime is not host-ready", ErrConflict)
	}
	hostIdentity, err := identity.HostSpec()
	if err != nil {
		return ociresources.Spec{}, err
	}
	application, err := findOCIApplicationTx(ctx, reader, accountID, applicationID, false)
	if err != nil {
		return ociresources.Spec{}, err
	}
	if application.Status != OCIApplicationPending {
		return ociresources.Spec{}, fmt.Errorf("%w: application image is not approved", ErrConflict)
	}
	if _, err := findOCIImageArtifactTx(ctx, reader, accountID, applicationID, application.Revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ociresources.Spec{}, fmt.Errorf("%w: application image evidence is missing", ErrConflict)
		}
		return ociresources.Spec{}, err
	}
	spec := ociresources.Spec{
		Identity: hostIdentity, ApplicationID: string(application.ID), Revision: application.Revision,
		Volumes: append([]ociapps.VolumeMount(nil), application.Spec.VolumeMounts...),
	}
	for _, reference := range application.Spec.SecretReferences {
		secretID, parseErr := ParseID(reference.SecretID)
		if parseErr != nil {
			return ociresources.Spec{}, errors.New("stored OCI environment reference is invalid")
		}
		secret, findErr := findOCIEnvironmentSecretTx(ctx, reader, accountID, secretID, false)
		if findErr != nil {
			return ociresources.Spec{}, findErr
		}
		spec.EnvironmentReferences = append(spec.EnvironmentReferences, ociresources.EnvironmentReference{
			SecretID: reference.SecretID, Environment: reference.Environment, Generation: secret.Generation,
		})
	}
	normalized, err := ociresources.Normalize(spec)
	if err != nil {
		return ociresources.Spec{}, errors.New("stored OCI private-resource intent is invalid")
	}
	return normalized, nil
}

func findOCIResourceArtifactTx(
	ctx context.Context, reader store.Reader, accountID, applicationID ID, revision int64, resourceDigest string,
) (OCIResourceArtifact, error) {
	var artifact OCIResourceArtifact
	var preparedAt string
	err := reader.QueryRowContext(ctx, `
		SELECT application_id, account_id, application_revision, resource_digest,
		       policy_version, network_name, secret_count, volume_count,
		       prepared_at, prepared_by_identity_id
		FROM oci_resource_artifacts
		WHERE account_id = ? AND application_id = ? AND application_revision = ? AND resource_digest = ?`,
		string(accountID), string(applicationID), revision, resourceDigest).Scan(
		&artifact.ApplicationID, &artifact.AccountID, &artifact.ApplicationRevision,
		&artifact.Result.ResourceDigest, &artifact.Result.PolicyVersion, &artifact.Result.NetworkName,
		&artifact.Result.EnvironmentReferenceCount, &artifact.Result.VolumeCount,
		&preparedAt, &artifact.PreparedByIdentityID,
	)
	if err != nil {
		return OCIResourceArtifact{}, err
	}
	if artifact.PreparedAt, err = parseTime(preparedAt); err != nil || ociresources.ValidateResult(artifact.Result) != nil {
		return OCIResourceArtifact{}, errors.New("stored OCI private-resource artifact is invalid")
	}
	return artifact, nil
}
