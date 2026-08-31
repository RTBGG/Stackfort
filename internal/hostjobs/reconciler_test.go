// SPDX-License-Identifier: AGPL-3.0-or-later

package hostjobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

func TestReconcilerCreatesUpdatesAndDeletesDerivedTimer(t *testing.T) {
	t.Parallel()
	host := &fakeJobHost{nextChanged: true}
	reconciler := &Reconciler{
		platform: fakeJobPlatform{}, runner: host, files: host,
	}
	spec := testScheduledJobSpec()
	created, err := reconciler.Reconcile(context.Background(), spec, true)
	if err != nil || !created.Changed || !created.Present || !created.Enabled ||
		!host.files || !host.loaded || !host.enabled || !host.active {
		t.Fatalf("create result = %#v, host = %#v, error = %v", created, host, err)
	}
	if strings.Contains(strings.Join(host.actions, " "), spec.Definition.ScriptPath) {
		t.Fatal("script path crossed the fixed command runner boundary")
	}

	host.actions = nil
	unchanged, err := reconciler.Reconcile(context.Background(), spec, true)
	if err != nil || unchanged.Changed {
		t.Fatalf("idempotent result = %#v, %v", unchanged, err)
	}
	for _, action := range host.actions {
		if action != string(agentexec.ProfileSystemdShowScheduledJob) {
			t.Fatalf("idempotent action = %s", action)
		}
	}

	host.actions = nil
	host.nextChanged = true
	spec.Definition.Enabled = false
	disabled, err := reconciler.Reconcile(context.Background(), spec, true)
	if err != nil || !disabled.Changed || disabled.Enabled || host.active || host.enabled {
		t.Fatalf("disable result = %#v, host = %#v, error = %v", disabled, host, err)
	}
	host.actions = nil
	deleted, err := reconciler.Reconcile(context.Background(), spec, false)
	if err != nil || !deleted.Changed || deleted.Present || host.files || host.loaded {
		t.Fatalf("delete result = %#v, host = %#v, error = %v", deleted, host, err)
	}
	joined := strings.Join(host.actions, " ")
	if !strings.Contains(joined, string(agentexec.ProfileSystemdCleanScheduledJob)) ||
		!strings.Contains(joined, string(agentexec.ProfileSystemdDaemonReload)) {
		t.Fatalf("delete actions = %v", host.actions)
	}

	host.actions = nil
	deletedAgain, err := reconciler.Reconcile(context.Background(), spec, false)
	if err != nil || deletedAgain.Changed || len(host.actions) != 1 ||
		host.actions[0] != string(agentexec.ProfileSystemdShowScheduledJob) {
		t.Fatalf("idempotent delete = %#v, actions %v, error %v", deletedAgain, host.actions, err)
	}
}

func TestReconcilerRejectsMissingScriptBeforeWritingUnits(t *testing.T) {
	t.Parallel()
	host := &fakeJobHost{scriptError: ErrNotFound, nextChanged: true}
	reconciler := &Reconciler{platform: fakeJobPlatform{}, runner: host, files: host}
	_, err := reconciler.Reconcile(context.Background(), testScheduledJobSpec(), true)
	if !errors.Is(err, ErrNotFound) || host.files {
		t.Fatalf("missing script error = %v, files = %t", err, host.files)
	}
}

func TestReconcilerCanRemoveJobWhoseRuntimeIsUnavailableOnCurrentHost(t *testing.T) {
	t.Parallel()
	host := &fakeJobHost{}
	reconciler := &Reconciler{platform: fakeJobPlatform{}, runner: host, files: host}
	spec := testScheduledJobSpec()
	spec.Definition.Runtime = scheduledjobs.RuntimePHP
	spec.Definition.ScriptPath = "jobs/refresh.php"
	spec.Definition.PHPVersion = "8.3"
	removed, err := reconciler.Reconcile(context.Background(), spec, false)
	if err != nil || removed.Changed || removed.Present {
		t.Fatalf("remove unavailable-runtime job = %#v, %v", removed, err)
	}
}

type fakeJobPlatform struct{}

func (fakeJobPlatform) InspectPlatform() agentprotocol.PlatformCapabilities {
	return agentprotocol.PlatformCapabilities{
		DistributionID: "debian", Support: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}
}

type fakeJobHost struct {
	files       bool
	loaded      bool
	active      bool
	enabled     bool
	nextChanged bool
	scriptError error
	actions     []string
}

func (host *fakeJobHost) Run(_ context.Context, invocation agentexec.Invocation) (agentexec.Result, error) {
	host.actions = append(host.actions, string(invocation.Profile))
	switch invocation.Profile {
	case agentexec.ProfileSystemdShowScheduledJob:
		load, active, enabled := "not-found", "inactive", "disabled"
		if host.loaded {
			load = "loaded"
		}
		if host.active {
			active = "active"
		}
		if host.enabled {
			enabled = "enabled"
		}
		return agentexec.Result{Stdout: "LoadState=" + load + "\nActiveState=" + active + "\nUnitFileState=" + enabled + "\n"}, nil
	case agentexec.ProfileSystemdDaemonReload:
		host.loaded = host.files
	case agentexec.ProfileSystemdEnableScheduledJob:
		host.loaded, host.enabled, host.active = true, true, true
	case agentexec.ProfileSystemdRestartScheduledJob:
		host.active = true
	case agentexec.ProfileSystemdDisableScheduledJob:
		host.active, host.enabled = false, false
	case agentexec.ProfileSystemdCleanScheduledJob:
	default:
		return agentexec.Result{ExitCode: 1}, nil
	}
	return agentexec.Result{}, nil
}

func (host *fakeJobHost) ValidateScript(scheduledjobs.Spec) error  { return host.scriptError }
func (host *fakeJobHost) Managed(scheduledjobs.Spec) (bool, error) { return host.files, nil }
func (host *fakeJobHost) Prepare(scheduledjobs.RuntimeProfile, scheduledjobs.Spec) (*configurationChange, error) {
	previous := host.files
	changed := host.nextChanged || !host.files
	host.nextChanged = false
	host.files = true
	return &configurationChange{Changed: changed, rollback: func() error { host.files = previous; return nil }}, nil
}
func (host *fakeJobHost) Remove(scheduledjobs.Spec) (*configurationChange, error) {
	previous := host.files
	host.files = false
	return &configurationChange{Changed: previous, rollback: func() error { host.files = previous; return nil }}, nil
}
func (host *fakeJobHost) Verify(scheduledjobs.RuntimeProfile, scheduledjobs.Spec) error {
	if !host.files {
		return ErrMutation
	}
	return nil
}

func testScheduledJobSpec() scheduledjobs.Spec {
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	return scheduledjobs.Spec{
		Identity: hostingidentity.Spec{
			AccountID: accountID, Username: username, UID: 200000, GID: 200000, HomeDirectory: home,
		},
		Definition: scheduledjobs.Definition{
			ID: accountID, Runtime: scheduledjobs.RuntimeShell, ScriptPath: "jobs/refresh.sh", Enabled: true,
			Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleHourly, MinuteUTC: 15},
		},
	}
}
