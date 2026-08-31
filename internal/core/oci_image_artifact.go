// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/RTBGG/stackfort/internal/ociimage"
	"github.com/RTBGG/stackfort/internal/store"
)

// OCIImagePrepareSpec reconstructs the privileged intent from stored,
// account-owned state. No host path or Podman argument is accepted from a
// browser-facing caller.
func (r *Repository) OCIImagePrepareSpec(
	ctx context.Context, accountID, applicationID ID,
) (ociimage.PrepareSpec, error) {
	account, err := r.GetHostingAccount(ctx, accountID)
	if err != nil {
		return ociimage.PrepareSpec{}, err
	}
	if account.Status != AccountActive || account.UnixIdentity.State != HostingUnixIdentityReconciled ||
		account.UnixIdentity.OCIRuntimeReconciledAt == nil {
		return ociimage.PrepareSpec{}, fmt.Errorf("%w: account OCI runtime is not host-ready", ErrConflict)
	}
	application, err := r.GetOCIApplication(ctx, accountID, applicationID)
	if err != nil {
		return ociimage.PrepareSpec{}, err
	}
	if application.Status != OCIApplicationDraft && application.Status != OCIApplicationPending {
		return ociimage.PrepareSpec{}, fmt.Errorf("%w: application cannot prepare an image in its current state", ErrConflict)
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil {
		return ociimage.PrepareSpec{}, err
	}
	spec := ociimage.PrepareSpec{
		Identity: identity, ApplicationID: string(application.ID), Revision: application.Revision,
		Source: application.Spec.Source,
	}
	if err := ociimage.ValidateSpec(spec); err != nil {
		return ociimage.PrepareSpec{}, errors.New("stored OCI image preparation intent is invalid")
	}
	return spec, nil
}

func (r *Repository) RecordOCIImageArtifact(
	ctx context.Context, params RecordOCIImageArtifactParams,
) (OCIApplication, OCIImageArtifact, error) {
	params.Result.Reused = false
	if err := validateOCIApplicationMutationIDs(params.AccountID, params.ApplicationID, params.ActorID); err != nil {
		return OCIApplication{}, OCIImageArtifact{}, err
	}
	if params.ExpectedRevision < 1 || ociimage.ValidateResult(params.Result) != nil {
		return OCIApplication{}, OCIImageArtifact{}, fmt.Errorf("%w: OCI image result is invalid", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIApplication{}, OCIImageArtifact{}, err
	}
	now := r.timestamp()
	var application OCIApplication
	var artifact OCIImageArtifact
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var ready int
		if err := executor.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM hosting_accounts AS account
			JOIN hosting_account_unix_identities AS identity ON identity.account_id = account.id
			WHERE account.id = ? AND account.status = 'active'
			  AND identity.lifecycle_state = 'reconciled'
			  AND identity.oci_runtime_reconciled_at IS NOT NULL`, string(params.AccountID)).Scan(&ready); err != nil {
			return err
		}
		if ready != 1 {
			return fmt.Errorf("%w: account OCI runtime is not host-ready", ErrConflict)
		}
		current, err := findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		if err != nil {
			return err
		}
		if current.Revision != params.ExpectedRevision ||
			(current.Status != OCIApplicationDraft && current.Status != OCIApplicationPending) {
			return fmt.Errorf("%w: OCI application revision or state changed", ErrConflict)
		}
		if err := validateArtifactSource(current.Spec.Source, params.Result); err != nil {
			return err
		}
		if current.Status == OCIApplicationPending {
			existing, err := findOCIImageArtifactTx(ctx, executor, params.AccountID, params.ApplicationID, params.ExpectedRevision)
			if err != nil || existing.Result != params.Result {
				return fmt.Errorf("%w: OCI image artifact differs from the recorded result", ErrConflict)
			}
			application, artifact = current, existing
			return nil
		}
		artifact = OCIImageArtifact{
			ApplicationID: current.ID, AccountID: current.AccountID, ApplicationRevision: current.Revision,
			Result: params.Result, PreparedAt: now, PreparedByIdentityID: params.ActorID,
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO oci_image_artifacts (
				application_id, account_id, application_revision, image_digest, source_digest,
				policy_version, scanner_provider, scanner_version,
				unknown_vulnerabilities, low_vulnerabilities, medium_vulnerabilities,
				high_vulnerabilities, critical_vulnerabilities, prepared_at, prepared_by_identity_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(artifact.ApplicationID), string(artifact.AccountID), artifact.ApplicationRevision,
			artifact.Result.ImageDigest, artifact.Result.SourceDigest, artifact.Result.PolicyVersion,
			artifact.Result.ScannerProvider, artifact.Result.ScannerVersion,
			artifact.Result.Vulnerabilities.Unknown, artifact.Result.Vulnerabilities.Low,
			artifact.Result.Vulnerabilities.Medium, artifact.Result.Vulnerabilities.High,
			artifact.Result.Vulnerabilities.Critical, formatTime(now), string(params.ActorID))
		if err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE oci_applications SET status = 'pending', updated_at = ?
			WHERE account_id = ? AND id = ? AND status = 'draft' AND revision = ?`,
			formatTime(now), string(params.AccountID), string(params.ApplicationID), params.ExpectedRevision)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: OCI application changed concurrently", ErrConflict)
		}
		application, err = findOCIApplicationTx(ctx, executor, params.AccountID, params.ApplicationID, false)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_application.image_prepared",
			TargetType: "oci_application", TargetID: string(params.ApplicationID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{
				"revision": params.ExpectedRevision, "imageDigest": params.Result.ImageDigest,
				"sourceDigest": params.Result.SourceDigest, "policyVersion": params.Result.PolicyVersion,
				"scannerProvider": params.Result.ScannerProvider, "scannerVersion": params.Result.ScannerVersion,
			},
		}, now)
	})
	if err != nil {
		return OCIApplication{}, OCIImageArtifact{}, classifyDatabaseError(err)
	}
	return application, artifact, nil
}

func (r *Repository) GetOCIImageArtifact(
	ctx context.Context, accountID, applicationID ID, revision int64,
) (OCIImageArtifact, error) {
	if validateID(accountID, "accountId") != nil || validateID(applicationID, "applicationId") != nil || revision < 1 {
		return OCIImageArtifact{}, fmt.Errorf("%w: OCI image artifact key is invalid", ErrInvalidInput)
	}
	var artifact OCIImageArtifact
	err := r.state.Read(ctx, func(reader store.Reader) error {
		var err error
		artifact, err = findOCIImageArtifactTx(ctx, reader, accountID, applicationID, revision)
		return err
	})
	return artifact, classifyDatabaseError(err)
}

func validateArtifactSource(source ociapps.Source, result ociimage.Result) error {
	switch source.Kind {
	case ociapps.SourceImageDigest:
		digest, err := ociimage.RequestedDigest(source.ImageReference)
		if err != nil || result.SourceDigest != digest {
			return fmt.Errorf("%w: pulled source digest differs from application intent", ErrConflict)
		}
	case ociapps.SourceContainerfile:
		if !ociimage.ValidDigest(result.SourceDigest) {
			return fmt.Errorf("%w: build artifact lacks an immutable context digest", ErrConflict)
		}
	default:
		return fmt.Errorf("%w: unsupported OCI source", ErrConflict)
	}
	return nil
}

func findOCIImageArtifactTx(
	ctx context.Context, reader store.Reader, accountID, applicationID ID, revision int64,
) (OCIImageArtifact, error) {
	var artifact OCIImageArtifact
	var sourceDigest string
	var preparedAt string
	err := reader.QueryRowContext(ctx, `
		SELECT application_id, account_id, application_revision, image_digest, source_digest,
		       policy_version, scanner_provider, scanner_version,
		       unknown_vulnerabilities, low_vulnerabilities, medium_vulnerabilities,
		       high_vulnerabilities, critical_vulnerabilities, prepared_at, prepared_by_identity_id
		FROM oci_image_artifacts
		WHERE account_id = ? AND application_id = ? AND application_revision = ?`,
		string(accountID), string(applicationID), revision).Scan(
		&artifact.ApplicationID, &artifact.AccountID, &artifact.ApplicationRevision,
		&artifact.Result.ImageDigest, &sourceDigest, &artifact.Result.PolicyVersion,
		&artifact.Result.ScannerProvider, &artifact.Result.ScannerVersion,
		&artifact.Result.Vulnerabilities.Unknown, &artifact.Result.Vulnerabilities.Low,
		&artifact.Result.Vulnerabilities.Medium, &artifact.Result.Vulnerabilities.High,
		&artifact.Result.Vulnerabilities.Critical, &preparedAt, &artifact.PreparedByIdentityID,
	)
	if err != nil {
		return OCIImageArtifact{}, err
	}
	artifact.Result.SourceDigest = sourceDigest
	if artifact.PreparedAt, err = parseTime(preparedAt); err != nil || ociimage.ValidateResult(artifact.Result) != nil {
		return OCIImageArtifact{}, errors.New("stored OCI image artifact is invalid")
	}
	return artifact, nil
}
