// SPDX-License-Identifier: AGPL-3.0-or-later

package storageprep

import (
	"context"
	"errors"
	"fmt"
)

// Store must hold the installation lock for the entire Advance call. Save must
// durably replace the state (file sync, atomic rename, directory sync). A failed
// save has an unknown outcome and must never be followed by a side effect.
type Store interface {
	Load() (State, bool, error)
	Save(State) error
}

type Observation struct {
	MachineID     string
	RootUUID      string
	PartitionUUID string
	BootID        string
	Kernel        string
}

// Backend is an internal qualification seam, NOT a caller-configurable command
// runner. Observe/VerifyBoot/VerifyReady are read-only. Arm must stage and verify
// the pinned artifacts without rebooting. Resume must check current-boot proof
// before enabling quotas/persisting configuration; it cannot reconvert a disk.
// VerifyReady must re-read identity/geometry, managed mounts/configuration and
// kernel quota accounting AND enforcement, not trust a saved ready flag.
// No production implementation is registered.
type Backend interface {
	Observe(context.Context) (Observation, error)
	Arm(context.Context, Plan) error
	VerifyBoot(context.Context, Plan, string) error
	Resume(context.Context, Plan, string) error
	VerifyReady(context.Context, Plan) error
}

type Decision struct {
	State State
	// Waiting is a status, not permission to repeatedly request a reboot. The
	// future boot dispatcher needs its own acknowledged, bounded handoff.
	Waiting bool
	Ready   bool
}

// Advance records intent BEFORE a potentially mutating callback and observes
// the host afresh on every invocation. An ambiguous arm/resume is terminal: no
// repeated staging, automatic reset, rollback, or blind resume retry. This
// control-plane latch does not make an initramfs metadata operation crash-safe.
func Advance(ctx context.Context, store Store, backend Backend, plan Plan) (Decision, error) {
	if ctx == nil || store == nil || backend == nil {
		return Decision{}, errors.New("invalid storage preparation invocation")
	}
	if err := plan.Validate(); err != nil {
		return Decision{}, err
	}
	state, exists, err := store.Load()
	if err != nil {
		return Decision{}, err
	}
	if exists {
		if err := state.Validate(); err != nil {
			return Decision{}, err
		}
		if state.Plan != plan {
			return Decision{}, errors.New("storage journal cannot be rebound to another operation, host, source or manifest")
		}
		if state.Phase == RecoveryRequired {
			return Decision{State: state}, ErrRecoveryRequired
		}
	} else {
		state = State{SchemaVersion: SchemaVersion, Plan: plan, Phase: Planned}
	}
	if err := ctx.Err(); err != nil {
		return Decision{State: state}, err
	}
	observation, err := backend.Observe(ctx)
	if err != nil {
		return Decision{State: state}, fmt.Errorf("inspect storage preparation host: %w", err)
	}
	// Kernel identity is an immutable conversion input until the first verified
	// Ready transition. Later OS kernel updates are decided by the backend's
	// live readiness policy, never by rebinding the historical conversion plan.
	if observation.MachineID != plan.MachineID || observation.RootUUID != plan.RootUUID ||
		observation.PartitionUUID != plan.PartitionUUID || !kernelPattern.MatchString(observation.Kernel) ||
		(state.Phase != Ready && observation.Kernel != plan.Kernel) ||
		!canonicalUUID(observation.BootID) {
		if !exists {
			return Decision{}, errors.New("native storage plan does not match the current host")
		}
		return recoverState(store, state, "identity-drift", nil)
	}
	if !exists {
		if observation.BootID != plan.PreviousBootID {
			return Decision{}, errors.New("new storage preparation requires its original boot")
		}
		if err := store.Save(state); err != nil {
			return Decision{State: state}, err
		}
	}
	switch state.Phase {
	case Planned:
		if observation.BootID != plan.PreviousBootID {
			return recoverState(store, state, "unexpected-boot", nil)
		}
		state.Phase, state.ArmAttempts = Arming, 1
		if err := store.Save(state); err != nil {
			return Decision{State: state}, err
		}
		if err := ctx.Err(); err != nil {
			return recoverState(store, state, "arm-failed", err)
		}
		if err := backend.Arm(ctx, plan); err != nil {
			return recoverState(store, state, "arm-failed", err)
		}
		state.Phase = AwaitingReboot
		if err := store.Save(state); err != nil {
			return Decision{State: state}, err
		}
		return Decision{State: state, Waiting: true}, nil
	case Arming:
		return recoverState(store, state, "interrupted-arm", nil)
	case AwaitingReboot:
		if observation.BootID == plan.PreviousBootID {
			return Decision{State: state, Waiting: true}, nil
		}
		if err := backend.VerifyBoot(ctx, plan, observation.BootID); err != nil {
			return recoverState(store, state, "boot-evidence-invalid", err)
		}
		state.Phase, state.ResumeBootID = Verifying, observation.BootID
		if err := store.Save(state); err != nil {
			return Decision{State: state}, err
		}
		if err := ctx.Err(); err != nil {
			return recoverState(store, state, "resume-failed", err)
		}
		if err := backend.Resume(ctx, plan, observation.BootID); err != nil {
			return recoverState(store, state, "resume-failed", err)
		}
		if err := backend.VerifyReady(ctx, plan); err != nil {
			return recoverState(store, state, "readiness-lost", err)
		}
		state.Phase = Ready
		if err := store.Save(state); err != nil {
			return Decision{State: state}, err
		}
		return Decision{State: state, Ready: true}, nil
	case Verifying:
		return recoverState(store, state, "interrupted-resume", nil)
	case Ready:
		// Ready records a past success, not permanent permission to serve data.
		if err := backend.VerifyReady(ctx, plan); err != nil {
			return recoverState(store, state, "readiness-lost", err)
		}
		return Decision{State: state, Ready: true}, nil
	default:
		return Decision{}, errors.New("unsupported storage preparation phase")
	}
}

func recoverState(store Store, state State, code string, cause error) (Decision, error) {
	state.Phase, state.FailureCode = RecoveryRequired, code
	return Decision{State: state}, errors.Join(ErrRecoveryRequired, cause, store.Save(state))
}
