// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostfilesystem owns Stackfort's fixed account layout, project quota
// assignment, and descriptor-relative document-root creation. It exposes no
// generic path or command operation.
package hostfilesystem

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingpath"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

var (
	ErrConflict          = errors.New("managed account filesystem conflicts with existing host state")
	ErrMigrationRequired = errors.New("managed account filesystem requires an offline project migration")
	ErrQuotaUnavailable  = errors.New("project quota capability is unavailable")
	ErrMutationFailed    = errors.New("managed account filesystem mutation failed")
)

type LayoutResult struct {
	ProjectAssigned    bool
	DirectoriesCreated []string
}

type ReconcileResult struct {
	Layout       LayoutResult
	QuotaApplied bool
	Capability   agentprotocol.Capability
}

type DocumentRootResult struct {
	RelativePath string
	Created      bool
}

type CapabilityError struct {
	Capability agentprotocol.Capability
}

func (failure *CapabilityError) Error() string { return ErrQuotaUnavailable.Error() }
func (failure *CapabilityError) Unwrap() error { return ErrQuotaUnavailable }

type capabilityInspector interface {
	InspectManagedFilesystem() agentprotocol.FilesystemCapabilities
}

type platformInspector interface {
	InspectPlatform() agentprotocol.PlatformCapabilities
}

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type directoryManager interface {
	EnsureLayout(hostingstorage.Spec) (LayoutResult, error)
	EnsureDocumentRoot(hostingidentity.Spec, string) (bool, error)
}

type Reconciler struct {
	capabilities capabilityInspector
	platform     platformInspector
	commands     commandRunner
	directories  directoryManager
}

func NewReconciler() *Reconciler {
	return &Reconciler{
		capabilities: hostcapabilities.NewInspector(), platform: hostcapabilities.NewInspector(),
		commands:    agentexec.NewRunner(),
		directories: newDirectoryManager(),
	}
}

func (reconciler *Reconciler) Reconcile(
	ctx context.Context,
	spec hostingstorage.Spec,
) (ReconcileResult, error) {
	if reconciler == nil || reconciler.capabilities == nil || reconciler.commands == nil || reconciler.directories == nil {
		return ReconcileResult{}, ErrMutationFailed
	}
	if err := hostingstorage.Validate(spec); err != nil {
		return ReconcileResult{}, fmt.Errorf("%w: invalid storage specification", ErrMutationFailed)
	}
	filesystem := reconciler.capabilities.InspectManagedFilesystem()
	if filesystem.Inspection.Status != agentprotocol.CapabilityAvailable ||
		filesystem.ProjectQuota.Status != agentprotocol.CapabilityAvailable {
		capability := filesystem.ProjectQuota
		if filesystem.Inspection.Status != agentprotocol.CapabilityAvailable {
			capability = filesystem.Inspection
		}
		return ReconcileResult{}, &CapabilityError{Capability: capability}
	}
	result, err := reconciler.commands.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfileSetProjectQuota,
		Values:  storageValues(spec),
	})
	if errors.Is(err, agentexec.ErrStart) {
		return ReconcileResult{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "quota-tool-unavailable",
		}}
	}
	if err != nil || result.ExitCode != 0 {
		return ReconcileResult{}, ErrMutationFailed
	}
	layout, err := reconciler.directories.EnsureLayout(spec)
	if err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{
		Layout: layout, QuotaApplied: true,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}, nil
}

func (reconciler *Reconciler) EnsureDocumentRoot(
	ctx context.Context,
	identity hostingidentity.Spec,
	relativePath string,
	access agentprotocol.DocumentRootAccess,
) (DocumentRootResult, error) {
	if reconciler == nil || reconciler.platform == nil || reconciler.commands == nil ||
		reconciler.directories == nil || ctx == nil {
		return DocumentRootResult{}, ErrMutationFailed
	}
	if err := hostingidentity.Validate(identity); err != nil || agentprotocol.ValidateDocumentRootAccess(access) != nil {
		return DocumentRootResult{}, fmt.Errorf("%w: invalid hosting identity", ErrMutationFailed)
	}
	normalized, err := hostingpath.NormalizeDocumentRoot(relativePath)
	if err != nil {
		return DocumentRootResult{}, fmt.Errorf("%w: invalid document root", ErrMutationFailed)
	}
	if err := ctx.Err(); err != nil {
		return DocumentRootResult{}, err
	}
	created, err := reconciler.directories.EnsureDocumentRoot(identity, normalized)
	if err != nil {
		return DocumentRootResult{}, err
	}
	platform := reconciler.platform.InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable {
		return DocumentRootResult{}, ErrMutationFailed
	}
	nginx, err := nginxbaseline.ForDistribution(platform.DistributionID)
	if err != nil {
		return DocumentRootResult{}, ErrMutationFailed
	}
	identityValues := []string{
		identity.AccountID, identity.Username,
		strconv.FormatUint(uint64(identity.UID), 10), strconv.FormatUint(uint64(identity.GID), 10),
		identity.HomeDirectory,
	}
	accesses := []struct {
		relative string
		scope    string
	}{
		{scope: "account"},
	}
	components := strings.Split(normalized, "/")
	for index := 1; index < len(components); index++ {
		accesses = append(accesses, struct {
			relative string
			scope    string
		}{relative: strings.Join(components[:index], "/"), scope: "ancestor"})
	}
	accesses = append(accesses,
		struct {
			relative string
			scope    string
		}{relative: normalized, scope: "document"},
		struct {
			relative string
			scope    string
		}{relative: normalized, scope: "default"},
	)
	for _, access := range accesses {
		values := append(append([]string(nil), identityValues...), access.relative, nginx.WorkerUser, access.scope)
		result, runErr := reconciler.commands.Run(ctx, agentexec.Invocation{
			Profile: agentexec.ProfileSetWebAccessACL, Values: values,
		})
		if runErr != nil || result.ExitCode != 0 {
			return DocumentRootResult{}, ErrMutationFailed
		}
	}
	if platform.DistributionID == "rocky" {
		if err := reconciler.ensureSELinuxWebContext(ctx, identityValues, normalized, access); err != nil {
			return DocumentRootResult{}, err
		}
	}
	return DocumentRootResult{RelativePath: normalized, Created: created}, nil
}

func (reconciler *Reconciler) ensureSELinuxWebContext(
	ctx context.Context,
	identityValues []string,
	relativePath string,
	access agentprotocol.DocumentRootAccess,
) error {
	targets := []struct {
		relative string
		scope    string
	}{
		{scope: "account"},
	}
	components := strings.Split(relativePath, "/")
	for index := 1; index < len(components); index++ {
		targets = append(targets, struct {
			relative string
			scope    string
		}{relative: strings.Join(components[:index], "/"), scope: "ancestor"})
	}
	targets = append(targets, struct {
		relative string
		scope    string
	}{relative: relativePath, scope: "document"})
	for _, contextTarget := range targets {
		values := append(append([]string(nil), identityValues...), contextTarget.relative, contextTarget.scope, string(access))
		result, err := reconciler.commands.Run(ctx, agentexec.Invocation{
			Profile: agentexec.ProfileAddSELinuxWebContext, Values: values,
		})
		if err != nil {
			return ErrMutationFailed
		}
		if result.ExitCode != 0 {
			result, err = reconciler.commands.Run(ctx, agentexec.Invocation{
				Profile: agentexec.ProfileModifySELinuxWebContext, Values: values,
			})
			if err != nil || result.ExitCode != 0 {
				return ErrMutationFailed
			}
		}
		result, err = reconciler.commands.Run(ctx, agentexec.Invocation{
			Profile: agentexec.ProfileRestoreSELinuxWebContext, Values: values,
		})
		if err != nil || result.ExitCode != 0 {
			return ErrMutationFailed
		}
	}
	return nil
}

func storageValues(spec hostingstorage.Spec) []string {
	return []string{
		spec.Identity.AccountID, spec.Identity.Username,
		strconv.FormatUint(uint64(spec.Identity.UID), 10),
		strconv.FormatUint(uint64(spec.Identity.GID), 10), spec.Identity.HomeDirectory,
		strconv.FormatUint(uint64(spec.ProjectID), 10),
		strconv.FormatUint(spec.ByteLimit, 10), strconv.FormatUint(spec.InodeLimit, 10),
	}
}
