// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostupdates exposes only the fixed systemd activation and bounded
// journal inspection required by the platform update control path.
package hostupdates

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/updateapply"
)

var (
	ErrInvalid     = errors.New("platform update request is invalid")
	ErrConflict    = errors.New("platform update conflicts with active recovery state")
	ErrUnavailable = errors.New("platform update service is unavailable")
)

type commandRunner interface {
	Run(context.Context, agentexec.Invocation) (agentexec.Result, error)
}

type journalStore interface {
	Load() (updateapply.Journal, bool, error)
}

type Manager struct {
	runner commandRunner
	store  journalStore
}

func NewManager() *Manager {
	return &Manager{runner: agentexec.NewRunner(), store: updateapply.NewFileStore()}
}

func (manager *Manager) Start(ctx context.Context, version string) error {
	if manager == nil || manager.runner == nil || manager.store == nil || ctx == nil {
		return ErrUnavailable
	}
	if _, err := updateapply.ParseVersion(version); err != nil {
		return ErrInvalid
	}
	journal, exists, err := manager.store.Load()
	if err != nil {
		return ErrUnavailable
	}
	if exists {
		if err := updateapply.ValidateJournal(journal); err != nil {
			return ErrConflict
		}
		if (journal.Status == updateapply.StatusApplying || journal.Status == updateapply.StatusRollingBack ||
			journal.Status == updateapply.StatusRollbackFailed) && journal.TargetVersion != version {
			return ErrConflict
		}
	}
	result, err := manager.runner.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfileSystemdStartPlatformUpdate, Values: []string{version},
	})
	if err != nil || result.ExitCode != 0 {
		return ErrUnavailable
	}
	return nil
}

func (manager *Manager) Status(ctx context.Context) (agentprotocol.PlatformUpdateStatusResponse, error) {
	if manager == nil || manager.store == nil || ctx == nil {
		return agentprotocol.PlatformUpdateStatusResponse{}, ErrUnavailable
	}
	select {
	case <-ctx.Done():
		return agentprotocol.PlatformUpdateStatusResponse{}, ErrUnavailable
	default:
	}
	journal, exists, err := manager.store.Load()
	if err != nil {
		return agentprotocol.PlatformUpdateStatusResponse{}, ErrUnavailable
	}
	if !exists {
		return agentprotocol.PlatformUpdateStatusResponse{State: "idle"}, nil
	}
	if err := updateapply.ValidateJournal(journal); err != nil {
		return agentprotocol.PlatformUpdateStatusResponse{}, ErrConflict
	}
	return agentprotocol.PlatformUpdateStatusResponse{
		State: string(journal.Status), CurrentVersion: journal.CurrentVersion,
		TargetVersion: journal.TargetVersion, StartedAt: journal.StartedAt,
		UpdatedAt: journal.UpdatedAt, CompletedAt: journal.CompletedAt, ErrorCode: journal.ErrorCode,
	}, nil
}
