// SPDX-License-Identifier: AGPL-3.0-or-later

package storageprep

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const nextBoot = "10000000-0000-4000-8000-000000000099"

func testPlan() Plan {
	return Plan{
		OperationID: "10000000-0000-4000-8000-000000000001", Version: "0.1.0-beta.3",
		SourceDigest: strings.Repeat("a", 64), Distribution: "debian",
		MachineID:      "10000000-0000-4000-8000-000000000002",
		RootUUID:       "10000000-0000-4000-8000-000000000003",
		PartitionUUID:  "10000000-0000-4000-8000-000000000004",
		PreviousBootID: "10000000-0000-4000-8000-000000000005",
		Kernel:         "6.12.107+deb13-cloud-amd64", ManifestDigest: strings.Repeat("b", 64),
	}
}

type memoryStore struct {
	state           State
	exists          bool
	saves           int
	failAt          int
	commitOnFailure bool
}

func (store *memoryStore) Load() (State, bool, error) { return store.state, store.exists, nil }
func (store *memoryStore) Save(state State) error {
	if err := ValidateTransition(store.state, store.exists, state); err != nil {
		return err
	}
	store.saves++
	failed := store.saves == store.failAt
	if !failed || store.commitOnFailure {
		store.state, store.exists = state, true
	}
	if failed {
		return errors.New("simulated journal sync failure")
	}
	return nil
}

type fakeBackend struct {
	store       Store
	observation Observation
	calls       []string
	fail        string
}

func newBackend(store Store) *fakeBackend {
	plan := testPlan()
	return &fakeBackend{store: store, observation: Observation{
		MachineID: plan.MachineID, RootUUID: plan.RootUUID, PartitionUUID: plan.PartitionUUID,
		BootID: plan.PreviousBootID, Kernel: plan.Kernel,
	}}
}

func (backend *fakeBackend) call(name string) error {
	backend.calls = append(backend.calls, name)
	if backend.fail == name {
		return errors.New("simulated " + name + " failure")
	}
	return nil
}
func (backend *fakeBackend) Observe(context.Context) (Observation, error) {
	return backend.observation, backend.call("observe")
}
func (backend *fakeBackend) Arm(context.Context, Plan) error {
	state, exists, err := backend.store.Load()
	if err != nil || !exists || state.Phase != Arming || state.ArmAttempts != 1 {
		panic("arm preceded durable intent")
	}
	return backend.call("arm")
}
func (backend *fakeBackend) VerifyBoot(_ context.Context, plan Plan, boot string) error {
	if boot == plan.PreviousBootID || boot != backend.observation.BootID {
		panic("invalid boot proof request")
	}
	return backend.call("boot-proof")
}
func (backend *fakeBackend) Resume(context.Context, Plan, string) error {
	state, exists, err := backend.store.Load()
	if err != nil || !exists || state.Phase != Verifying || state.ResumeBootID != backend.observation.BootID {
		panic("resume preceded durable intent")
	}
	return backend.call("resume")
}
func (backend *fakeBackend) VerifyReady(context.Context, Plan) error { return backend.call("ready") }

func TestPreparationAcrossBootAndLiveReadiness(t *testing.T) {
	store := &memoryStore{}
	backend := newBackend(store)
	first, err := Advance(t.Context(), store, backend, testPlan())
	if err != nil || !first.Waiting || first.Ready || first.State.Phase != AwaitingReboot {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	for range 5 {
		waiting, err := Advance(t.Context(), store, backend, testPlan())
		if err != nil || !waiting.Waiting {
			t.Fatalf("waiting=%+v err=%v", waiting, err)
		}
	}
	if store.saves != 3 || count(backend.calls, "arm") != 1 {
		t.Fatal("same-boot rerun rearmed or changed journal")
	}
	backend.observation.BootID = nextBoot
	ready, err := Advance(t.Context(), store, backend, testPlan())
	if err != nil || !ready.Ready || ready.Waiting || ready.State.Phase != Ready {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	if count(backend.calls, "resume") != 1 {
		t.Fatal("resume was not single-shot")
	}
	backend.observation.BootID = "10000000-0000-4000-8000-000000000098"
	if result, err := Advance(t.Context(), store, backend, testPlan()); err != nil || !result.Ready {
		t.Fatalf("later boot=%+v %v", result, err)
	}
	if count(backend.calls, "resume") != 1 || count(backend.calls, "ready") != 2 {
		t.Fatal("ready phase did not use read-only live validation")
	}
	backend.fail = "ready"
	assertRecovery(t, store, backend, "readiness-lost")
}

func TestAmbiguousPersistenceNeverTriggersBlindRetry(t *testing.T) {
	// Cover both outcomes of each fsync failure: old bytes remain OR replacement
	// persisted but the caller received an error. Neither may authorize an action
	// in the failed call. New process invocations re-read the durable state.
	for _, committed := range []bool{false, true} {
		for failAt := 1; failAt <= 5; failAt++ {
			t.Run(strings.Join([]string{map[bool]string{false: "old", true: "new"}[committed], string(rune('0' + failAt))}, "-"), func(t *testing.T) {
				store := &memoryStore{failAt: failAt, commitOnFailure: committed}
				backend := newBackend(store)
				_, err := Advance(t.Context(), store, backend, testPlan())
				if failAt > 3 {
					if err != nil {
						t.Fatal(err)
					}
					backend.observation.BootID = nextBoot
					_, err = Advance(t.Context(), store, backend, testPlan())
				}
				if err == nil {
					t.Fatal("save failure hidden")
				}
				if failAt <= 2 && count(backend.calls, "arm") != 0 {
					t.Fatal("armed after failed intent save")
				}
				if failAt == 4 && count(backend.calls, "resume") != 0 {
					t.Fatal("resumed after failed intent save")
				}
				armBefore, resumeBefore := count(backend.calls, "arm"), count(backend.calls, "resume")
				store.failAt = 0
				result, retryErr := Advance(t.Context(), store, backend, testPlan())
				if failAt == 2 && committed || failAt == 3 && !committed || failAt == 4 && committed || failAt == 5 && !committed {
					if !errors.Is(retryErr, ErrRecoveryRequired) || result.Ready || result.Waiting {
						t.Fatalf("ambiguous state retried: %+v %v", result, retryErr)
					}
					if count(backend.calls, "arm") != armBefore || count(backend.calls, "resume") != resumeBefore {
						t.Fatal("ambiguous side effect repeated")
					}
				} else if retryErr != nil {
					t.Fatal(retryErr)
				}
				if count(backend.calls, "arm") > 1 || count(backend.calls, "resume") > 1 {
					t.Fatal("side effect repeated")
				}
			})
		}
	}
}

func TestFailuresAreTerminalAndProofPrecedesResume(t *testing.T) {
	for _, failure := range []struct{ call, code string }{
		{"arm", "arm-failed"}, {"boot-proof", "boot-evidence-invalid"}, {"resume", "resume-failed"}, {"ready", "readiness-lost"},
	} {
		t.Run(failure.call, func(t *testing.T) {
			store := &memoryStore{}
			backend := newBackend(store)
			if failure.call != "arm" {
				if _, err := Advance(t.Context(), store, backend, testPlan()); err != nil {
					t.Fatal(err)
				}
				backend.observation.BootID = nextBoot
			}
			backend.fail = failure.call
			assertRecovery(t, store, backend, failure.code)
			if failure.call == "boot-proof" && count(backend.calls, "resume") != 0 {
				t.Fatal("invalid proof reached resume")
			}
		})
	}
}

func TestPlanCannotBeReboundAndHostDriftBlocksMutation(t *testing.T) {
	for _, field := range []string{"OperationID", "Version", "SourceDigest", "MachineID", "RootUUID", "PartitionUUID", "PreviousBootID", "Kernel", "ManifestDigest"} {
		t.Run(field, func(t *testing.T) {
			store := &memoryStore{}
			backend := newBackend(store)
			if _, err := Advance(t.Context(), store, backend, testPlan()); err != nil {
				t.Fatal(err)
			}
			plan := testPlan()
			value := reflect.ValueOf(&plan).Elem().FieldByName(field)
			replacement := nextBoot
			switch field {
			case "Version":
				replacement = "0.1.0-beta.4"
			case "SourceDigest", "ManifestDigest":
				replacement = strings.Repeat("f", 64)
			case "Kernel":
				replacement = "6.12.999"
			}
			value.SetString(replacement)
			before, calls := store.state, len(backend.calls)
			if _, err := Advance(t.Context(), store, backend, plan); err == nil {
				t.Fatal("rebound plan accepted")
			}
			if before != store.state || calls != len(backend.calls) {
				t.Fatal("rebound plan touched state or backend")
			}
		})
	}
	for _, field := range []string{"MachineID", "RootUUID", "PartitionUUID", "Kernel", "BootID"} {
		t.Run("observed-"+field, func(t *testing.T) {
			store := &memoryStore{}
			backend := newBackend(store)
			if _, err := Advance(t.Context(), store, backend, testPlan()); err != nil {
				t.Fatal(err)
			}
			reflect.ValueOf(&backend.observation).Elem().FieldByName(field).SetString("drift")
			assertRecovery(t, store, backend, "identity-drift")
		})
	}
}

func TestInvalidInputsAndReadFailureHaveNoSideEffects(t *testing.T) {
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Distribution = "rocky" }, func(p *Plan) { p.Distribution = "ubuntu" },
		func(p *Plan) { p.ManifestDigest = "missing" }, func(p *Plan) { p.Kernel = "6;reboot" },
		func(p *Plan) { p.RootUUID = "/dev/sda1" }, func(p *Plan) { p.OperationID = "00000000-0000-0000-0000-000000000000" },
	} {
		store := &memoryStore{}
		backend := newBackend(store)
		plan := testPlan()
		mutate(&plan)
		if _, err := Advance(t.Context(), store, backend, plan); err == nil {
			t.Fatal("invalid plan accepted")
		}
		if store.exists || len(backend.calls) != 0 {
			t.Fatal("invalid plan caused action")
		}
	}
	store := &memoryStore{}
	backend := newBackend(store)
	backend.fail = "observe"
	if _, err := Advance(t.Context(), store, backend, testPlan()); err == nil || store.exists {
		t.Fatal("failed inspection changed state")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	backend.calls = nil
	if _, err := Advance(ctx, store, backend, testPlan()); !errors.Is(err, context.Canceled) || len(backend.calls) != 0 {
		t.Fatal("cancellation ignored")
	}
}

func TestJournalCannotSkipOrResetPhases(t *testing.T) {
	planned := State{SchemaVersion: SchemaVersion, Plan: testPlan(), Phase: Planned}
	arming := planned
	arming.Phase = Arming
	arming.ArmAttempts = 1
	waiting := arming
	waiting.Phase = AwaitingReboot
	verifying := waiting
	verifying.Phase = Verifying
	verifying.ResumeBootID = nextBoot
	ready := verifying
	ready.Phase = Ready
	recovery := waiting
	recovery.Phase = RecoveryRequired
	recovery.FailureCode = "interrupted-arm"
	for _, state := range []State{planned, arming, waiting, verifying, ready, recovery} {
		if err := state.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range [][2]State{{planned, ready}, {planned, waiting}, {waiting, ready}, {arming, planned}, {ready, waiting}, {recovery, planned}, {recovery, ready}} {
		if err := ValidateTransition(pair[0], true, pair[1]); err == nil {
			t.Fatalf("unsafe %s -> %s", pair[0].Phase, pair[1].Phase)
		}
	}
	if err := ValidateTransition(State{}, false, ready); err == nil {
		t.Fatal("new journal started ready")
	}
	for _, mutate := range []func(*State){func(s *State) { s.SchemaVersion++ }, func(s *State) { s.ArmAttempts = 2 }, func(s *State) { s.Phase = "unknown" }, func(s *State) { s.FailureCode = "secret error" }, func(s *State) { s.ResumeBootID = s.Plan.PreviousBootID }} {
		bad := ready
		mutate(&bad)
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid state accepted: %+v", bad)
		}
	}
}

func TestUnarmedRebootAndFailedRecoveryPersistence(t *testing.T) {
	store := &memoryStore{}
	planned := State{SchemaVersion: SchemaVersion, Plan: testPlan(), Phase: Planned}
	if err := store.Save(planned); err != nil {
		t.Fatal(err)
	}
	backend := newBackend(store)
	backend.observation.BootID = nextBoot
	assertRecovery(t, store, backend, "unexpected-boot")
	if count(backend.calls, "arm") != 0 {
		t.Fatal("unarmed reboot led to arming")
	}

	store = &memoryStore{failAt: 3} // Failure saving recovery after a failed Arm.
	backend = newBackend(store)
	backend.fail = "arm"
	if _, err := Advance(t.Context(), store, backend, testPlan()); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatal(err)
	}
	if store.state.Phase != Arming {
		t.Fatal("failed recovery save hid the durable arming latch")
	}
	store.failAt, backend.fail = 0, ""
	assertRecovery(t, store, backend, "interrupted-arm")
	if count(backend.calls, "arm") != 1 {
		t.Fatal("recovery write failure repeated Arm")
	}
}

func assertRecovery(t *testing.T, store *memoryStore, backend *fakeBackend, code string) {
	t.Helper()
	result, err := Advance(t.Context(), store, backend, testPlan())
	if !errors.Is(err, ErrRecoveryRequired) || result.Ready || result.Waiting || store.state.Phase != RecoveryRequired || store.state.FailureCode != code {
		t.Fatalf("recovery=%+v err=%v", result, err)
	}
	calls, saves := len(backend.calls), store.saves
	for range 3 {
		if _, err := Advance(t.Context(), store, backend, testPlan()); !errors.Is(err, ErrRecoveryRequired) {
			t.Fatal(err)
		}
	}
	if calls != len(backend.calls) || saves != store.saves {
		t.Fatal("terminal recovery retried backend or rewrote evidence")
	}
}

func count(values []string, wanted string) int {
	n := 0
	for _, value := range values {
		if value == wanted {
			n++
		}
	}
	return n
}
