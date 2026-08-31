// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"fmt"

	"github.com/RTBGG/stackfort/internal/store"
)

const maximumSelfServiceAccounts = 100

// GetSelfServiceContext returns only accounts linked to the authenticated
// identity by an active membership. Platform administration is reported as a
// separate capability and never expands this owner-facing list implicitly.
func (r *Repository) GetSelfServiceContext(
	ctx context.Context,
	params GetSelfServiceContextParams,
) (SelfServiceContext, error) {
	decision, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject,
		Action:  AuthorizationIdentityProfileView,
	})
	if err != nil {
		return SelfServiceContext{}, err
	}

	result := SelfServiceContext{
		PlatformAdministrator: decision.PlatformAdministrator,
		Accounts:              make([]SelfServiceAccount, 0),
	}
	err = r.state.Read(ctx, func(reader store.Reader) error {
		rows, err := reader.QueryContext(ctx, `
			SELECT h.id, h.name, h.slug, h.status, m.role,
			       p.id, p.name, a.package_revision, a.effective_limits_json,
			       (h.status = 'active' AND u.lifecycle_state = 'reconciled' AND
			        u.oci_runtime_reconciled_at IS NOT NULL AND
			        f.status = 'applied' AND f.capability_status = 'available' AND
			        r.status = 'applied' AND r.capability_status = 'available'),
			       (
			           SELECT COUNT(*) FROM domains d
			           WHERE d.account_id = h.id AND d.status <> 'removed'
			       ),
			       h.created_at, h.updated_at
			FROM account_memberships m
			JOIN hosting_accounts h ON h.id = m.account_id
			JOIN account_package_assignments a
			  ON a.account_id = h.id AND a.id = h.current_package_assignment_id
			JOIN packages p ON p.id = a.package_id
			JOIN hosting_account_unix_identities u ON u.account_id = h.id
			JOIN hosting_account_filesystems f ON f.account_id = h.id
			JOIN hosting_account_resources r ON r.account_id = h.id
			WHERE m.identity_id = ?
			  AND m.revoked_at IS NULL
			  AND h.status IN ('active', 'suspended')
			  AND a.superseded_at IS NULL
			ORDER BY lower(h.name), h.id
			LIMIT ?`, string(params.Subject.identityID), maximumSelfServiceAccounts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var account SelfServiceAccount
			var status, role, limitsJSON, createdAt, updatedAt string
			if err := rows.Scan(
				&account.ID, &account.Name, &account.Slug, &status, &role,
				&account.PackageID, &account.PackageName, &account.PackageRevision, &limitsJSON,
				&account.HostReady, &account.DomainCount, &createdAt, &updatedAt,
			); err != nil {
				return err
			}
			account.Status = AccountStatus(status)
			account.MembershipRole = MembershipRole(role)
			account.EffectiveLimits, err = decodeLimits(limitsJSON)
			if err != nil {
				return err
			}
			account.CreatedAt, err = parseTime(createdAt)
			if err != nil {
				return err
			}
			account.UpdatedAt, err = parseTime(updatedAt)
			if err != nil {
				return err
			}
			result.Accounts = append(result.Accounts, account)
		}
		return rows.Err()
	})
	if err != nil {
		return SelfServiceContext{}, classifyDatabaseError(err)
	}
	return result, nil
}

// UpdateOwnProfile changes only the authenticated identity. A fresh session is
// revalidated inside the write transaction so a revoked or stale session
// cannot race the mutation.
func (r *Repository) UpdateOwnProfile(ctx context.Context, params UpdateOwnProfileParams) (Identity, error) {
	if _, err := r.Authorize(ctx, AuthorizeParams{
		Subject: params.Subject,
		Action:  AuthorizationIdentityProfileManage,
	}); err != nil {
		return Identity{}, err
	}
	email, normalizedEmail, err := normalizeEmail(params.Email)
	if err != nil {
		return Identity{}, err
	}
	displayName, err := validateText(params.DisplayName, "displayName", 1, 120)
	if err != nil {
		return Identity{}, err
	}
	if params.Locale != LocaleEnglish && params.Locale != LocaleGerman {
		return Identity{}, fmt.Errorf("%w: locale must be en or de", ErrInvalidInput)
	}
	requestID, sourceAddress, err := validateSessionManagementMetadata(params.RequestID, params.SourceAddress)
	if err != nil {
		return Identity{}, err
	}
	now := r.timestamp()
	err = r.state.Write(ctx, func(executor store.Executor) error {
		if _, err := r.requireSubjectSessionTx(ctx, executor, params.Subject, true, now); err != nil {
			return err
		}
		result, err := executor.ExecContext(ctx, `
			UPDATE identities
			SET email = ?, normalized_email = ?, display_name = ?, locale = ?, updated_at = ?
			WHERE id = ? AND status = 'active'`,
			email, normalizedEmail, displayName, string(params.Locale), formatTime(now),
			string(params.Subject.identityID))
		if err != nil {
			return err
		}
		if err := expectAffected(result); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID: &params.Subject.identityID, SessionID: &params.Subject.sessionID,
			SourceAddress: sourceAddress, Action: "identity.profile_updated",
			TargetType: "identity", TargetID: string(params.Subject.identityID),
			RequestID: requestID, Result: AuditSuccess,
			Details: map[string]any{"locale": params.Locale},
		}, now)
	})
	if err != nil {
		return Identity{}, classifyDatabaseError(err)
	}
	return r.GetIdentity(ctx, params.Subject.identityID)
}
