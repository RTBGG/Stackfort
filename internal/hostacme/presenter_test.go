// SPDX-License-Identifier: AGPL-3.0-or-later

package hostacme

import (
	"context"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
)

const presenterTestOperationID = "019c1234-5678-7abc-8def-0123456789ab"

func TestPresenterReconcilesClosedIntentAndRockySELinuxContext(t *testing.T) {
	t.Parallel()
	token := "0123456789abcdefghijkl"
	storage := &fakeStorage{result: Result{Changed: true, Presented: true}}
	commands := &fakeCommandRunner{results: []agentexec.Result{{ExitCode: 1}, {ExitCode: 0}, {ExitCode: 0}}}
	presenter := &Presenter{
		storage: storage, platform: fakePlatformInspector{platform: agentprotocol.PlatformCapabilities{
			DistributionID: "rocky", Support: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
		}}, commands: commands,
	}
	result, err := presenter.Reconcile(context.Background(), presenterTestOperationID, acmehttp01.Intent{
		Action: acmehttp01.ActionPresent, Token: token,
		KeyAuthorization: token + "." + strings.Repeat("a", 43),
	})
	if err != nil || !result.Changed || !result.Presented || storage.calls != 1 {
		t.Fatalf("present result = %#v, calls=%d, error=%v", result, storage.calls, err)
	}
	want := []agentexec.ProfileID{
		agentexec.ProfileAddSELinuxACMEContext,
		agentexec.ProfileModifySELinuxACMEContext,
		agentexec.ProfileRestoreSELinuxACMEContext,
	}
	if len(commands.invocations) != len(want) {
		t.Fatalf("SELinux invocations = %#v", commands.invocations)
	}
	for index, profile := range want {
		if commands.invocations[index].Profile != profile || len(commands.invocations[index].Values) != 0 {
			t.Fatalf("SELinux invocation %d = %#v", index, commands.invocations[index])
		}
	}
}

func TestPresenterRejectsMalformedIntentBeforeStorage(t *testing.T) {
	t.Parallel()
	storage := &fakeStorage{}
	presenter := &Presenter{
		storage: storage, platform: fakePlatformInspector{}, commands: &fakeCommandRunner{},
	}
	if _, err := presenter.Reconcile(context.Background(), presenterTestOperationID, acmehttp01.Intent{
		Action: acmehttp01.ActionPresent, Token: "../escape", KeyAuthorization: "raw",
	}); err == nil || storage.calls != 0 {
		t.Fatalf("malformed intent reached storage: calls=%d error=%v", storage.calls, err)
	}
}

type fakeStorage struct {
	calls  int
	result Result
	err    error
}

func (storage *fakeStorage) Reconcile(
	context.Context,
	string,
	acmehttp01.Intent,
) (Result, error) {
	storage.calls++
	return storage.result, storage.err
}

type fakePlatformInspector struct {
	platform agentprotocol.PlatformCapabilities
}

func (inspector fakePlatformInspector) InspectPlatform() agentprotocol.PlatformCapabilities {
	return inspector.platform
}

type fakeCommandRunner struct {
	invocations []agentexec.Invocation
	results     []agentexec.Result
}

func (runner *fakeCommandRunner) Run(
	_ context.Context,
	invocation agentexec.Invocation,
) (agentexec.Result, error) {
	runner.invocations = append(runner.invocations, invocation)
	if len(runner.results) == 0 {
		return agentexec.Result{}, nil
	}
	result := runner.results[0]
	runner.results = runner.results[1:]
	return result, nil
}
