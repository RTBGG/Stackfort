// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/RTBGG/stackfort/internal/store"
)

const recentAuthenticationTTL = 5 * time.Minute

type authorizationScope uint8

const (
	authorizationScopePlatform authorizationScope = iota + 1
	authorizationScopeAccount
	authorizationScopeIdentity
)

type authorizationPolicy struct {
	scope          authorizationScope
	roles          []MembershipRole
	recentRequired bool
	mutating       bool
}

var authorizationPolicies = map[AuthorizationAction]authorizationPolicy{
	AuthorizationPlatformView:             {scope: authorizationScopePlatform},
	AuthorizationPlatformManage:           {scope: authorizationScopePlatform, recentRequired: true, mutating: true},
	AuthorizationPackagesView:             {scope: authorizationScopePlatform},
	AuthorizationPackagesManage:           {scope: authorizationScopePlatform, recentRequired: true, mutating: true},
	AuthorizationAccountsCreate:           {scope: authorizationScopePlatform, recentRequired: true, mutating: true},
	AuthorizationAccountView:              {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember, MembershipAuditor}},
	AuthorizationAccountManage:            {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner}, recentRequired: true, mutating: true},
	AuthorizationAccountMembershipsView:   {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipAuditor}},
	AuthorizationAccountMembershipsManage: {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner}, recentRequired: true, mutating: true},
	AuthorizationAccountPackageView:       {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember, MembershipAuditor}},
	AuthorizationAccountPackageManage:     {scope: authorizationScopeAccount, recentRequired: true, mutating: true},
	AuthorizationAccountResourcesView:     {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember, MembershipAuditor}},
	AuthorizationAccountResourcesManage:   {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember}, mutating: true},
	AuthorizationAccountFilesView:         {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember}},
	AuthorizationAccountFilesManage:       {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember}, mutating: true},
	AuthorizationAccountBackupsView:       {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember}},
	AuthorizationAccountBackupsManage:     {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember}, mutating: true},
	AuthorizationAccountBackupsRestore:    {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner}, recentRequired: true, mutating: true},
	AuthorizationAccountBackupsDelete:     {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner}, recentRequired: true, mutating: true},
	AuthorizationAccountLogsView:          {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember, MembershipAuditor}},
	AuthorizationAccountJobsView:          {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember, MembershipAuditor}},
	AuthorizationAccountJobsManage:        {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember}, mutating: true},
	AuthorizationAccountCredentialsManage: {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipMember}, recentRequired: true, mutating: true},
	AuthorizationAccountAuditView:         {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner, MembershipAuditor}},
	AuthorizationAccountDestructive:       {scope: authorizationScopeAccount, roles: []MembershipRole{MembershipOwner}, recentRequired: true, mutating: true},
	AuthorizationIdentityProfileView:      {scope: authorizationScopeIdentity},
	AuthorizationIdentityProfileManage:    {scope: authorizationScopeIdentity, recentRequired: true, mutating: true},
	AuthorizationIdentityFactorsView:      {scope: authorizationScopeIdentity},
	AuthorizationIdentityFactorsManage:    {scope: authorizationScopeIdentity, recentRequired: true, mutating: true},
	AuthorizationIdentitySessionsView:     {scope: authorizationScopeIdentity},
	AuthorizationIdentitySessionsManage:   {scope: authorizationScopeIdentity, recentRequired: true, mutating: true},
}

type authorizationFacts struct {
	identityStatus        IdentityStatus
	authenticatedAt       time.Time
	lastSeenAt            time.Time
	expiresAt             time.Time
	platformAdministrator bool
	accountExists         bool
	accountStatus         AccountStatus
	membershipRole        *MembershipRole
}

// Authorize evaluates an explicit server-selected action against current
// session, platform-role, membership, and account-state data. Unknown actions
// and unmatched relationships are denied by default.
func (r *Repository) Authorize(ctx context.Context, params AuthorizeParams) (AuthorizationDecision, error) {
	if err := r.validateAuthorizationSubject(params.Subject); err != nil {
		return AuthorizationDecision{}, err
	}
	policy, known := authorizationPolicies[params.Action]
	if !known {
		return AuthorizationDecision{}, ErrAuthorizationDenied
	}
	if policy.scope != authorizationScopeAccount && params.AccountID != nil {
		return AuthorizationDecision{}, ErrAuthorizationDenied
	}
	if policy.scope == authorizationScopeAccount {
		if params.AccountID == nil {
			return AuthorizationDecision{}, ErrAuthorizationDenied
		}
		if err := validateID(*params.AccountID, "accountId"); err != nil {
			return AuthorizationDecision{}, ErrAuthorizationDenied
		}
	}

	facts, err := r.loadAuthorizationFacts(ctx, params.Subject, params.AccountID)
	if err != nil {
		return AuthorizationDecision{}, err
	}
	return evaluateAuthorization(params.Action, params.AccountID, policy, facts, r.timestamp())
}

// GetAuthorizedHostingAccount performs the account lookup and permission check
// from the same SQLite statement so changing an object identifier cannot turn a
// successful authorization for one account into a read from another account.
func (r *Repository) GetAuthorizedHostingAccount(
	ctx context.Context,
	params GetAuthorizedHostingAccountParams,
) (AuthorizedHostingAccount, error) {
	if err := r.validateAuthorizationSubject(params.Subject); err != nil {
		return AuthorizedHostingAccount{}, err
	}
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return AuthorizedHostingAccount{}, ErrAuthorizationDenied
	}

	var account AuthorizedHostingAccount
	var facts authorizationFacts
	var identityStatus, authenticatedAt, lastSeenAt, expiresAt string
	var accountID, accountName, accountSlug, accountStatus, assignmentID sql.NullString
	var accountCreatedAt, accountUpdatedAt sql.NullString
	var membershipRole string
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT i.status, s.authenticated_at, s.last_seen_at, s.expires_at,
			       EXISTS (
			           SELECT 1 FROM platform_role_assignments p
			           WHERE p.identity_id = s.identity_id AND p.role = 'platform_admin'
			       ),
			       h.id, h.name, h.slug, h.status, h.current_package_assignment_id,
			       h.created_at, h.updated_at,
			       COALESCE(m.role, '')
			FROM sessions s
			JOIN identities i ON i.id = s.identity_id
			LEFT JOIN hosting_accounts h ON h.id = ?
			LEFT JOIN account_memberships m
			  ON m.account_id = h.id
			 AND m.identity_id = s.identity_id
			 AND m.revoked_at IS NULL
			WHERE s.id = ? AND s.identity_id = ? AND s.revoked_at IS NULL`,
			string(params.AccountID), string(params.Subject.sessionID), string(params.Subject.identityID)).Scan(
			&identityStatus, &authenticatedAt, &lastSeenAt, &expiresAt,
			&facts.platformAdministrator,
			&accountID, &accountName, &accountSlug, &accountStatus, &assignmentID,
			&accountCreatedAt, &accountUpdatedAt, &membershipRole,
		)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthorizedHostingAccount{}, ErrSessionInvalid
		}
		return AuthorizedHostingAccount{}, err
	}
	if err := populateAuthorizationFacts(&facts, identityStatus, authenticatedAt, lastSeenAt, expiresAt, accountStatus.String, membershipRole); err != nil {
		return AuthorizedHostingAccount{}, err
	}
	facts.accountExists = accountID.Valid
	accountPointer := params.AccountID
	decision, err := evaluateAuthorization(
		AuthorizationAccountView,
		&accountPointer,
		authorizationPolicies[AuthorizationAccountView],
		facts,
		r.timestamp(),
	)
	if err != nil {
		return AuthorizedHostingAccount{}, err
	}

	account.Account = HostingAccount{
		ID:                         ID(accountID.String),
		Name:                       accountName.String,
		Slug:                       accountSlug.String,
		Status:                     AccountStatus(accountStatus.String),
		CurrentPackageAssignmentID: ID(assignmentID.String),
	}
	account.Account.CreatedAt, err = parseTime(accountCreatedAt.String)
	if err != nil {
		return AuthorizedHostingAccount{}, err
	}
	account.Account.UpdatedAt, err = parseTime(accountUpdatedAt.String)
	if err != nil {
		return AuthorizedHostingAccount{}, err
	}
	account.Authorization = decision
	return account, nil
}

func (r *Repository) loadAuthorizationFacts(
	ctx context.Context,
	subject AuthorizationSubject,
	accountID *ID,
) (authorizationFacts, error) {
	var facts authorizationFacts
	var identityStatus, authenticatedAt, lastSeenAt, expiresAt string
	var accountStatus, membershipRole string
	var err error
	if accountID == nil {
		err = r.state.Read(ctx, func(reader store.Reader) error {
			return reader.QueryRowContext(ctx, `
				SELECT i.status, s.authenticated_at, s.last_seen_at, s.expires_at,
				       EXISTS (
				           SELECT 1 FROM platform_role_assignments p
				           WHERE p.identity_id = s.identity_id AND p.role = 'platform_admin'
				       )
				FROM sessions s
				JOIN identities i ON i.id = s.identity_id
				WHERE s.id = ? AND s.identity_id = ? AND s.revoked_at IS NULL`,
				string(subject.sessionID), string(subject.identityID)).Scan(
				&identityStatus, &authenticatedAt, &lastSeenAt, &expiresAt,
				&facts.platformAdministrator,
			)
		})
	} else {
		err = r.state.Read(ctx, func(reader store.Reader) error {
			return reader.QueryRowContext(ctx, `
				SELECT i.status, s.authenticated_at, s.last_seen_at, s.expires_at,
				       EXISTS (
				           SELECT 1 FROM platform_role_assignments p
				           WHERE p.identity_id = s.identity_id AND p.role = 'platform_admin'
				       ),
				       h.id IS NOT NULL,
				       COALESCE(h.status, ''),
				       COALESCE(m.role, '')
				FROM sessions s
				JOIN identities i ON i.id = s.identity_id
				LEFT JOIN hosting_accounts h ON h.id = ?
				LEFT JOIN account_memberships m
				  ON m.account_id = h.id
				 AND m.identity_id = s.identity_id
				 AND m.revoked_at IS NULL
				WHERE s.id = ? AND s.identity_id = ? AND s.revoked_at IS NULL`,
				string(*accountID), string(subject.sessionID), string(subject.identityID)).Scan(
				&identityStatus, &authenticatedAt, &lastSeenAt, &expiresAt,
				&facts.platformAdministrator, &facts.accountExists,
				&accountStatus, &membershipRole,
			)
		})
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authorizationFacts{}, ErrSessionInvalid
		}
		return authorizationFacts{}, err
	}
	if err := populateAuthorizationFacts(&facts, identityStatus, authenticatedAt, lastSeenAt, expiresAt, accountStatus, membershipRole); err != nil {
		return authorizationFacts{}, err
	}
	return facts, nil
}

func populateAuthorizationFacts(
	facts *authorizationFacts,
	identityStatus, authenticatedAt, lastSeenAt, expiresAt, accountStatus, membershipRole string,
) error {
	facts.identityStatus = IdentityStatus(identityStatus)
	var err error
	facts.authenticatedAt, err = parseTime(authenticatedAt)
	if err != nil {
		return fmt.Errorf("parse authorization authentication time: %w", err)
	}
	facts.lastSeenAt, err = parseTime(lastSeenAt)
	if err != nil {
		return fmt.Errorf("parse authorization activity time: %w", err)
	}
	facts.expiresAt, err = parseTime(expiresAt)
	if err != nil {
		return fmt.Errorf("parse authorization expiry: %w", err)
	}
	if accountStatus != "" {
		facts.accountStatus = AccountStatus(accountStatus)
	}
	if membershipRole != "" {
		role := MembershipRole(membershipRole)
		facts.membershipRole = &role
	}
	return nil
}

func evaluateAuthorization(
	action AuthorizationAction,
	accountID *ID,
	policy authorizationPolicy,
	facts authorizationFacts,
	now time.Time,
) (AuthorizationDecision, error) {
	if facts.identityStatus != IdentityActive || facts.authenticatedAt.After(now) ||
		!facts.expiresAt.After(now) || !facts.lastSeenAt.Add(passwordSessionIdleTTL).After(now) {
		return AuthorizationDecision{}, ErrSessionInvalid
	}
	if policy.scope == authorizationScopePlatform {
		if !facts.platformAdministrator {
			return AuthorizationDecision{}, ErrAuthorizationDenied
		}
	} else if policy.scope == authorizationScopeAccount {
		if accountID == nil || !facts.accountExists {
			return AuthorizationDecision{}, ErrAuthorizationDenied
		}
		if facts.accountStatus != AccountActive && facts.accountStatus != AccountSuspended && facts.accountStatus != AccountArchived {
			return AuthorizationDecision{}, ErrAuthorizationDenied
		}
		if !facts.platformAdministrator {
			if facts.accountStatus == AccountArchived ||
				(facts.accountStatus == AccountSuspended && policy.mutating) ||
				facts.membershipRole == nil || !slices.Contains(policy.roles, *facts.membershipRole) {
				return AuthorizationDecision{}, ErrAuthorizationDenied
			}
		}
	} else if policy.scope != authorizationScopeIdentity {
		return AuthorizationDecision{}, ErrAuthorizationDenied
	}
	if policy.recentRequired && facts.authenticatedAt.Before(now.Add(-recentAuthenticationTTL)) {
		return AuthorizationDecision{}, ErrRecentAuthenticationRequired
	}

	decision := AuthorizationDecision{
		Action:                       action,
		PlatformAdministrator:        facts.platformAdministrator,
		RequiresRecentAuthentication: policy.recentRequired,
	}
	if accountID != nil {
		value := *accountID
		decision.AccountID = &value
	}
	if facts.membershipRole != nil {
		value := *facts.membershipRole
		decision.MembershipRole = &value
	}
	return decision, nil
}

func (r *Repository) newAuthorizationSubject(identityID, sessionID ID) AuthorizationSubject {
	return AuthorizationSubject{
		identityID: identityID,
		sessionID:  sessionID,
		proof:      r.authorizationSubjectProof(identityID, sessionID),
	}
}

func (r *Repository) authorizationSubjectProof(identityID, sessionID ID) [sha256.Size]byte {
	mac := hmac.New(sha256.New, r.authorizationSubjectKey[:])
	_, _ = mac.Write([]byte(identityID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(sessionID))
	var proof [sha256.Size]byte
	copy(proof[:], mac.Sum(nil))
	return proof
}

func (r *Repository) validateAuthorizationSubject(subject AuthorizationSubject) error {
	if err := validateID(subject.identityID, "authorization.identityId"); err != nil {
		return ErrSessionInvalid
	}
	if err := validateID(subject.sessionID, "authorization.sessionId"); err != nil {
		return ErrSessionInvalid
	}
	want := r.authorizationSubjectProof(subject.identityID, subject.sessionID)
	if !hmac.Equal(subject.proof[:], want[:]) {
		return ErrSessionInvalid
	}
	return nil
}
