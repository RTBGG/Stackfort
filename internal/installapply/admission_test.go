// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memoryAdmissionStore struct {
	inspection AdmissionInspection
	failPhase  string
}

func (store *memoryAdmissionStore) Load() (AdmissionInspection, error) { return store.inspection, nil }
func (store *memoryAdmissionStore) Save(state AdmissionState) error {
	if state.Phase == store.failPhase {
		return errors.New("injected journal sync failure")
	}
	data, err := state.encode()
	if err != nil {
		return err
	}
	store.inspection.Exists, store.inspection.State = true, state
	store.inspection.Review.StateSHA256 = admissionDigest(data)
	return nil
}

type fakeAdmissionGate struct {
	open, stopped                                bool
	closeFailure, openFailure, verifyOpenFailure bool
	openCalls                                    int
	store                                        *memoryAdmissionStore
}

func (gate *fakeAdmissionGate) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if gate.closeFailure {
		gate.closeFailure = false
		return errors.New("close failed")
	}
	gate.open = false
	return nil
}
func (gate *fakeAdmissionGate) VerifyClosed(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if gate.open {
		return errors.New("gate opened early")
	}
	return nil
}
func (gate *fakeAdmissionGate) Open(ctx context.Context) error {
	gate.openCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if gate.store.inspection.State.Phase != "admitted" {
		return errors.New("opened before durable admission")
	}
	gate.open = true
	if gate.openFailure {
		return errors.New("open acknowledgement lost")
	}
	return nil
}
func (gate *fakeAdmissionGate) VerifyOpen(context.Context) error {
	if !gate.open || gate.verifyOpenFailure {
		return errors.New("open verification failed")
	}
	return nil
}
func (gate *fakeAdmissionGate) StopConsumers(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	gate.stopped = true
	return nil
}

func admissionFixture(t *testing.T) (admissionCoordinator, *memoryAdmissionStore, *fakeAdmissionGate, Result) {
	t.Helper()
	plan := pinPlan(testSourcePin())
	store := &memoryAdmissionStore{inspection: AdmissionInspection{PackageComplete: true, Review: AdmissionRecovery{PackageSHA256: strings.Repeat("b", 64)}}}
	gate := &fakeAdmissionGate{store: store}
	result := Result{Version: plan.Version, SourceDigest: plan.SourceDigest, Status: InstallComplete}
	for _, id := range orderedStages {
		result.Stages = append(result.Stages, StageState{ID: id, Status: StageComplete, Attempts: 1})
	}
	coordinator := admissionCoordinator{store: store, gate: gate, plan: plan, boot: "30000000-0000-4000-8000-000000000099",
		check: gate.VerifyClosed, install: func(ctx context.Context) (Result, error) { return result, gate.VerifyClosed(ctx) }}
	return coordinator, store, gate, result
}

func TestAdmissionOpensOnlyAfterDurableChecksAndRechecksEveryBoot(t *testing.T) {
	coordinator, store, gate, _ := admissionFixture(t)
	for attempt := uint64(1); attempt <= 2; attempt++ {
		if _, err := coordinator.run(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		if !gate.open || gate.stopped || store.inspection.State.Phase != "admitted" || store.inspection.State.Attempt != attempt {
			t.Fatal("bad admission", store.inspection)
		}
		coordinator.boot = "30000000-0000-4000-8000-000000000098"
	}
}

func TestAdmissionFailuresNeverLeavePublicListenersOpen(t *testing.T) {
	for _, scenario := range []string{"check-first", "check-final", "install", "result", "stage", "save-check", "save-admit", "close", "open", "verify-open", "drift", "cancel", "panic"} {
		t.Run(scenario, func(t *testing.T) {
			coordinator, store, gate, result := admissionFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			checks := 0
			coordinator.check = func(ctx context.Context) error {
				checks++
				if (scenario == "check-first" && checks == 1) || (scenario == "check-final" && checks == 2) {
					return errors.New("proof changed")
				}
				return gate.VerifyClosed(ctx)
			}
			coordinator.install = func(context.Context) (Result, error) {
				switch scenario {
				case "install":
					return Result{}, errors.New("installation failed")
				case "result":
					result.Status = InstallApplying
				case "stage":
					result.Stages[0].Status = StagePending
				case "drift":
					gate.open = true
				case "cancel":
					cancel()
				case "panic":
					panic("simulated process unwind")
				}
				return result, nil
			}
			switch scenario {
			case "save-check":
				store.failPhase = "checking"
			case "save-admit":
				store.failPhase = "admitted"
			case "close":
				gate.closeFailure = true
			case "open":
				gate.openFailure = true
			case "verify-open":
				gate.verifyOpenFailure = true
			}
			func() {
				defer func() {
					if recovered := recover(); recovered != nil && scenario != "panic" {
						t.Fatal(recovered)
					}
				}()
				if _, err := coordinator.run(ctx, nil); err == nil {
					t.Fatal("accepted failure")
				}
			}()
			if gate.open || !gate.stopped {
				t.Fatal("failed to quarantine after", scenario)
			}
			if store.inspection.Exists && store.inspection.State.Phase != "recovery-required" {
				t.Fatal("failure not durable")
			}
		})
	}
}

func TestAdmissionRecoveryRequiresExactStateAndPackageReview(t *testing.T) {
	for _, phase := range []string{"checking", "recovery-required", "admitted"} {
		t.Run(phase, func(t *testing.T) {
			coordinator, store, gate, _ := admissionFixture(t)
			state := AdmissionState{SchemaVersion: 1, Plan: coordinator.plan, BootID: coordinator.boot, Attempt: 1, Phase: phase}
			if err := store.Save(state); err != nil {
				t.Fatal(err)
			}
			if phase == "admitted" {
				store.inspection.PackageComplete = false
			}
			before := store.inspection
			for _, request := range []*AdmissionRecovery{nil, {}, {StateSHA256: strings.Repeat("a", 64), PackageSHA256: before.Review.PackageSHA256}, {StateSHA256: before.Review.StateSHA256, PackageSHA256: strings.Repeat("c", 64)}} {
				if _, err := coordinator.run(t.Context(), request); !errors.Is(err, ErrAdmissionRecovery) {
					t.Fatal("stale recovery accepted", err)
				}
				if store.inspection != before || gate.openCalls != 0 {
					t.Fatal("denied recovery mutated journal or opened gate")
				}
			}
			if _, err := coordinator.run(t.Context(), &before.Review); err != nil {
				t.Fatal("reviewed recovery", err)
			}
			if store.inspection.State.Attempt != 2 || !gate.open {
				t.Fatal("recovery not recorded")
			}
			if _, err := coordinator.run(t.Context(), &before.Review); err == nil {
				t.Fatal("replayed recovery approval")
			}
		})
	}
}

func TestAdmissionRejectsForeignPlanAndFreshRecovery(t *testing.T) {
	coordinator, store, gate, _ := admissionFixture(t)
	if _, err := coordinator.run(t.Context(), &AdmissionRecovery{}); err == nil {
		t.Fatal("fresh recovery adopted")
	}
	state := AdmissionState{SchemaVersion: 1, Plan: coordinator.plan, BootID: coordinator.boot, Attempt: 1, Phase: "admitted"}
	state.Plan.OperationID = "30000000-0000-4000-8000-000000000098"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.run(t.Context(), nil); err == nil || gate.open {
		t.Fatal("foreign plan accepted")
	}
}
