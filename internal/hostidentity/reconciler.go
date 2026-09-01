// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostidentity reconciles only Stackfort's deterministic local Linux
// users and groups. It has no generic account-management entry point.
package hostidentity

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

type RuntimeCapabilityError struct{ Capability agentprotocol.Capability }

func (failure *RuntimeCapabilityError) Error() string { return ErrRuntimeUnavailable.Error() }

func (failure *RuntimeCapabilityError) Unwrap() error { return ErrRuntimeUnavailable }

var (
	ErrIdentityConflict   = errors.New("managed Unix identity conflicts with existing host identity")
	ErrArchiveRequired    = errors.New("managed account directory must be archived before deletion")
	ErrMutationFailed     = errors.New("managed Unix identity mutation failed")
	ErrInvalidDatabase    = errors.New("local Unix account database is malformed")
	ErrRuntimeUnavailable = errors.New("rootless OCI account runtime is unavailable")
)

type ReconcileResult struct {
	GroupCreated      bool `json:"groupCreated"`
	UserCreated       bool `json:"userCreated"`
	UserRepaired      bool `json:"userRepaired"`
	DirectoryCreated  bool `json:"directoryCreated"`
	OwnershipRepaired bool `json:"ownershipRepaired"`
	SubUIDsConfigured bool `json:"subUidsConfigured"`
	SubGIDsConfigured bool `json:"subGidsConfigured"`
	LingerEnabled     bool `json:"lingerEnabled"`
	RuntimePrepared   bool `json:"runtimePrepared"`
}

func (result ReconcileResult) Changed() bool {
	return result.GroupCreated || result.UserCreated || result.UserRepaired ||
		result.DirectoryCreated || result.OwnershipRepaired || result.SubUIDsConfigured ||
		result.SubGIDsConfigured || result.LingerEnabled || result.RuntimePrepared
}

type DeleteResult struct {
	UserDeleted    bool `json:"userDeleted"`
	GroupDeleted   bool `json:"groupDeleted"`
	RuntimeRemoved bool `json:"runtimeRemoved"`
	SubUIDsRemoved bool `json:"subUidsRemoved"`
	SubGIDsRemoved bool `json:"subGidsRemoved"`
	LingerDisabled bool `json:"lingerDisabled"`
}

func (result DeleteResult) Changed() bool {
	return result.UserDeleted || result.GroupDeleted || result.RuntimeRemoved ||
		result.SubUIDsRemoved || result.SubGIDsRemoved || result.LingerDisabled
}

type RuntimeResult struct {
	SubUIDsConfigured bool
	SubGIDsConfigured bool
	LingerEnabled     bool
	RuntimePrepared   bool
}

type RuntimeRemovalResult struct {
	RuntimeRemoved bool
	SubUIDsRemoved bool
	SubGIDsRemoved bool
	LingerDisabled bool
}

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type accountLookup interface {
	Load(context.Context) (accountSnapshot, error)
}

type directoryManager interface {
	Ensure(hostingidentity.Spec) (created bool, ownershipRepaired bool, err error)
	RequireArchived(hostingidentity.Spec) error
}

type runtimeManager interface {
	EnsureRuntime(context.Context, hostingidentity.Spec) (RuntimeResult, error)
	RemoveRuntime(context.Context, hostingidentity.Spec) (RuntimeRemovalResult, error)
}

type Reconciler struct {
	commands    commandRunner
	lookup      accountLookup
	directories directoryManager
	runtimes    runtimeManager
}

func NewReconciler() *Reconciler {
	return &Reconciler{
		commands: agentexec.NewRunner(), lookup: newFileAccountLookup(), directories: newDirectoryManager(),
		runtimes: newRuntimeManager(),
	}
}

func (reconciler *Reconciler) Reconcile(
	ctx context.Context,
	spec hostingidentity.Spec,
) (ReconcileResult, error) {
	result, err := reconciler.ReconcileBase(ctx, spec)
	if err != nil {
		return ReconcileResult{}, err
	}
	runtime, err := reconciler.ReconcileRuntime(ctx, spec)
	if err != nil {
		return ReconcileResult{}, err
	}
	result.SubUIDsConfigured = runtime.SubUIDsConfigured
	result.SubGIDsConfigured = runtime.SubGIDsConfigured
	result.LingerEnabled = runtime.LingerEnabled
	result.RuntimePrepared = runtime.RuntimePrepared
	return result, nil
}

// ReconcileBase creates only the Unix identity and empty account root. The
// filesystem reconciler must assign the project before ReconcileRuntime can
// create rootless container storage below it.
func (reconciler *Reconciler) ReconcileBase(
	ctx context.Context,
	spec hostingidentity.Spec,
) (ReconcileResult, error) {
	if reconciler == nil || reconciler.commands == nil || reconciler.lookup == nil || reconciler.directories == nil || reconciler.runtimes == nil {
		return ReconcileResult{}, ErrMutationFailed
	}
	if err := hostingidentity.Validate(spec); err != nil {
		return ReconcileResult{}, fmt.Errorf("%w: %v", ErrMutationFailed, err)
	}
	snapshot, err := reconciler.lookup.Load(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	groupExists, userExists, userNeedsRepair, err := inspectExpectedIdentity(snapshot, spec, false)
	if err != nil {
		return ReconcileResult{}, err
	}

	result := ReconcileResult{}
	if !groupExists {
		if err := reconciler.run(ctx, agentexec.ProfileGroupAdd, spec); err != nil {
			return ReconcileResult{}, err
		}
		result.GroupCreated = true
		if err := reconciler.verify(ctx, spec, true, userExists, false); err != nil {
			return ReconcileResult{}, err
		}
	}
	if !userExists {
		if err := reconciler.run(ctx, agentexec.ProfileUserAdd, spec); err != nil {
			return ReconcileResult{}, err
		}
		result.UserCreated = true
	} else if userNeedsRepair {
		if err := reconciler.run(ctx, agentexec.ProfileUserMod, spec); err != nil {
			return ReconcileResult{}, err
		}
		result.UserRepaired = true
	}
	if err := reconciler.verify(ctx, spec, true, true, true); err != nil {
		return ReconcileResult{}, err
	}
	result.DirectoryCreated, result.OwnershipRepaired, err = reconciler.directories.Ensure(spec)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("%w: account directory", ErrMutationFailed)
	}
	return result, nil
}

// ReconcileRuntime requires an existing exact base identity. It is deliberately
// separate so project inheritance is active before Podman storage is created.
func (reconciler *Reconciler) ReconcileRuntime(
	ctx context.Context, spec hostingidentity.Spec,
) (ReconcileResult, error) {
	if reconciler == nil || reconciler.lookup == nil || reconciler.runtimes == nil {
		return ReconcileResult{}, ErrMutationFailed
	}
	if err := hostingidentity.Validate(spec); err != nil {
		return ReconcileResult{}, fmt.Errorf("%w: %v", ErrMutationFailed, err)
	}
	snapshot, err := reconciler.lookup.Load(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	groupExists, userExists, needsRepair, err := inspectExpectedIdentity(snapshot, spec, true)
	if err != nil {
		return ReconcileResult{}, err
	}
	if !groupExists || !userExists || needsRepair {
		return ReconcileResult{}, ErrIdentityConflict
	}
	runtimeResult, err := reconciler.runtimes.EnsureRuntime(ctx, spec)
	if err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{
		SubUIDsConfigured: runtimeResult.SubUIDsConfigured,
		SubGIDsConfigured: runtimeResult.SubGIDsConfigured,
		LingerEnabled:     runtimeResult.LingerEnabled,
		RuntimePrepared:   runtimeResult.RuntimePrepared,
	}, nil
}

func (reconciler *Reconciler) Delete(
	ctx context.Context,
	spec hostingidentity.Spec,
) (DeleteResult, error) {
	if reconciler == nil || reconciler.commands == nil || reconciler.lookup == nil || reconciler.directories == nil || reconciler.runtimes == nil {
		return DeleteResult{}, ErrMutationFailed
	}
	if err := hostingidentity.Validate(spec); err != nil {
		return DeleteResult{}, fmt.Errorf("%w: %v", ErrMutationFailed, err)
	}
	if err := reconciler.directories.RequireArchived(spec); err != nil {
		return DeleteResult{}, err
	}
	snapshot, err := reconciler.lookup.Load(ctx)
	if err != nil {
		return DeleteResult{}, err
	}
	groupExists, userExists, _, err := inspectExpectedIdentity(snapshot, spec, true)
	if err != nil {
		return DeleteResult{}, err
	}
	result := DeleteResult{}
	runtimeResult, err := reconciler.runtimes.RemoveRuntime(ctx, spec)
	if err != nil {
		return DeleteResult{}, err
	}
	result.RuntimeRemoved = runtimeResult.RuntimeRemoved
	result.SubUIDsRemoved = runtimeResult.SubUIDsRemoved
	result.SubGIDsRemoved = runtimeResult.SubGIDsRemoved
	result.LingerDisabled = runtimeResult.LingerDisabled
	if userExists {
		if err := reconciler.run(ctx, agentexec.ProfileUserDel, spec); err != nil {
			return DeleteResult{}, fmt.Errorf("delete managed user: %w", err)
		}
		result.UserDeleted = true
		postDelete, err := reconciler.lookup.Load(ctx)
		if err != nil {
			return DeleteResult{}, err
		}
		remainingGroup, remainingUser, _, err := inspectExpectedIdentity(postDelete, spec, true)
		if err != nil || remainingUser {
			if err != nil {
				return DeleteResult{}, fmt.Errorf("verify managed user deletion: %w", err)
			}
			return DeleteResult{}, fmt.Errorf("verify managed user deletion: %w", ErrMutationFailed)
		}
		result.GroupDeleted = groupExists && !remainingGroup
		groupExists = remainingGroup
	}
	if groupExists {
		if err := reconciler.run(ctx, agentexec.ProfileGroupDel, spec); err != nil {
			return DeleteResult{}, fmt.Errorf("delete managed group: %w", err)
		}
		result.GroupDeleted = true
	}
	if err := reconciler.verify(ctx, spec, false, false, true); err != nil {
		return DeleteResult{}, fmt.Errorf("verify managed identity deletion: %w", err)
	}
	return result, nil
}

func (reconciler *Reconciler) run(
	ctx context.Context,
	profile agentexec.ProfileID,
	spec hostingidentity.Spec,
) error {
	result, err := reconciler.commands.Run(ctx, agentexec.Invocation{
		Profile: profile,
		Values: []string{
			spec.AccountID, spec.Username, strconv.FormatUint(uint64(spec.UID), 10),
			strconv.FormatUint(uint64(spec.GID), 10), spec.HomeDirectory,
		},
	})
	if err != nil || result.ExitCode != 0 {
		return ErrMutationFailed
	}
	return nil
}

func (reconciler *Reconciler) verify(
	ctx context.Context,
	spec hostingidentity.Spec,
	wantGroup bool,
	wantUser bool,
	requireCompleteUser bool,
) error {
	snapshot, err := reconciler.lookup.Load(ctx)
	if err != nil {
		return err
	}
	groupExists, userExists, needsRepair, err := inspectExpectedIdentity(snapshot, spec, requireCompleteUser)
	if err != nil {
		return err
	}
	if groupExists != wantGroup || userExists != wantUser ||
		requireCompleteUser && wantUser && needsRepair {
		return ErrMutationFailed
	}
	return nil
}

func inspectExpectedIdentity(
	snapshot accountSnapshot,
	spec hostingidentity.Spec,
	requireCompleteUser bool,
) (bool, bool, bool, error) {
	group, groupByName := snapshot.groupsByName[spec.Username]
	groupWithID, groupByID := snapshot.groupsByID[spec.GID]
	if groupByName && group.GID != spec.GID || groupByID && groupWithID.Name != spec.Username {
		return false, false, false, ErrIdentityConflict
	}
	user, userByName := snapshot.usersByName[spec.Username]
	userWithID, userByID := snapshot.usersByID[spec.UID]
	if userByName && (user.UID != spec.UID || user.GID != spec.GID) ||
		userByID && userWithID.Name != spec.Username {
		return false, false, false, ErrIdentityConflict
	}
	needsRepair := userByName && (user.HomeDirectory != spec.HomeDirectory || user.Shell != hostingidentity.NoLoginShell)
	if requireCompleteUser && needsRepair {
		return false, false, false, ErrIdentityConflict
	}
	return groupByName, userByName, needsRepair, nil
}
