// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/store"
)

func (r *Repository) CreateHostingAccount(ctx context.Context, params CreateHostingAccountParams) (HostingAccount, error) {
	name, err := validateText(params.Name, "name", 1, 120)
	if err != nil {
		return HostingAccount{}, err
	}
	slug, err := validateSlug(params.Slug)
	if err != nil {
		return HostingAccount{}, err
	}
	if err := validateID(params.OwnerIdentityID, "ownerIdentityId"); err != nil {
		return HostingAccount{}, err
	}
	if err := validateID(params.PackageID, "packageId"); err != nil {
		return HostingAccount{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return HostingAccount{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return HostingAccount{}, err
	}
	accountID, err := r.newID()
	if err != nil {
		return HostingAccount{}, err
	}
	membershipID, err := r.newID()
	if err != nil {
		return HostingAccount{}, err
	}
	assignmentID, err := r.newID()
	if err != nil {
		return HostingAccount{}, err
	}
	now := r.timestamp()
	username, err := hostingidentity.UsernameForAccount(string(accountID))
	if err != nil {
		return HostingAccount{}, fmt.Errorf("derive hosting account username: %w", err)
	}
	homeDirectory, err := hostingidentity.HomeDirectoryForAccount(string(accountID))
	if err != nil {
		return HostingAccount{}, fmt.Errorf("derive hosting account home: %w", err)
	}
	account := HostingAccount{
		ID:                         accountID,
		Name:                       name,
		Slug:                       slug,
		Status:                     AccountActive,
		CurrentPackageAssignmentID: assignmentID,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		packageRevision, limitsJSON, err := currentPackageRevision(ctx, executor, params.PackageID)
		if err != nil {
			return err
		}
		limits, err := decodeLimits(limitsJSON)
		if err != nil {
			return err
		}
		var numericID int64
		if err := executor.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(uid) + 1, ?)
			FROM hosting_account_unix_identities`, int64(hostingidentity.MinimumID)).Scan(&numericID); err != nil {
			return err
		}
		allocatedID, conversionErr := hostingUnixNumericID(numericID)
		if conversionErr != nil {
			return fmt.Errorf("%w: reserved hosting Unix identity range is exhausted", ErrConflict)
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO hosting_accounts (
				id, name, slug, status, current_package_assignment_id,
				created_at, updated_at, created_by_identity_id
			) VALUES (?, ?, ?, 'active', ?, ?, ?, ?)`,
			string(account.ID),
			account.Name,
			account.Slug,
			string(assignmentID),
			formatTime(now),
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		account.UnixIdentity = HostingUnixIdentity{
			AccountID: account.ID, Username: username, UID: allocatedID, GID: allocatedID,
			HomeDirectory: homeDirectory, State: HostingUnixIdentityAllocated, AllocatedAt: now,
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO hosting_account_unix_identities (
				account_id, username, uid, gid, home_directory, lifecycle_state, allocated_at
			) VALUES (?, ?, ?, ?, ?, 'allocated', ?)`,
			string(account.ID), username, numericID, numericID, homeDirectory, formatTime(now),
		)
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO hosting_account_filesystems (
				account_id, project_id, desired_storage_bytes, desired_storage_inodes,
				revision, status, capability_status, updated_at
			) VALUES (?, ?, ?, ?, 1, 'pending', 'pending', ?)`,
			string(account.ID), numericID, limits.StorageBytes, limits.StorageInodes, formatTime(now),
		)
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO hosting_account_resources (
				account_id, desired_cpu_quota_percent, desired_cpu_weight,
				desired_memory_bytes, desired_swap_bytes, desired_process_limit,
				revision, status, capability_status, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, 1, 'pending', 'pending', ?)`,
			string(account.ID), limits.CPUQuotaPercent, limits.CPUWeight,
			limits.MemoryBytes, limits.SwapBytes, limits.ProcessLimit, formatTime(now),
		)
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO account_memberships (
				id, account_id, identity_id, role, granted_at, granted_by_identity_id
			) VALUES (?, ?, ?, 'owner', ?, ?)`,
			string(membershipID),
			string(account.ID),
			string(params.OwnerIdentityID),
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO account_package_assignments (
				id, account_id, package_id, package_revision, effective_limits_json,
				assigned_at, assigned_by_identity_id
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(assignmentID),
			string(account.ID),
			string(params.PackageID),
			packageRevision,
			limitsJSON,
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "hosting_account.created",
			TargetType: "hosting_account",
			TargetID:   string(account.ID),
			AccountID:  &account.ID,
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"ownerIdentityId": params.OwnerIdentityID,
				"packageId":       params.PackageID,
				"packageRevision": packageRevision,
				"unixUsername":    username,
				"unixUid":         numericID,
				"unixGid":         numericID,
			},
		}, now)
	})
	if err != nil {
		return HostingAccount{}, classifyDatabaseError(err)
	}
	return account, nil
}

func (r *Repository) GetHostingAccount(ctx context.Context, accountID ID) (HostingAccount, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return HostingAccount{}, err
	}

	var account HostingAccount
	var status, createdAt, updatedAt string
	var identityState, allocatedAt string
	var reconciledAt, archiveRequestedAt, archivedAt, archiveReference sql.NullString
	var deletionRequestedAt, deletedAt sql.NullString
	var uid, gid int64
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT h.id, h.name, h.slug, h.status, h.current_package_assignment_id,
			       h.created_at, h.updated_at,
			       u.account_id, u.username, u.uid, u.gid, u.home_directory,
			       u.lifecycle_state, u.allocated_at, u.reconciled_at,
			       u.archive_requested_at, u.archived_at, u.archive_reference,
			       u.deletion_requested_at, u.deleted_at
			FROM hosting_accounts AS h
			JOIN hosting_account_unix_identities AS u ON u.account_id = h.id
			WHERE h.id = ?`, string(accountID)).Scan(
			&account.ID,
			&account.Name,
			&account.Slug,
			&status,
			&account.CurrentPackageAssignmentID,
			&createdAt,
			&updatedAt,
			&account.UnixIdentity.AccountID,
			&account.UnixIdentity.Username,
			&uid,
			&gid,
			&account.UnixIdentity.HomeDirectory,
			&identityState,
			&allocatedAt,
			&reconciledAt,
			&archiveRequestedAt,
			&archivedAt,
			&archiveReference,
			&deletionRequestedAt,
			&deletedAt,
		)
	})
	if err != nil {
		return HostingAccount{}, classifyDatabaseError(err)
	}
	account.Status = AccountStatus(status)
	account.UnixIdentity.UID, err = hostingUnixNumericID(uid)
	if err != nil {
		return HostingAccount{}, err
	}
	account.UnixIdentity.GID, err = hostingUnixNumericID(gid)
	if err != nil || account.UnixIdentity.GID != account.UnixIdentity.UID {
		return HostingAccount{}, fmt.Errorf("stored hosting Unix identity numeric IDs are invalid")
	}
	account.UnixIdentity.State = HostingUnixIdentityState(identityState)
	account.UnixIdentity.ArchiveReference = archiveReference.String
	account.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return HostingAccount{}, err
	}
	account.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return HostingAccount{}, err
	}
	account.UnixIdentity.AllocatedAt, err = parseTime(allocatedAt)
	if err != nil {
		return HostingAccount{}, err
	}
	optionalTimes := []struct {
		stored sql.NullString
		target **time.Time
	}{
		{reconciledAt, &account.UnixIdentity.ReconciledAt},
		{archiveRequestedAt, &account.UnixIdentity.ArchiveRequestedAt},
		{archivedAt, &account.UnixIdentity.ArchivedAt},
		{deletionRequestedAt, &account.UnixIdentity.DeletionRequestedAt},
		{deletedAt, &account.UnixIdentity.DeletedAt},
	}
	for _, optional := range optionalTimes {
		if !optional.stored.Valid {
			continue
		}
		parsed, parseErr := parseTime(optional.stored.String)
		if parseErr != nil {
			return HostingAccount{}, parseErr
		}
		*optional.target = &parsed
	}
	return account, nil
}

func hostingUnixNumericID(value int64) (uint32, error) {
	if value < int64(hostingidentity.MinimumID) || value > int64(hostingidentity.MaximumID) {
		return 0, fmt.Errorf("stored hosting Unix identity numeric ID is outside the reserved range")
	}
	return uint32(value), nil
}

// HostSpec returns the strictly validated value sent to the privileged agent.
func (identity HostingUnixIdentity) HostSpec() (hostingidentity.Spec, error) {
	spec := hostingidentity.Spec{
		AccountID: string(identity.AccountID), Username: identity.Username,
		UID: identity.UID, GID: identity.GID, HomeDirectory: identity.HomeDirectory,
	}
	if err := hostingidentity.Validate(spec); err != nil {
		return hostingidentity.Spec{}, fmt.Errorf("stored hosting Unix identity is invalid: %w", err)
	}
	return spec, nil
}

func (r *Repository) AddMembership(ctx context.Context, params AddMembershipParams) (Membership, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return Membership{}, err
	}
	if err := validateID(params.IdentityID, "identityId"); err != nil {
		return Membership{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return Membership{}, err
	}
	if params.Role != MembershipOwner && params.Role != MembershipMember && params.Role != MembershipAuditor {
		return Membership{}, fmt.Errorf("%w: unsupported membership role", ErrInvalidInput)
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return Membership{}, err
	}
	id, err := r.newID()
	if err != nil {
		return Membership{}, err
	}
	now := r.timestamp()
	membership := Membership{
		ID:         id,
		AccountID:  params.AccountID,
		IdentityID: params.IdentityID,
		Role:       params.Role,
		GrantedAt:  now,
	}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		_, err := executor.ExecContext(ctx, `
			INSERT INTO account_memberships (
				id, account_id, identity_id, role, granted_at, granted_by_identity_id
			) VALUES (?, ?, ?, ?, ?, ?)`,
			string(membership.ID),
			string(membership.AccountID),
			string(membership.IdentityID),
			string(membership.Role),
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "account_membership.granted",
			TargetType: "identity",
			TargetID:   string(membership.IdentityID),
			AccountID:  &membership.AccountID,
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"membershipId": membership.ID,
				"role":         membership.Role,
			},
		}, now)
	})
	if err != nil {
		return Membership{}, classifyDatabaseError(err)
	}
	return membership, nil
}

func (r *Repository) AssignPackage(ctx context.Context, params AssignPackageParams) (PackageAssignment, error) {
	if err := validateID(params.AccountID, "accountId"); err != nil {
		return PackageAssignment{}, err
	}
	if err := validateID(params.PackageID, "packageId"); err != nil {
		return PackageAssignment{}, err
	}
	if err := validateOptionalID(params.ActorID, "actorId"); err != nil {
		return PackageAssignment{}, err
	}
	requestID, err := validateOptionalText(params.RequestID, "requestId", 128)
	if err != nil {
		return PackageAssignment{}, err
	}
	id, err := r.newID()
	if err != nil {
		return PackageAssignment{}, err
	}
	now := r.timestamp()
	assignment := PackageAssignment{ID: id, AccountID: params.AccountID, PackageID: params.PackageID, AssignedAt: now}

	err = r.state.Write(ctx, func(executor store.Executor) error {
		var currentAssignmentID string
		var accountStatus string
		if err := executor.QueryRowContext(ctx, `
			SELECT current_package_assignment_id, status
			FROM hosting_accounts
			WHERE id = ?`, string(params.AccountID)).Scan(&currentAssignmentID, &accountStatus); err != nil {
			return err
		}
		if AccountStatus(accountStatus) == AccountArchived {
			return fmt.Errorf("%w: archived account cannot receive a package", ErrConflict)
		}
		packageRevision, limitsJSON, err := currentPackageRevision(ctx, executor, params.PackageID)
		if err != nil {
			return err
		}
		var currentPackageID string
		var currentPackageRevision int64
		if err := executor.QueryRowContext(ctx, `
			SELECT package_id, package_revision
			FROM account_package_assignments
			WHERE id = ?`, currentAssignmentID).Scan(&currentPackageID, &currentPackageRevision); err != nil {
			return err
		}
		if currentPackageID == string(params.PackageID) && currentPackageRevision == packageRevision {
			return fmt.Errorf("%w: account already has this package revision", ErrConflict)
		}
		updateResult, err := executor.ExecContext(ctx, `
			UPDATE account_package_assignments
			SET superseded_at = ?
			WHERE id = ? AND account_id = ? AND superseded_at IS NULL`,
			formatTime(now),
			currentAssignmentID,
			string(params.AccountID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(updateResult); err != nil {
			return err
		}
		_, err = executor.ExecContext(ctx, `
			INSERT INTO account_package_assignments (
				id, account_id, package_id, package_revision, effective_limits_json,
				assigned_at, assigned_by_identity_id
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(assignment.ID),
			string(assignment.AccountID),
			string(assignment.PackageID),
			packageRevision,
			limitsJSON,
			formatTime(now),
			nullableID(params.ActorID),
		)
		if err != nil {
			return err
		}
		updateResult, err = executor.ExecContext(ctx, `
			UPDATE hosting_accounts
			SET current_package_assignment_id = ?, updated_at = ?
			WHERE id = ? AND current_package_assignment_id = ?`,
			string(assignment.ID),
			formatTime(now),
			string(assignment.AccountID),
			currentAssignmentID,
		)
		if err != nil {
			return err
		}
		if err := expectAffected(updateResult); err != nil {
			return err
		}
		limits, err := decodeLimits(limitsJSON)
		if err != nil {
			return err
		}
		assignment.PackageRevision = packageRevision
		assignment.EffectiveLimits = limits
		updateResult, err = executor.ExecContext(ctx, `
			UPDATE hosting_account_filesystems
			SET desired_storage_bytes = ?, desired_storage_inodes = ?,
			    revision = revision + 1, status = 'pending',
			    capability_status = 'pending', reason_code = NULL, updated_at = ?
			WHERE account_id = ?`,
			limits.StorageBytes, limits.StorageInodes, formatTime(now), string(assignment.AccountID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(updateResult); err != nil {
			return err
		}
		updateResult, err = executor.ExecContext(ctx, `
			UPDATE hosting_account_resources
			SET desired_cpu_quota_percent = ?, desired_cpu_weight = ?,
			    desired_memory_bytes = ?, desired_swap_bytes = ?, desired_process_limit = ?,
			    revision = revision + 1, status = 'pending',
			    capability_status = 'pending', reason_code = NULL, updated_at = ?
			WHERE account_id = ?`,
			limits.CPUQuotaPercent, limits.CPUWeight, limits.MemoryBytes,
			limits.SwapBytes, limits.ProcessLimit, formatTime(now), string(assignment.AccountID),
		)
		if err != nil {
			return err
		}
		if err := expectAffected(updateResult); err != nil {
			return err
		}
		return r.appendAuditTx(ctx, executor, AppendAuditEventParams{
			ActorID:    params.ActorID,
			Action:     "hosting_account.package_assigned",
			TargetType: "hosting_account",
			TargetID:   string(assignment.AccountID),
			AccountID:  &assignment.AccountID,
			RequestID:  requestID,
			Result:     AuditSuccess,
			Details: map[string]any{
				"assignmentId":    assignment.ID,
				"packageId":       assignment.PackageID,
				"packageRevision": assignment.PackageRevision,
			},
		}, now)
	})
	if err != nil {
		return PackageAssignment{}, classifyDatabaseError(err)
	}
	return assignment, nil
}

func (r *Repository) CurrentPackageAssignment(ctx context.Context, accountID ID) (PackageAssignment, error) {
	if err := validateID(accountID, "accountId"); err != nil {
		return PackageAssignment{}, err
	}

	var assignment PackageAssignment
	var limitsJSON, assignedAt string
	err := r.state.Read(ctx, func(reader store.Reader) error {
		return reader.QueryRowContext(ctx, `
			SELECT a.id, a.account_id, a.package_id, a.package_revision,
			       a.effective_limits_json, a.assigned_at
			FROM hosting_accounts AS h
			JOIN account_package_assignments AS a
			  ON a.account_id = h.id AND a.id = h.current_package_assignment_id
			WHERE h.id = ? AND a.superseded_at IS NULL`, string(accountID)).Scan(
			&assignment.ID,
			&assignment.AccountID,
			&assignment.PackageID,
			&assignment.PackageRevision,
			&limitsJSON,
			&assignedAt,
		)
	})
	if err != nil {
		return PackageAssignment{}, classifyDatabaseError(err)
	}
	assignment.EffectiveLimits, err = decodeLimits(limitsJSON)
	if err != nil {
		return PackageAssignment{}, err
	}
	assignment.AssignedAt, err = parseTime(assignedAt)
	if err != nil {
		return PackageAssignment{}, err
	}
	return assignment, nil
}
