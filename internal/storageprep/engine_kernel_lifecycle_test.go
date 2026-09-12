// SPDX-License-Identifier: AGPL-3.0-or-later

package storageprep

import (
	"errors"
	"strings"
	"testing"
)

func TestUpdatedKernelRequiresPreviouslyReadyAndLiveVerification(t *testing.T) {
	for _, phase := range []Phase{Planned, Arming, AwaitingReboot, Verifying, Ready} {
		t.Run(string(phase), func(t *testing.T) {
			state := State{SchemaVersion: SchemaVersion, Plan: testPlan(), Phase: phase, ArmAttempts: 1}
			if phase == Planned {
				state.ArmAttempts = 0
			}
			if phase == Verifying || phase == Ready {
				state.ResumeBootID = nextBoot
			}
			store := &memoryStore{state: state, exists: true}
			backend := newBackend(store)
			backend.observation.Kernel = "6.12.999+deb13-cloud-amd64"
			backend.observation.BootID = "10000000-0000-4000-8000-000000000098"
			result, err := Advance(t.Context(), store, backend, testPlan())
			if phase != Ready {
				if !errors.Is(err, ErrRecoveryRequired) || result.Ready || store.state.FailureCode != "identity-drift" || len(backend.calls) != 1 {
					t.Fatal("kernel changed before first verified readiness", result, err, backend.calls)
				}
				return
			}
			if err != nil || !result.Ready || store.saves != 0 || store.state != state || strings.Join(backend.calls, ",") != "observe,ready" {
				t.Fatal("normal kernel update rebound plan or skipped live verification", result, err, backend.calls)
			}
			backend.fail = "ready"
			assertRecovery(t, store, backend, "readiness-lost")
		})
	}
	for _, kernel := range []string{"", "../kernel", "6/kernel", "6;reboot", "6\nother", strings.Repeat("6", 129)} {
		store := &memoryStore{state: State{SchemaVersion: SchemaVersion, Plan: testPlan(), Phase: Ready, ArmAttempts: 1, ResumeBootID: nextBoot}, exists: true}
		backend := newBackend(store)
		backend.observation.Kernel = kernel
		backend.observation.BootID = nextBoot
		assertRecovery(t, store, backend, "identity-drift")
		if count(backend.calls, "ready") != 0 {
			t.Fatal("unsafe kernel passed to readiness backend")
		}
	}
}
