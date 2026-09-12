// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

var ErrAdmissionRecovery = errors.New("installation admission requires explicit state-bound recovery")

// InstallationGate isolates public listeners while real local health checks run.
// Close must be idempotent; StopConsumers must never stop the coordinator itself.
type InstallationGate interface {
	Close(context.Context) error
	VerifyClosed(context.Context) error
	Open(context.Context) error
	VerifyOpen(context.Context) error
	StopConsumers(context.Context) error
}

type AdmissionState struct {
	SchemaVersion int              `json:"schemaVersion"`
	Plan          storageprep.Plan `json:"plan"`
	BootID        string           `json:"bootId"`
	Attempt       uint64           `json:"attempt"`
	Phase         string           `json:"phase"`
}

func (state AdmissionState) validate() error {
	if err := state.Plan.Validate(); err != nil {
		return err
	}
	if state.SchemaVersion != 1 || !validSourceOperation(state.BootID) || state.Attempt == 0 || state.Attempt > 1_000_000 ||
		(state.Phase != "checking" && state.Phase != "admitted" && state.Phase != "recovery-required") {
		return errors.New("invalid installation admission state")
	}
	return nil
}

func (state AdmissionState) encode() ([]byte, error) {
	if err := state.validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	return append(data, '\n'), err
}

func admissionDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Recovery must name both exact snapshots reviewed by the operator. It is not
// a reset flag and cannot change source, storage state, or completed stages.
type AdmissionRecovery struct {
	StateSHA256   string `json:"stateSHA256"`
	PackageSHA256 string `json:"packageSHA256"`
}

type AdmissionInspection struct {
	Exists          bool              `json:"exists"`
	PackageComplete bool              `json:"packageComplete"`
	State           AdmissionState    `json:"state"`
	Review          AdmissionRecovery `json:"review"`
}

type admissionStore interface {
	Load() (AdmissionInspection, error)
	Save(AdmissionState) error
}

type admissionCoordinator struct {
	store   admissionStore
	gate    InstallationGate
	plan    storageprep.Plan
	boot    string
	check   func(context.Context) error
	install func(context.Context) (Result, error)
}

func (coordinator admissionCoordinator) run(ctx context.Context, recovery *AdmissionRecovery) (result Result, err error) {
	if ctx == nil || coordinator.store == nil || coordinator.gate == nil || coordinator.check == nil || coordinator.install == nil {
		return result, errors.New("invalid admission invocation")
	}
	var state AdmissionState
	started, released := false, false
	// Independent cleanup survives cancellation, panic and Goexit. A service
	// supervisor must provide the same quarantine after SIGKILL/process loss.
	defer func() {
		if released {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err = errors.Join(err, coordinator.gate.Close(cleanup), coordinator.gate.VerifyClosed(cleanup), coordinator.gate.StopConsumers(cleanup))
		if started {
			state.Phase = "recovery-required"
			err = errors.Join(err, coordinator.store.Save(state))
		}
	}()
	if err = coordinator.gate.Close(ctx); err != nil {
		return result, err
	}
	if err = coordinator.gate.VerifyClosed(ctx); err != nil {
		return result, err
	}
	if err = coordinator.plan.Validate(); err != nil {
		return result, err
	}
	if !validSourceOperation(coordinator.boot) {
		return result, errors.New("invalid live boot identity")
	}
	inspection, err := coordinator.store.Load()
	if err != nil {
		return result, err
	}
	if inspection.Exists && inspection.State.Plan != coordinator.plan {
		return result, errors.New("admission belongs to another native plan")
	}
	needsRecovery := inspection.Exists && (inspection.State.Phase != "admitted" || !inspection.PackageComplete)
	if needsRecovery {
		if recovery == nil || *recovery != inspection.Review || !pinDigestPattern.MatchString(recovery.StateSHA256) || !pinDigestPattern.MatchString(recovery.PackageSHA256) {
			return result, ErrAdmissionRecovery
		}
	} else if recovery != nil {
		return result, errors.New("recovery cannot adopt fresh or admitted state")
	}
	state = AdmissionState{SchemaVersion: 1, Plan: coordinator.plan, BootID: coordinator.boot, Attempt: inspection.State.Attempt + 1, Phase: "checking"}
	if err = coordinator.store.Save(state); err != nil {
		return result, err
	}
	started = true
	if err = coordinator.check(ctx); err != nil {
		return result, err
	}
	if result, err = coordinator.install(ctx); err != nil {
		return result, err
	}
	if result.Status != InstallComplete || result.Version != coordinator.plan.Version || result.SourceDigest != coordinator.plan.SourceDigest || len(result.Stages) != len(orderedStages) {
		return result, errors.New("installation did not return the complete bound result")
	}
	for index, stage := range result.Stages {
		if stage.ID != orderedStages[index] || stage.Status != StageComplete || stage.Attempts < 1 {
			return result, errors.New("incomplete installed stage")
		}
	}
	// Recheck after all real health checks, before publishing any admission.
	if err = coordinator.check(ctx); err != nil {
		return result, err
	}
	if err = coordinator.gate.VerifyClosed(ctx); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	state.Phase = "admitted"
	if err = coordinator.store.Save(state); err != nil {
		return result, err
	}
	if err = coordinator.gate.Open(ctx); err != nil {
		return result, err
	}
	if err = coordinator.gate.VerifyOpen(ctx); err != nil {
		return result, err
	}
	released = true
	return result, nil
}
