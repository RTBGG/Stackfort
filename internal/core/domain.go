// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/store"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

type preparedDomainTarget struct {
	spec              DomainTargetSpec
	documentRootPath  string
	redirectURL       string
	redirectASCIIHost string
	redirectPort      string
}

type domainLifecycleAction string

const (
	domainLifecycleCreate  domainLifecycleAction = "create"
	domainLifecycleEdit    domainLifecycleAction = "edit"
	domainLifecycleSuspend domainLifecycleAction = "suspend"
	domainLifecycleResume  domainLifecycleAction = "resume"
	domainLifecycleRemove  domainLifecycleAction = "remove"
)

func (r *Repository) CreateDomain(ctx context.Context, params CreateDomainParams) (Domain, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return Domain{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Domain{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return Domain{}, err
	}
	if err := validateOptionalID(params.DomainID, "domainId"); err != nil {
		return Domain{}, err
	}
	if params.OperationID != nil && params.DomainID == nil {
		return Domain{}, fmt.Errorf("%w: operation-correlated domain creation requires a stable domainId", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Domain{}, err
	}
	name, err := normalizeDomainBase(params.Name)
	if err != nil {
		return Domain{}, err
	}
	canonicalMode, err := normalizeCanonicalMode(params.CanonicalMode)
	if err != nil {
		return Domain{}, err
	}
	tlsMode, err := normalizeTLSMode(params.TLSMode)
	if err != nil {
		return Domain{}, err
	}
	wafMode, err := normalizeWAFMode(params.WAFMode)
	if err != nil {
		return Domain{}, err
	}
	cachePreset, err := normalizeCachePreset(params.CachePreset)
	if err != nil {
		return Domain{}, err
	}
	target, err := prepareDomainTarget(params.Target, name)
	if err != nil {
		return Domain{}, err
	}
	if err := validateCacheTarget(cachePreset, target.spec.Type); err != nil {
		return Domain{}, err
	}

	var domainID ID
	if params.DomainID != nil {
		domainID = *params.DomainID
	} else {
		domainID, err = r.newID()
		if err != nil {
			return Domain{}, err
		}
	}
	targetID, err := r.newID()
	if err != nil {
		return Domain{}, err
	}
	rootCandidateID, err := r.newID()
	if err != nil {
		return Domain{}, err
	}
	redirectCandidateID, err := r.newID()
	if err != nil {
		return Domain{}, err
	}
	now := r.timestamp()

	err = r.state.Write(ctx, func(executor store.Executor) error {
		if replay, err := domainMutationReplayTx(
			ctx, executor, params.AccountID, params.OperationID, domainLifecycleCreate, domainID,
		); err != nil || replay {
			return err
		}
		limits, err := mutableAccountLimits(ctx, executor, params.AccountID)
		if err != nil {
			return err
		}
		var domainCount int64
		if err := executor.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM domains
			WHERE account_id = ? AND removed_at IS NULL`, string(params.AccountID)).Scan(&domainCount); err != nil {
			return err
		}
		if domainCount >= limits.MaxDomains {
			return fmt.Errorf("%w: package domain limit of %d reached", ErrConflict, limits.MaxDomains)
		}
		if err := validateTargetAgainstPackage(target, limits); err != nil {
			return err
		}
		if err := ensureNoWildcardConflict(ctx, executor, name.ASCII, "", target); err != nil {
			return err
		}

		_, err = executor.ExecContext(ctx, `
			INSERT INTO domains (
				id, account_id, display_name, ascii_name, status, canonical_mode,
				created_at, updated_at, created_by_identity_id
			) VALUES (?, ?, ?, ?, 'pending', ?, ?, ?, ?)`,
			string(domainID),
			string(params.AccountID),
			name.Display,
			name.ASCII,
			string(canonicalMode),
			formatTime(now),
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		if err := r.insertTargetTx(
			ctx,
			executor,
			params.AccountID,
			domainID,
			targetID,
			rootCandidateID,
			redirectCandidateID,
			name,
			target,
			params.ActorID,
			now,
		); err != nil {
			return err
		}
		if err := insertInitialTLSState(ctx, executor, params.AccountID, domainID, name, target, params.DisableTLS, tlsMode, now); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO domain_waf_policies (account_id, domain_id, mode, updated_at)
			VALUES (?, ?, ?, ?)`,
			string(params.AccountID), string(domainID), string(wafMode), formatTime(now),
		); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			INSERT INTO domain_cache_policies (account_id, domain_id, preset, updated_at)
			VALUES (?, ?, ?, ?)`,
			string(params.AccountID), string(domainID), string(cachePreset), formatTime(now),
		); err != nil {
			return err
		}
		if err := recordDomainMutationTx(
			ctx, executor, params.AccountID, params.OperationID, domainLifecycleCreate, domainID, now,
		); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:     params.ActorID,
			Action:      "domain.created",
			TargetType:  "domain",
			TargetID:    string(domainID),
			AccountID:   &params.AccountID,
			RequestID:   requestID,
			OperationID: params.OperationID,
			Result:      AuditSuccess,
			Details: map[string]any{
				"asciiName":     name.ASCII,
				"canonicalMode": canonicalMode,
				"targetType":    target.spec.Type,
				"wafMode":       wafMode,
				"cachePreset":   cachePreset,
			},
		}, now)
	})
	if err != nil {
		return Domain{}, classifyDatabaseError(err)
	}
	return r.GetDomain(ctx, params.AccountID, domainID)
}

func (r *Repository) ReplaceDomainTarget(ctx context.Context, params ReplaceDomainTargetParams) (Domain, error) {
	target := params.Target
	return r.UpdateDomain(ctx, UpdateDomainParams{
		AccountID: params.AccountID, DomainID: params.DomainID, Target: &target,
		OperationID: params.OperationID, ActorID: params.ActorID, RequestID: params.RequestID,
	})
}

func (r *Repository) UpdateDomain(ctx context.Context, params UpdateDomainParams) (Domain, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return Domain{}, err
	}
	if err := validateID(params.DomainID, "domainId"); err != nil {
		return Domain{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Domain{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return Domain{}, err
	}
	if params.Target == nil && params.CanonicalMode == nil && params.WAFMode == nil && params.CachePreset == nil {
		return Domain{}, fmt.Errorf("%w: domain update contains no changes", ErrInvalidInput)
	}
	var canonicalMode *CanonicalMode
	if params.CanonicalMode != nil {
		normalized, err := normalizeCanonicalMode(*params.CanonicalMode)
		if err != nil {
			return Domain{}, err
		}
		canonicalMode = &normalized
	}
	var wafMode *WAFMode
	if params.WAFMode != nil {
		normalized, err := normalizeWAFMode(*params.WAFMode)
		if err != nil {
			return Domain{}, err
		}
		wafMode = &normalized
	}
	var cachePreset *CachePreset
	if params.CachePreset != nil {
		normalized, err := normalizeCachePreset(*params.CachePreset)
		if err != nil {
			return Domain{}, err
		}
		cachePreset = &normalized
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Domain{}, err
	}
	targetID, err := r.newID()
	if err != nil {
		return Domain{}, err
	}
	rootCandidateID, err := r.newID()
	if err != nil {
		return Domain{}, err
	}
	redirectCandidateID, err := r.newID()
	if err != nil {
		return Domain{}, err
	}
	now := r.timestamp()

	err = r.state.Write(ctx, func(executor store.Executor) error {
		if replay, err := domainMutationReplayTx(
			ctx, executor, params.AccountID, params.OperationID, domainLifecycleEdit, params.DomainID,
		); err != nil || replay {
			return err
		}
		limits, err := mutableAccountLimits(ctx, executor, params.AccountID)
		if err != nil {
			return err
		}
		var displayName, asciiName, status, currentTargetType, currentCachePreset string
		if err := executor.QueryRowContext(ctx, `
			SELECT domain.display_name, domain.ascii_name, domain.status, target.target_type, cache.preset
			FROM domains AS domain
			JOIN domain_targets AS target
			  ON target.account_id = domain.account_id AND target.domain_id = domain.id AND target.superseded_at IS NULL
			JOIN domain_cache_policies AS cache
			  ON cache.account_id = domain.account_id AND cache.domain_id = domain.id
			WHERE domain.account_id = ? AND domain.id = ?`, string(params.AccountID), string(params.DomainID)).Scan(
			&displayName,
			&asciiName,
			&status,
			&currentTargetType,
			&currentCachePreset,
		); err != nil {
			return err
		}
		if DomainStatus(status) == DomainRemoved {
			return fmt.Errorf("%w: removed domain cannot be updated", ErrConflict)
		}
		name := NormalizedDomainName{Display: displayName, ASCII: asciiName}
		var prepared *preparedDomainTarget
		if params.Target != nil {
			value, err := prepareDomainTarget(*params.Target, name)
			if err != nil {
				return err
			}
			if err := validateTargetAgainstPackage(value, limits); err != nil {
				return err
			}
			if err := ensureNoWildcardConflict(ctx, executor, name.ASCII, string(params.DomainID), value); err != nil {
				return err
			}
			prepared = &value
			result, err := executor.ExecContext(ctx, `
				UPDATE domain_targets
				SET superseded_at = ?
				WHERE account_id = ? AND domain_id = ? AND superseded_at IS NULL`,
				formatTime(now), string(params.AccountID), string(params.DomainID),
			)
			if err != nil {
				return err
			}
			if err := expectAffected(result); err != nil {
				return err
			}
			if err := r.insertTargetTx(
				ctx, executor, params.AccountID, params.DomainID,
				targetID, rootCandidateID, redirectCandidateID, name, value, params.ActorID, now,
			); err != nil {
				return err
			}
			if err := refreshTLSIntentTx(ctx, executor, params.AccountID, params.DomainID, name, value, now); err != nil {
				return err
			}
		}
		effectiveTargetType := DomainTargetType(currentTargetType)
		if prepared != nil {
			effectiveTargetType = prepared.spec.Type
		}
		effectiveCachePreset := CachePreset(currentCachePreset)
		if cachePreset != nil {
			effectiveCachePreset = *cachePreset
		}
		if err := validateCacheTarget(effectiveCachePreset, effectiveTargetType); err != nil {
			return err
		}
		newCanonicalMode := CanonicalMode("")
		if canonicalMode != nil {
			newCanonicalMode = *canonicalMode
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE domains
			SET canonical_mode = CASE WHEN ? = '' THEN canonical_mode ELSE ? END,
			    status = CASE WHEN status = 'suspended' THEN status ELSE 'pending' END,
			    updated_at = ?
			WHERE account_id = ? AND id = ?`,
			string(newCanonicalMode), string(newCanonicalMode), formatTime(now),
			string(params.AccountID), string(params.DomainID),
		); err != nil {
			return err
		}
		if wafMode != nil {
			result, err := executor.ExecContext(ctx, `
				UPDATE domain_waf_policies
				SET mode = ?, updated_at = ?
				WHERE account_id = ? AND domain_id = ?`,
				string(*wafMode), formatTime(now), string(params.AccountID), string(params.DomainID),
			)
			if err != nil {
				return err
			}
			if err := expectAffected(result); err != nil {
				return err
			}
		}
		if cachePreset != nil {
			result, err := executor.ExecContext(ctx, `
				UPDATE domain_cache_policies
				SET preset = ?, updated_at = ?
				WHERE account_id = ? AND domain_id = ?`,
				string(*cachePreset), formatTime(now), string(params.AccountID), string(params.DomainID),
			)
			if err != nil {
				return err
			}
			if err := expectAffected(result); err != nil {
				return err
			}
		}
		if err := recordDomainMutationTx(
			ctx, executor, params.AccountID, params.OperationID, domainLifecycleEdit, params.DomainID, now,
		); err != nil {
			return err
		}
		details := map[string]any{
			"canonicalModeChanged": canonicalMode != nil,
			"targetChanged":        prepared != nil,
			"wafModeChanged":       wafMode != nil,
			"cachePresetChanged":   cachePreset != nil,
		}
		if wafMode != nil {
			details["wafMode"] = *wafMode
		}
		if cachePreset != nil {
			details["cachePreset"] = *cachePreset
		}
		if prepared != nil {
			details["targetRevisionId"] = targetID
			details["targetType"] = prepared.spec.Type
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:     params.ActorID,
			Action:      "domain.updated",
			TargetType:  "domain",
			TargetID:    string(params.DomainID),
			AccountID:   &params.AccountID,
			RequestID:   requestID,
			OperationID: params.OperationID,
			Result:      AuditSuccess,
			Details:     details,
		}, now)
	})
	if err != nil {
		return Domain{}, classifyDatabaseError(err)
	}
	return r.GetDomain(ctx, params.AccountID, params.DomainID)
}

func (r *Repository) SuspendDomain(ctx context.Context, params ChangeDomainStatusParams) (Domain, error) {
	return r.changeDomainStatus(ctx, params, domainLifecycleSuspend, DomainSuspended, []DomainStatus{
		DomainPending, DomainActive,
	})
}

func (r *Repository) ResumeDomain(ctx context.Context, params ChangeDomainStatusParams) (Domain, error) {
	return r.changeDomainStatus(ctx, params, domainLifecycleResume, DomainPending, []DomainStatus{
		DomainSuspended,
	})
}

func (r *Repository) changeDomainStatus(
	ctx context.Context,
	params ChangeDomainStatusParams,
	action domainLifecycleAction,
	targetStatus DomainStatus,
	allowed []DomainStatus,
) (Domain, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return Domain{}, err
	}
	if err := validateID(params.DomainID, "domainId"); err != nil {
		return Domain{}, err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return Domain{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Domain{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Domain{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if replay, err := domainMutationReplayTx(
			ctx, executor, params.AccountID, params.OperationID, action, params.DomainID,
		); err != nil || replay {
			return err
		}
		if _, err := mutableAccountLimits(ctx, executor, params.AccountID); err != nil {
			return err
		}
		placeholders := make([]string, len(allowed))
		arguments := []any{string(targetStatus), formatTime(now), string(params.AccountID), string(params.DomainID)}
		for index, status := range allowed {
			placeholders[index] = "?"
			arguments = append(arguments, string(status))
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE domains
			SET status = ?, updated_at = ?
			WHERE account_id = ? AND id = ? AND status IN (`+strings.Join(placeholders, ",")+`)`,
			arguments...,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return sql.ErrNoRows
		}
		if err := recordDomainMutationTx(
			ctx, executor, params.AccountID, params.OperationID, action, params.DomainID, now,
		); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "domain." + string(action) + "d",
			TargetType: "domain", TargetID: string(params.DomainID), AccountID: &params.AccountID,
			RequestID: requestID, OperationID: params.OperationID, Result: AuditSuccess,
			Details: map[string]any{"status": targetStatus},
		}, now)
	})
	if err != nil {
		return Domain{}, classifyDatabaseError(err)
	}
	return r.GetDomain(ctx, params.AccountID, params.DomainID)
}

func (r *Repository) RemoveDomain(ctx context.Context, params RemoveDomainParams) error {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return err
	}
	if err := validateID(params.DomainID, "domainId"); err != nil {
		return err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return err
	}
	if err := validateOptionalID(params.OperationID, "operationId"); err != nil {
		return err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if replay, err := domainMutationReplayTx(
			ctx, executor, params.AccountID, params.OperationID, domainLifecycleRemove, params.DomainID,
		); err != nil || replay {
			return err
		}
		if _, err := mutableAccountLimits(ctx, executor, params.AccountID); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE domains
			SET status = 'removed', removed_at = ?, updated_at = ?
			WHERE account_id = ? AND id = ? AND removed_at IS NULL`,
			formatTime(now),
			formatTime(now),
			string(params.AccountID),
			string(params.DomainID),
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return sql.ErrNoRows
		}
		if err := recordDomainMutationTx(
			ctx, executor, params.AccountID, params.OperationID, domainLifecycleRemove, params.DomainID, now,
		); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE tls_certificates
			SET status = 'retired', retired_at = ?
			WHERE account_id = ? AND domain_id = ? AND status = 'active'`,
			formatTime(now), string(params.AccountID), string(params.DomainID),
		); err != nil {
			return err
		}
		if _, err := executor.ExecContext(ctx, `
			UPDATE domain_tls_states
			SET enabled = 0, issuance_status = 'disabled', active_certificate_ref = NULL,
			    issuer = NULL, not_before = NULL, expires_at = NULL, next_renewal_at = NULL,
			    last_error_code = NULL, last_error_at = NULL, updated_at = ?
			WHERE account_id = ? AND domain_id = ?`,
			formatTime(now), string(params.AccountID), string(params.DomainID),
		); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:     params.ActorID,
			Action:      "domain.removed",
			TargetType:  "domain",
			TargetID:    string(params.DomainID),
			AccountID:   &params.AccountID,
			RequestID:   requestID,
			OperationID: params.OperationID,
			Result:      AuditSuccess,
			Details:     map[string]any{"historyRetained": true},
		}, now)
	})
	return classifyDatabaseError(err)
}

// ConfirmDomainActivation promotes only pending domain rows whose immutable
// target still matches the snapshot that the agent activated. Later edits can
// therefore never be marked active by an older operation replay.
func (r *Repository) ConfirmDomainActivation(
	ctx context.Context,
	params ConfirmDomainActivationParams,
) (int64, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return 0, err
	}
	if err := validateID(params.DesiredStateRevisionID, "desiredStateRevisionId"); err != nil {
		return 0, err
	}
	if err := validateID(params.OperationID, "operationId"); err != nil {
		return 0, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return 0, err
	}
	if len(params.Expected) > 10_000 {
		return 0, fmt.Errorf("%w: too many domain activation expectations", ErrInvalidInput)
	}
	seen := make(map[ID]struct{}, len(params.Expected))
	for _, expected := range params.Expected {
		if err := validateID(expected.DomainID, "domainId"); err != nil {
			return 0, err
		}
		if err := validateID(expected.TargetID, "targetId"); err != nil {
			return 0, err
		}
		if _, exists := seen[expected.DomainID]; exists {
			return 0, fmt.Errorf("%w: duplicate domain activation expectation", ErrInvalidInput)
		}
		seen[expected.DomainID] = struct{}{}
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return 0, err
	}
	now := r.timestamp()
	var activated int64
	err = r.state.Write(ctx, func(executor store.Executor) error {
		var appliedMatches bool
		if err := executor.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM applied_state_revisions
				WHERE account_id = ? AND desired_state_revision_id = ?
				  AND operation_id = ? AND status = 'active'
			)`, string(params.AccountID), string(params.DesiredStateRevisionID),
			string(params.OperationID)).Scan(&appliedMatches); err != nil {
			return err
		}
		if !appliedMatches {
			return fmt.Errorf("%w: applied state does not match the activation confirmation", ErrConflict)
		}
		for _, expected := range params.Expected {
			result, err := executor.ExecContext(ctx, `
				UPDATE domains
				SET status = 'active', updated_at = ?
				WHERE account_id = ? AND id = ? AND status = 'pending'
				  AND EXISTS (
					SELECT 1 FROM domain_targets
					WHERE account_id = ? AND domain_id = ? AND id = ?
					  AND superseded_at IS NULL
				  )`,
				formatTime(now), string(params.AccountID), string(expected.DomainID),
				string(params.AccountID), string(expected.DomainID), string(expected.TargetID),
			)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			activated += count
		}
		if activated == 0 {
			return nil
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: params.ActorID, Action: "domains.activation_confirmed",
			TargetType: "desired_state_revision", TargetID: string(params.DesiredStateRevisionID),
			AccountID: &params.AccountID, RequestID: requestID, OperationID: &params.OperationID,
			Result: AuditSuccess, Details: map[string]any{"activatedDomains": activated},
		}, now)
	})
	if err != nil {
		return 0, classifyDatabaseError(err)
	}
	return activated, nil
}

func (r *Repository) GetDomain(ctx context.Context, accountID, domainID ID) (Domain, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return Domain{}, err
	}
	if err := validateID(domainID, "domainId"); err != nil {
		return Domain{}, err
	}

	var result Domain
	var domainStatus, canonicalMode, targetType string
	var domainCreated, domainUpdated, targetCreated, tlsUpdated, wafUpdated, cacheUpdated string
	var wafMode, cachePreset string
	var removedAt sql.NullString
	var rootID, rootPath, rootCreated sql.NullString
	var phpVersion, applicationID sql.NullString
	var redirectID, redirectURL, redirectHost, redirectHostMode, redirectCreated sql.NullString
	var redirectStatus, preservePath, preserveQuery, wildcardSubdomains sql.NullInt64
	var tlsEnabled int64
	var tlsMode, tlsChallenge, tlsStatus, tlsNamesJSON string
	var certificateRef, issuer sql.NullString
	var notBefore, expiresAt, nextRenewalAt, lastErrorAt sql.NullString
	var lastErrorCode sql.NullString

	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT
				d.id, d.account_id, d.display_name, d.ascii_name, d.status,
				d.canonical_mode, d.created_at, d.updated_at, d.removed_at,
				t.id, t.target_type, t.php_version, t.application_id, t.created_at,
				r.id, r.relative_path, r.created_at,
				dr.id, dr.status_code, dr.target_url, dr.target_ascii_host, dr.host_mode,
				dr.preserve_path, dr.preserve_query, dr.wildcard_subdomains, dr.created_at,
				tls.enabled, tls.mode, tls.challenge_type, tls.issuance_status,
				tls.names_json, tls.active_certificate_ref, tls.issuer,
				tls.not_before, tls.expires_at, tls.next_renewal_at,
				tls.last_error_code, tls.last_error_at, tls.updated_at,
				waf.mode, waf.updated_at, cache.preset, cache.updated_at
			FROM domains AS d
			JOIN domain_targets AS t
			  ON t.account_id = d.account_id AND t.domain_id = d.id AND t.superseded_at IS NULL
			LEFT JOIN document_roots AS r
			  ON r.account_id = t.account_id AND r.id = t.document_root_id
			LEFT JOIN domain_redirects AS dr
			  ON dr.account_id = t.account_id AND dr.domain_id = t.domain_id AND dr.id = t.redirect_id
			JOIN domain_tls_states AS tls
			  ON tls.account_id = d.account_id AND tls.domain_id = d.id
			JOIN domain_waf_policies AS waf
			  ON waf.account_id = d.account_id AND waf.domain_id = d.id
			JOIN domain_cache_policies AS cache
			  ON cache.account_id = d.account_id AND cache.domain_id = d.id
			WHERE d.account_id = ? AND d.id = ?`, string(accountID), string(domainID)).Scan(
			&result.ID,
			&result.AccountID,
			&result.Name.Display,
			&result.Name.ASCII,
			&domainStatus,
			&canonicalMode,
			&domainCreated,
			&domainUpdated,
			&removedAt,
			&result.Target.ID,
			&targetType,
			&phpVersion,
			&applicationID,
			&targetCreated,
			&rootID,
			&rootPath,
			&rootCreated,
			&redirectID,
			&redirectStatus,
			&redirectURL,
			&redirectHost,
			&redirectHostMode,
			&preservePath,
			&preserveQuery,
			&wildcardSubdomains,
			&redirectCreated,
			&tlsEnabled,
			&tlsMode,
			&tlsChallenge,
			&tlsStatus,
			&tlsNamesJSON,
			&certificateRef,
			&issuer,
			&notBefore,
			&expiresAt,
			&nextRenewalAt,
			&lastErrorCode,
			&lastErrorAt,
			&tlsUpdated,
			&wafMode,
			&wafUpdated,
			&cachePreset,
			&cacheUpdated,
		)
	})
	if err != nil {
		return Domain{}, classifyDatabaseError(err)
	}

	result.Status = DomainStatus(domainStatus)
	result.CanonicalMode = CanonicalMode(canonicalMode)
	result.Target.Type = DomainTargetType(targetType)
	result.Target.PHPVersion = phpVersion.String
	result.TLS.Enabled = tlsEnabled == 1
	result.TLS.Mode = TLSMode(tlsMode)
	result.TLS.ChallengeType = TLSChallengeType(tlsChallenge)
	result.TLS.IssuanceStatus = TLSIssuanceStatus(tlsStatus)
	result.TLS.ActiveCertificateRef = certificateRef.String
	result.TLS.Issuer = issuer.String
	result.TLS.LastErrorCode = lastErrorCode.String
	result.WAF.Mode = WAFMode(wafMode)
	if _, err := normalizeWAFMode(result.WAF.Mode); err != nil {
		return Domain{}, fmt.Errorf("decode stored WAF mode: %w", err)
	}
	result.Cache.Preset = CachePreset(cachePreset)
	if _, err := normalizeCachePreset(result.Cache.Preset); err != nil {
		return Domain{}, fmt.Errorf("decode stored cache preset: %w", err)
	}
	if err := json.Unmarshal([]byte(tlsNamesJSON), &result.TLS.Names); err != nil {
		return Domain{}, fmt.Errorf("decode stored TLS names: %w", err)
	}
	if result.CreatedAt, err = parseTime(domainCreated); err != nil {
		return Domain{}, err
	}
	if result.UpdatedAt, err = parseTime(domainUpdated); err != nil {
		return Domain{}, err
	}
	if result.Target.CreatedAt, err = parseTime(targetCreated); err != nil {
		return Domain{}, err
	}
	if result.TLS.UpdatedAt, err = parseTime(tlsUpdated); err != nil {
		return Domain{}, err
	}
	if result.WAF.UpdatedAt, err = parseTime(wafUpdated); err != nil {
		return Domain{}, err
	}
	if result.Cache.UpdatedAt, err = parseTime(cacheUpdated); err != nil {
		return Domain{}, err
	}
	if result.RemovedAt, err = parseOptionalTime(removedAt); err != nil {
		return Domain{}, err
	}
	if result.TLS.NotBefore, err = parseOptionalTime(notBefore); err != nil {
		return Domain{}, err
	}
	if result.TLS.ExpiresAt, err = parseOptionalTime(expiresAt); err != nil {
		return Domain{}, err
	}
	if result.TLS.NextRenewalAt, err = parseOptionalTime(nextRenewalAt); err != nil {
		return Domain{}, err
	}
	if result.TLS.LastErrorAt, err = parseOptionalTime(lastErrorAt); err != nil {
		return Domain{}, err
	}
	if applicationID.Valid {
		value := ID(applicationID.String)
		result.Target.ApplicationID = &value
	}
	if rootID.Valid {
		root := &DocumentRoot{ID: ID(rootID.String), AccountID: accountID, RelativePath: rootPath.String}
		if root.CreatedAt, err = parseTime(rootCreated.String); err != nil {
			return Domain{}, err
		}
		if err := r.state.Read(ctx, func(reader store.Reader) error {
			return reader.QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM domain_targets AS target
				JOIN domains AS domain
				  ON domain.account_id = target.account_id AND domain.id = target.domain_id
				WHERE target.account_id = ? AND target.document_root_id = ?
				  AND target.superseded_at IS NULL AND domain.removed_at IS NULL`,
				string(accountID),
				rootID.String,
			).Scan(&root.ReferenceCount)
		}); err != nil {
			return Domain{}, err
		}
		result.Target.DocumentRoot = root
	}
	if redirectID.Valid {
		redirect := &DomainRedirect{
			ID:                 ID(redirectID.String),
			StatusCode:         RedirectStatusCode(redirectStatus.Int64),
			TargetURL:          redirectURL.String,
			TargetASCIIHost:    redirectHost.String,
			HostMode:           RedirectHostMode(redirectHostMode.String),
			PreservePath:       preservePath.Int64 == 1,
			PreserveQuery:      preserveQuery.Int64 == 1,
			WildcardSubdomains: wildcardSubdomains.Int64 == 1,
		}
		if redirect.CreatedAt, err = parseTime(redirectCreated.String); err != nil {
			return Domain{}, err
		}
		result.Target.Redirect = redirect
	}
	result.WAF.Exceptions, err = r.ListDomainWAFExceptions(ctx, accountID, domainID)
	if err != nil {
		return Domain{}, err
	}
	return result, nil
}

func (r *Repository) ListDomains(ctx context.Context, accountID ID, includeRemoved bool) ([]Domain, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return nil, err
	}
	query := `SELECT id FROM domains WHERE account_id = ?`
	if !includeRemoved {
		query += ` AND removed_at IS NULL`
	}
	query += ` ORDER BY ascii_name, created_at`
	var ids []ID
	err := r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, query, string(accountID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id ID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, classifyDatabaseError(err)
	}
	domains := make([]Domain, 0, len(ids))
	for _, id := range ids {
		domain, err := r.GetDomain(ctx, accountID, id)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func prepareDomainTarget(spec DomainTargetSpec, source NormalizedDomainName) (preparedDomainTarget, error) {
	if spec.Type == "" {
		spec.Type = DomainTargetStatic
	}
	prepared := preparedDomainTarget{spec: spec}
	switch spec.Type {
	case DomainTargetStatic, DomainTargetPHP:
		if spec.ApplicationID != nil || spec.Redirect != nil {
			return preparedDomainTarget{}, fmt.Errorf("%w: document-root target has incompatible fields", ErrInvalidInput)
		}
		if spec.Type == DomainTargetPHP {
			if !phpVersionPattern.MatchString(spec.PHPVersion) {
				return preparedDomainTarget{}, fmt.Errorf("%w: PHP target requires a major.minor version", ErrInvalidInput)
			}
		} else if spec.PHPVersion != "" {
			return preparedDomainTarget{}, fmt.Errorf("%w: static target must not specify a PHP version", ErrInvalidInput)
		}
		if spec.RootMode == "" {
			spec.RootMode = DocumentRootDefault
			prepared.spec.RootMode = DocumentRootDefault
		}
		switch spec.RootMode {
		case DocumentRootDefault:
			if spec.DocumentRoot != "" || spec.SharedWithDomainID != nil {
				return preparedDomainTarget{}, fmt.Errorf("%w: default root must not include a custom or shared root", ErrInvalidInput)
			}
		case DocumentRootCustom:
			if spec.SharedWithDomainID != nil {
				return preparedDomainTarget{}, fmt.Errorf("%w: custom root must not reference another domain", ErrInvalidInput)
			}
			root, err := normalizeDocumentRoot(spec.DocumentRoot)
			if err != nil {
				return preparedDomainTarget{}, err
			}
			prepared.documentRootPath = root
		case DocumentRootShared:
			if spec.DocumentRoot != "" || spec.SharedWithDomainID == nil {
				return preparedDomainTarget{}, fmt.Errorf("%w: shared root requires one source domain and no path", ErrInvalidInput)
			}
			if err := validateID(*spec.SharedWithDomainID, "sharedWithDomainId"); err != nil {
				return preparedDomainTarget{}, err
			}
		default:
			return preparedDomainTarget{}, fmt.Errorf("%w: unsupported document-root mode", ErrInvalidInput)
		}
	case DomainTargetOCIApplication:
		if spec.ApplicationID == nil || spec.Redirect != nil || spec.PHPVersion != "" || spec.DocumentRoot != "" || spec.SharedWithDomainID != nil {
			return preparedDomainTarget{}, fmt.Errorf("%w: OCI target requires only an application ID", ErrInvalidInput)
		}
		if err := validateID(*spec.ApplicationID, "applicationId"); err != nil {
			return preparedDomainTarget{}, err
		}
	case DomainTargetRedirect:
		if spec.Redirect == nil || spec.ApplicationID != nil || spec.PHPVersion != "" || spec.DocumentRoot != "" || spec.SharedWithDomainID != nil {
			return preparedDomainTarget{}, fmt.Errorf("%w: redirect target has missing or incompatible fields", ErrInvalidInput)
		}
		if spec.Redirect.StatusCode != RedirectPermanent && spec.Redirect.StatusCode != RedirectTemporary {
			return preparedDomainTarget{}, fmt.Errorf("%w: redirect status must be 301 or 302", ErrInvalidInput)
		}
		redirect := *spec.Redirect
		if redirect.HostMode == "" {
			redirect.HostMode = RedirectHostBoth
		}
		if redirect.HostMode != RedirectHostApexOnly && redirect.HostMode != RedirectHostWWWOnly &&
			redirect.HostMode != RedirectHostBoth {
			return preparedDomainTarget{}, fmt.Errorf("%w: unsupported redirect host mode", ErrInvalidInput)
		}
		if redirect.WildcardSubdomains && redirect.HostMode != RedirectHostBoth {
			return preparedDomainTarget{}, fmt.Errorf("%w: wildcard redirects require both exact source hosts", ErrInvalidInput)
		}
		prepared.spec.Redirect = &redirect
		var err error
		prepared.redirectURL, prepared.redirectASCIIHost, prepared.redirectPort, err = normalizeRedirectURL(redirect.TargetURL)
		if err != nil {
			return preparedDomainTarget{}, err
		}
		isSourceHost := prepared.redirectASCIIHost == source.ASCII || prepared.redirectASCIIHost == "www."+source.ASCII
		isWildcardChild := redirect.WildcardSubdomains && strings.HasSuffix(prepared.redirectASCIIHost, "."+source.ASCII)
		if prepared.redirectPort == "" && (isSourceHost || isWildcardChild) {
			return preparedDomainTarget{}, fmt.Errorf("%w: redirect target would loop to the source domain", ErrInvalidInput)
		}
	default:
		return preparedDomainTarget{}, fmt.Errorf("%w: unsupported domain target type", ErrInvalidInput)
	}
	return prepared, nil
}

func validateTargetAgainstPackage(target preparedDomainTarget, limits PackageLimits) error {
	switch target.spec.Type {
	case DomainTargetPHP:
		if !slices.Contains(limits.AllowedPHPVersions, target.spec.PHPVersion) {
			return fmt.Errorf("%w: PHP %s is not allowed by the account package", ErrConflict, target.spec.PHPVersion)
		}
	case DomainTargetRedirect:
		if !limits.Features.CustomRedirects {
			return fmt.Errorf("%w: redirects are not enabled by the account package", ErrConflict)
		}
	case DomainTargetOCIApplication:
		if !limits.Features.OCIApplications {
			return fmt.Errorf("%w: OCI applications are not enabled by the account package", ErrConflict)
		}
		// The OCI phase will add the account-owned application parent relation.
		// Until that record exists, accepting an opaque ID would break tenant isolation.
		return fmt.Errorf("%w: OCI application ownership cannot be verified before application records are available", ErrConflict)
	}
	return nil
}

func mutableAccountLimits(ctx context.Context, executor store.Executor, accountID ID) (PackageLimits, error) {
	var status, limitsJSON string
	if err := executor.QueryRowContext(ctx, `
		SELECT h.status, assignment.effective_limits_json
		FROM hosting_accounts AS h
		JOIN account_package_assignments AS assignment
		  ON assignment.account_id = h.id AND assignment.id = h.current_package_assignment_id
		WHERE h.id = ? AND assignment.superseded_at IS NULL`, string(accountID)).Scan(&status, &limitsJSON); err != nil {
		return PackageLimits{}, err
	}
	if AccountStatus(status) != AccountActive {
		return PackageLimits{}, fmt.Errorf("%w: only active accounts may change domains", ErrConflict)
	}
	return decodeLimits(limitsJSON)
}

func normalizeCanonicalMode(value CanonicalMode) (CanonicalMode, error) {
	if value == "" {
		return CanonicalPreferApex, nil
	}
	if value != CanonicalPreferApex && value != CanonicalPreferWWW && value != CanonicalServeBoth {
		return "", fmt.Errorf("%w: unsupported canonical host mode", ErrInvalidInput)
	}
	return value, nil
}

func normalizeTLSMode(value TLSMode) (TLSMode, error) {
	if value == "" {
		return TLSModeACME, nil
	}
	if value != TLSModeACME && value != TLSModeImported {
		return "", fmt.Errorf("%w: unsupported TLS mode", ErrInvalidInput)
	}
	return value, nil
}

func normalizeWAFMode(value WAFMode) (WAFMode, error) {
	normalized, err := wafconfig.Normalize(value)
	if err != nil {
		return "", fmt.Errorf("%w: unsupported WAF mode", ErrInvalidInput)
	}
	return normalized, nil
}

func normalizeCachePreset(value CachePreset) (CachePreset, error) {
	normalized, err := cacheconfig.NormalizePreset(value)
	if err != nil {
		return "", fmt.Errorf("%w: unsupported cache preset", ErrInvalidInput)
	}
	return normalized, nil
}

func validateCacheTarget(preset CachePreset, targetType DomainTargetType) error {
	if preset == CachePresetDisabled {
		return nil
	}
	if targetType != DomainTargetPHP && targetType != DomainTargetOCIApplication {
		return fmt.Errorf("%w: page caching is supported only for dynamic application targets", ErrInvalidInput)
	}
	return nil
}

func ensureNoWildcardConflict(
	ctx context.Context,
	executor store.Executor,
	asciiName string,
	excludedDomainID string,
	target preparedDomainTarget,
) error {
	var conflict bool
	if err := executor.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM domains AS domain
			JOIN domain_targets AS target
			  ON target.account_id = domain.account_id
			 AND target.domain_id = domain.id
			 AND target.superseded_at IS NULL
			JOIN domain_redirects AS redirect
			  ON redirect.account_id = target.account_id
			 AND redirect.domain_id = target.domain_id
			 AND redirect.id = target.redirect_id
			WHERE domain.removed_at IS NULL
			  AND redirect.wildcard_subdomains = 1
			  AND domain.id <> ?
			  AND (? = domain.ascii_name OR ? LIKE '%.' || domain.ascii_name)
		)`, excludedDomainID, asciiName, asciiName).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return fmt.Errorf("%w: host is already covered by a wildcard redirect", ErrConflict)
	}
	if target.spec.Type != DomainTargetRedirect || !target.spec.Redirect.WildcardSubdomains {
		return nil
	}
	if err := executor.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM domains
			WHERE removed_at IS NULL
			  AND id <> ?
			  AND ascii_name LIKE '%.' || ?
		)`, excludedDomainID, asciiName).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return fmt.Errorf("%w: wildcard redirect overlaps an existing domain", ErrConflict)
	}
	return nil
}

func (r *Repository) insertTargetTx(
	ctx context.Context,
	executor store.Executor,
	accountID ID,
	domainID ID,
	targetID ID,
	rootCandidateID ID,
	redirectCandidateID ID,
	name NormalizedDomainName,
	target preparedDomainTarget,
	actorID *ID,
	now time.Time,
) error {
	var rootID any
	if target.spec.Type == DomainTargetStatic || target.spec.Type == DomainTargetPHP {
		resolvedRootID, err := r.resolveDocumentRootTx(
			ctx,
			executor,
			accountID,
			domainID,
			rootCandidateID,
			name.ASCII,
			target,
			actorID,
			now,
		)
		if err != nil {
			return err
		}
		rootID = string(resolvedRootID)
	}

	var redirectID any
	if target.spec.Type == DomainTargetRedirect {
		_, err := executor.ExecContext(ctx, `
			INSERT INTO domain_redirects (
				id, account_id, domain_id, status_code, target_url, target_ascii_host,
				host_mode, preserve_path, preserve_query, wildcard_subdomains, created_at,
				created_by_identity_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(redirectCandidateID),
			string(accountID),
			string(domainID),
			int64(target.spec.Redirect.StatusCode),
			target.redirectURL,
			target.redirectASCIIHost,
			string(target.spec.Redirect.HostMode),
			boolInt(target.spec.Redirect.PreservePath),
			boolInt(target.spec.Redirect.PreserveQuery),
			boolInt(target.spec.Redirect.WildcardSubdomains),
			formatTime(now),
			nullableID(actorID),
		)
		if err != nil {
			return err
		}
		redirectID = string(redirectCandidateID)
	}

	_, err := executor.ExecContext(ctx, `
		INSERT INTO domain_targets (
			id, account_id, domain_id, target_type, document_root_id,
			php_version, application_id, redirect_id, created_at,
			created_by_identity_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(targetID),
		string(accountID),
		string(domainID),
		string(target.spec.Type),
		rootID,
		nullableString(target.spec.PHPVersion),
		nullableID(target.spec.ApplicationID),
		redirectID,
		formatTime(now),
		nullableID(actorID),
	)
	return err
}

func (r *Repository) resolveDocumentRootTx(
	ctx context.Context,
	executor store.Executor,
	accountID ID,
	domainID ID,
	rootCandidateID ID,
	asciiName string,
	target preparedDomainTarget,
	actorID *ID,
	now time.Time,
) (ID, error) {
	if target.spec.RootMode == DocumentRootShared {
		if *target.spec.SharedWithDomainID == domainID {
			return "", fmt.Errorf("%w: domain cannot declare itself as a shared-root source", ErrInvalidInput)
		}
		var rootID ID
		err := executor.QueryRowContext(ctx, `
			SELECT root.id
			FROM domains AS domain
			JOIN domain_targets AS target
			  ON target.account_id = domain.account_id
			 AND target.domain_id = domain.id
			 AND target.superseded_at IS NULL
			JOIN document_roots AS root
			  ON root.account_id = target.account_id AND root.id = target.document_root_id
			WHERE domain.account_id = ? AND domain.id = ? AND domain.removed_at IS NULL`,
			string(accountID),
			string(*target.spec.SharedWithDomainID),
		).Scan(&rootID)
		if err != nil {
			return "", err
		}
		return rootID, nil
	}

	rootPath := target.documentRootPath
	if target.spec.RootMode == DocumentRootDefault {
		var hasParent bool
		if err := executor.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM domains
				WHERE account_id = ? AND removed_at IS NULL
				  AND id <> ? AND ? LIKE '%.' || ascii_name
			)`, string(accountID), string(domainID), asciiName).Scan(&hasParent); err != nil {
			return "", err
		}
		if hasParent {
			rootPath = asciiName
		} else {
			rootPath = "public_html"
		}
	}
	rootPath, err := normalizeDocumentRoot(rootPath)
	if err != nil {
		return "", err
	}
	_, err = executor.ExecContext(ctx, `
		INSERT INTO document_roots (
			id, account_id, relative_path, created_at, created_by_identity_id
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(account_id, relative_path) DO NOTHING`,
		string(rootCandidateID),
		string(accountID),
		rootPath,
		formatTime(now),
		nullableID(actorID),
	)
	if err != nil {
		return "", err
	}
	var rootID ID
	if err := executor.QueryRowContext(ctx, `
		SELECT id FROM document_roots WHERE account_id = ? AND relative_path = ?`,
		string(accountID),
		rootPath,
	).Scan(&rootID); err != nil {
		return "", err
	}
	return rootID, nil
}

func insertInitialTLSState(
	ctx context.Context,
	executor store.Executor,
	accountID ID,
	domainID ID,
	name NormalizedDomainName,
	target preparedDomainTarget,
	disabled bool,
	mode TLSMode,
	now time.Time,
) error {
	names, challenge := tlsIntent(name, target)
	if mode == TLSModeImported {
		challenge = TLSChallengeImported
	}
	status := TLSPending
	if disabled {
		status = TLSDisabled
	}
	namesJSON, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("encode TLS names: %w", err)
	}
	_, err = executor.ExecContext(ctx, `
		INSERT INTO domain_tls_states (
			account_id, domain_id, enabled, mode, challenge_type,
			issuance_status, names_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(accountID),
		string(domainID),
		boolInt(!disabled),
		string(mode),
		string(challenge),
		string(status),
		string(namesJSON),
		formatTime(now),
	)
	return err
}

func refreshTLSIntentTx(
	ctx context.Context,
	executor store.Executor,
	accountID ID,
	domainID ID,
	name NormalizedDomainName,
	target preparedDomainTarget,
	now time.Time,
) error {
	names, challenge := tlsIntent(name, target)
	namesJSON, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("encode TLS names: %w", err)
	}
	if _, err := executor.ExecContext(ctx, `
		UPDATE tls_certificates
		SET status = 'retired', retired_at = ?
		WHERE account_id = ? AND domain_id = ? AND status = 'active'
		  AND EXISTS (
		      SELECT 1 FROM domain_tls_states tls
		      WHERE tls.account_id = ? AND tls.domain_id = ?
		        AND (
		            tls.names_json <> ?
		            OR tls.challenge_type <> CASE WHEN tls.mode = 'imported' THEN 'imported' ELSE ? END
		        )
		  )`,
		formatTime(now), string(accountID), string(domainID), string(accountID), string(domainID),
		string(namesJSON), string(challenge),
	); err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `
		UPDATE domain_tls_states
		SET challenge_type = CASE WHEN mode = 'imported' THEN 'imported' ELSE ? END,
		    names_json = ?,
		    issuance_status = CASE WHEN enabled = 0 THEN 'disabled' ELSE 'pending' END,
		    active_certificate_ref = NULL,
		    issuer = NULL,
		    not_before = NULL,
		    expires_at = NULL,
		    next_renewal_at = NULL,
		    last_error_code = NULL,
		    last_error_at = NULL,
		    updated_at = ?
		WHERE account_id = ? AND domain_id = ?
		  AND (
		      names_json <> ?
		      OR challenge_type <> CASE WHEN mode = 'imported' THEN 'imported' ELSE ? END
		  )`,
		string(challenge),
		string(namesJSON),
		formatTime(now),
		string(accountID),
		string(domainID),
		string(namesJSON),
		string(challenge),
	)
	return err
}

func tlsIntent(name NormalizedDomainName, target preparedDomainTarget) ([]string, TLSChallengeType) {
	names := []string{name.ASCII, "www." + name.ASCII}
	challenge := TLSChallengeHTTP01
	if target.spec.Type == DomainTargetRedirect && target.spec.Redirect.WildcardSubdomains {
		names = append(names, "*."+name.ASCII)
		challenge = TLSChallengeDNS01
	}
	return names, challenge
}

func parseOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func domainMutationReplayTx(
	ctx context.Context,
	executor store.Executor,
	accountID ID,
	operationID *ID,
	action domainLifecycleAction,
	domainID ID,
) (bool, error) {
	if operationID == nil {
		return false, nil
	}
	var storedDomainID ID
	var storedAction string
	err := executor.QueryRowContext(ctx, `
		SELECT domain_id, action
		FROM domain_lifecycle_mutations
		WHERE account_id = ? AND operation_id = ?`,
		string(accountID), string(*operationID),
	).Scan(&storedDomainID, &storedAction)
	switch {
	case err == nil:
		if storedDomainID != domainID || storedAction != string(action) {
			return false, fmt.Errorf("%w: operation was already used for a different domain mutation", ErrConflict)
		}
		return true, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	var operationExists bool
	if err := executor.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM operations WHERE account_id = ? AND id = ?
		)`, string(accountID), string(*operationID)).Scan(&operationExists); err != nil {
		return false, err
	}
	if !operationExists {
		return false, fmt.Errorf("%w: operation does not belong to the account", ErrNotFound)
	}
	return false, nil
}

func recordDomainMutationTx(
	ctx context.Context,
	executor store.Executor,
	accountID ID,
	operationID *ID,
	action domainLifecycleAction,
	domainID ID,
	now time.Time,
) error {
	if operationID == nil {
		return nil
	}
	_, err := executor.ExecContext(ctx, `
		INSERT INTO domain_lifecycle_mutations (
			operation_id, account_id, domain_id, action, applied_at
		) VALUES (?, ?, ?, ?, ?)`,
		string(*operationID), string(accountID), string(domainID), string(action), formatTime(now),
	)
	return err
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
