// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/store"
)

const (
	ociEnvironmentValueEnvelopeKind        = "oci-environment-value"
	maximumOCIEnvironmentValueBytes        = 32 << 10
	maximumOCIEnvironmentSecretsPerAccount = 128
	maximumOCIVolumesPerAccount            = 64
)

func (r *Repository) CreateOCIEnvironmentSecret(
	ctx context.Context, params CreateOCIEnvironmentSecretParams,
) (OCIEnvironmentSecret, error) {
	if err := validateOCIResourceMutationIDs(params.AccountID, params.ActorID); err != nil {
		return OCIEnvironmentSecret{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	name, err := validateOCIApplicationName(params.Name)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	slug, err := validateSlug(params.Slug)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	value, err := normalizeOCIEnvironmentValue(params.Value)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	defer clear(value)
	id, err := r.newID()
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	envelope, err := r.encryptSecret(ociEnvironmentValueEnvelopeKind, id, params.AccountID, value)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	now := r.timestamp()
	result := OCIEnvironmentSecret{
		ID: id, AccountID: params.AccountID, Name: name, Slug: slug,
		Generation: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if err := requireOCIResourceFeatureTx(ctx, executor, params.AccountID); err != nil {
			return err
		}
		var count int64
		if err := executor.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM oci_environment_secrets
			WHERE account_id = ? AND removed_at IS NULL`, string(params.AccountID)).Scan(&count); err != nil {
			return err
		}
		if count >= maximumOCIEnvironmentSecretsPerAccount {
			return fmt.Errorf("%w: account OCI environment value limit reached", ErrConflict)
		}
		_, err := executor.ExecContext(ctx, `
			INSERT INTO oci_environment_secrets (
				id, account_id, name, slug, generation,
				value_ciphertext, value_nonce, value_wrapped_key, value_wrap_nonce, value_key_version,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
			string(id), string(params.AccountID), name, slug, envelope.Ciphertext, envelope.Nonce,
			envelope.WrappedKey, envelope.WrapNonce, envelope.KeyVersion, formatTime(now), formatTime(now))
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_environment_value.created",
			TargetType: "oci_environment_value", TargetID: string(id), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{"generation": 1},
		}, now)
	})
	if err != nil {
		return OCIEnvironmentSecret{}, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) RotateOCIEnvironmentSecret(
	ctx context.Context, params RotateOCIEnvironmentSecretParams,
) (OCIEnvironmentSecret, error) {
	if err := validateOCIResourceRecordMutationIDs(params.AccountID, params.SecretID, params.ActorID); err != nil {
		return OCIEnvironmentSecret{}, err
	}
	if params.ExpectedGeneration < 1 {
		return OCIEnvironmentSecret{}, fmt.Errorf("%w: expectedGeneration must be positive", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	value, err := normalizeOCIEnvironmentValue(params.Value)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	defer clear(value)
	envelope, err := r.encryptSecret(
		ociEnvironmentValueEnvelopeKind, params.SecretID, params.AccountID, value,
	)
	if err != nil {
		return OCIEnvironmentSecret{}, err
	}
	now := r.timestamp()
	var result OCIEnvironmentSecret
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if err := requireOCIResourceFeatureTx(ctx, executor, params.AccountID); err != nil {
			return err
		}
		current, err := findOCIEnvironmentSecretTx(ctx, executor, params.AccountID, params.SecretID, false)
		if err != nil {
			return err
		}
		if current.Generation != params.ExpectedGeneration {
			return fmt.Errorf("%w: OCI environment value generation changed", ErrConflict)
		}
		nextGeneration := current.Generation + 1
		update, err := executor.ExecContext(ctx, `
			UPDATE oci_environment_secrets
			SET generation = ?, value_ciphertext = ?, value_nonce = ?, value_wrapped_key = ?,
			    value_wrap_nonce = ?, value_key_version = ?, updated_at = ?
			WHERE account_id = ? AND id = ? AND generation = ? AND removed_at IS NULL`,
			nextGeneration, envelope.Ciphertext, envelope.Nonce, envelope.WrappedKey,
			envelope.WrapNonce, envelope.KeyVersion, formatTime(now), string(params.AccountID),
			string(params.SecretID), params.ExpectedGeneration)
		if err != nil {
			return err
		}
		if err := expectAffected(update); err != nil {
			return fmt.Errorf("%w: OCI environment value changed concurrently", ErrConflict)
		}
		result, err = findOCIEnvironmentSecretTx(ctx, executor, params.AccountID, params.SecretID, false)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_environment_value.rotated",
			TargetType: "oci_environment_value", TargetID: string(params.SecretID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{"generation": nextGeneration},
		}, now)
	})
	if err != nil {
		return OCIEnvironmentSecret{}, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) RemoveOCIEnvironmentSecret(
	ctx context.Context, params RemoveOCIEnvironmentSecretParams,
) error {
	if err := validateOCIResourceRecordMutationIDs(params.AccountID, params.SecretID, params.ActorID); err != nil {
		return err
	}
	if params.ExpectedGeneration < 1 {
		return fmt.Errorf("%w: expectedGeneration must be positive", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if err := requireOCIResourceFeatureTx(ctx, executor, params.AccountID); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE oci_environment_secrets
			SET value_ciphertext = X'', value_nonce = zeroblob(12),
			    value_wrapped_key = zeroblob(48), value_wrap_nonce = zeroblob(12),
			    value_key_version = 1, updated_at = ?, removed_at = ?
			WHERE account_id = ? AND id = ? AND generation = ? AND removed_at IS NULL`,
			formatTime(now), formatTime(now), string(params.AccountID), string(params.SecretID),
			params.ExpectedGeneration)
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: OCI environment value is referenced or changed", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_environment_value.removed",
			TargetType: "oci_environment_value", TargetID: string(params.SecretID), AccountID: &params.AccountID,
			RequestID: requestID, Result: AuditSuccess, Details: map[string]any{"generation": params.ExpectedGeneration},
		}, now)
	})
	return classifyDatabaseError(err)
}

func (r *Repository) ListOCIEnvironmentSecrets(
	ctx context.Context, accountID ID,
) ([]OCIEnvironmentSecret, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return nil, err
	}
	result := []OCIEnvironmentSecret{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT id, account_id, name, slug, generation, created_at, updated_at, removed_at
			FROM oci_environment_secrets
			WHERE account_id = ? AND removed_at IS NULL ORDER BY created_at, id`, string(accountID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanOCIEnvironmentSecret(rows)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, classifyDatabaseError(err)
}

// LoadOCIEnvironmentSecretValue is an internal deployment boundary. Public
// response models contain metadata only and never call this method.
func (r *Repository) LoadOCIEnvironmentSecretValue(
	ctx context.Context, accountID, secretID ID,
) ([]byte, int64, error) {
	if validateID(accountID, "accountId") != nil || validateID(secretID, "secretId") != nil {
		return nil, 0, ErrInvalidInput
	}
	var envelope encryptedSecretEnvelope
	var generation int64
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT generation, value_ciphertext, value_nonce, value_wrapped_key,
			       value_wrap_nonce, value_key_version
			FROM oci_environment_secrets
			WHERE account_id = ? AND id = ? AND removed_at IS NULL`,
			string(accountID), string(secretID)).Scan(
			&generation, &envelope.Ciphertext, &envelope.Nonce, &envelope.WrappedKey,
			&envelope.WrapNonce, &envelope.KeyVersion)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, classifyDatabaseError(err)
	}
	value, err := r.decryptSecret(ociEnvironmentValueEnvelopeKind, secretID, accountID, envelope)
	if err != nil {
		return nil, 0, err
	}
	if normalized, normalizeErr := normalizeOCIEnvironmentValue(value); normalizeErr != nil || !bytes.Equal(normalized, value) {
		clear(value)
		return nil, 0, errors.New("stored OCI environment value is invalid")
	}
	return value, generation, nil
}

func (r *Repository) CreateOCIVolume(ctx context.Context, params CreateOCIVolumeParams) (OCIVolume, error) {
	if err := validateOCIResourceMutationIDs(params.AccountID, params.ActorID); err != nil {
		return OCIVolume{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return OCIVolume{}, err
	}
	name, err := validateOCIApplicationName(params.Name)
	if err != nil {
		return OCIVolume{}, err
	}
	slug, err := validateSlug(params.Slug)
	if err != nil {
		return OCIVolume{}, err
	}
	id, err := r.newID()
	if err != nil {
		return OCIVolume{}, err
	}
	now := r.timestamp()
	result := OCIVolume{ID: id, AccountID: params.AccountID, Name: name, Slug: slug, CreatedAt: now, UpdatedAt: now}
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if err := requireOCIResourceFeatureTx(ctx, executor, params.AccountID); err != nil {
			return err
		}
		var count int64
		if err := executor.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM oci_volumes WHERE account_id = ? AND removed_at IS NULL`,
			string(params.AccountID)).Scan(&count); err != nil {
			return err
		}
		if count >= maximumOCIVolumesPerAccount {
			return fmt.Errorf("%w: account OCI volume limit reached", ErrConflict)
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO oci_volumes (id, account_id, name, slug, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, string(id), string(params.AccountID), name, slug,
			formatTime(now), formatTime(now)); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_volume.created", TargetType: "oci_volume",
			TargetID: string(id), AccountID: &params.AccountID, RequestID: requestID,
			Result: AuditSuccess, Details: map[string]any{},
		}, now)
	})
	if err != nil {
		return OCIVolume{}, classifyDatabaseError(err)
	}
	return result, nil
}

func (r *Repository) RemoveOCIVolume(ctx context.Context, params RemoveOCIVolumeParams) error {
	if err := validateOCIResourceRecordMutationIDs(params.AccountID, params.VolumeID, params.ActorID); err != nil {
		return err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if err := requireOCIResourceFeatureTx(ctx, executor, params.AccountID); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE oci_volumes SET updated_at = ?, removed_at = ?
			WHERE account_id = ? AND id = ? AND removed_at IS NULL`,
			formatTime(now), formatTime(now), string(params.AccountID), string(params.VolumeID))
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return fmt.Errorf("%w: OCI volume is referenced or changed", ErrConflict)
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.ActorID, Action: "oci_volume.removed", TargetType: "oci_volume",
			TargetID: string(params.VolumeID), AccountID: &params.AccountID, RequestID: requestID,
			Result: AuditSuccess, Details: map[string]any{},
		}, now)
	})
	return classifyDatabaseError(err)
}

func (r *Repository) ListOCIVolumes(ctx context.Context, accountID ID) ([]OCIVolume, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return nil, err
	}
	result := []OCIVolume{}
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT id, account_id, name, slug, created_at, updated_at, removed_at
			FROM oci_volumes WHERE account_id = ? AND removed_at IS NULL ORDER BY created_at, id`, string(accountID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanOCIVolume(rows)
			if err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, classifyDatabaseError(err)
}

func normalizeOCIEnvironmentValue(value []byte) ([]byte, error) {
	if len(value) < 1 || len(value) > maximumOCIEnvironmentValueBytes || !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 {
		return nil, fmt.Errorf("%w: OCI environment value must be non-empty UTF-8 without NUL bytes and at most %d bytes",
			ErrInvalidInput, maximumOCIEnvironmentValueBytes)
	}
	return append([]byte(nil), value...), nil
}

func requireOCIResourceFeatureTx(ctx context.Context, reader store.Reader, accountID ID) error {
	limits, err := currentPackageLimitsTx(ctx, reader, accountID)
	if err != nil {
		return err
	}
	if !limits.Features.OCIApplications || limits.MaxOCIApplications == 0 {
		return fmt.Errorf("%w: OCI applications are not enabled by the account package", ErrConflict)
	}
	return nil
}

func validateOCIResourceMutationIDs(accountID, actorID ID) error {
	if err := validateID(accountID, "accountId"); err != nil {
		return err
	}
	return validateID(actorID, "actorId")
}

func validateOCIResourceRecordMutationIDs(accountID, recordID, actorID ID) error {
	if err := validateOCIResourceMutationIDs(accountID, actorID); err != nil {
		return err
	}
	return validateID(recordID, "resourceId")
}

func findOCIEnvironmentSecretTx(
	ctx context.Context, reader store.Reader, accountID, secretID ID, includeRemoved bool,
) (OCIEnvironmentSecret, error) {
	query := `SELECT id, account_id, name, slug, generation, created_at, updated_at, removed_at
		FROM oci_environment_secrets WHERE account_id = ? AND id = ?`
	if !includeRemoved {
		query += ` AND removed_at IS NULL`
	}
	return scanOCIEnvironmentSecret(reader.QueryRowContext(ctx, query, string(accountID), string(secretID)))
}

func scanOCIEnvironmentSecret(scanner rowScanner) (OCIEnvironmentSecret, error) {
	var result OCIEnvironmentSecret
	var createdAt, updatedAt string
	var removedAt sql.NullString
	if err := scanner.Scan(&result.ID, &result.AccountID, &result.Name, &result.Slug, &result.Generation,
		&createdAt, &updatedAt, &removedAt); err != nil {
		return OCIEnvironmentSecret{}, err
	}
	var err error
	if result.Generation < 1 {
		return OCIEnvironmentSecret{}, errors.New("stored OCI environment value is invalid")
	}
	if result.CreatedAt, err = parseTime(createdAt); err != nil {
		return OCIEnvironmentSecret{}, err
	}
	if result.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return OCIEnvironmentSecret{}, err
	}
	if result.RemovedAt, err = parseOptionalTime(removedAt); err != nil {
		return OCIEnvironmentSecret{}, err
	}
	return result, nil
}

func findOCIVolumeTx(
	ctx context.Context, reader store.Reader, accountID, volumeID ID, includeRemoved bool,
) (OCIVolume, error) {
	query := `SELECT id, account_id, name, slug, created_at, updated_at, removed_at
		FROM oci_volumes WHERE account_id = ? AND id = ?`
	if !includeRemoved {
		query += ` AND removed_at IS NULL`
	}
	return scanOCIVolume(reader.QueryRowContext(ctx, query, string(accountID), string(volumeID)))
}

func scanOCIVolume(scanner rowScanner) (OCIVolume, error) {
	var result OCIVolume
	var createdAt, updatedAt string
	var removedAt sql.NullString
	if err := scanner.Scan(&result.ID, &result.AccountID, &result.Name, &result.Slug,
		&createdAt, &updatedAt, &removedAt); err != nil {
		return OCIVolume{}, err
	}
	var err error
	if result.CreatedAt, err = parseTime(createdAt); err != nil {
		return OCIVolume{}, err
	}
	if result.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return OCIVolume{}, err
	}
	if result.RemovedAt, err = parseOptionalTime(removedAt); err != nil {
		return OCIVolume{}, err
	}
	return result, nil
}
