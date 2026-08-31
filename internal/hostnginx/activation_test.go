// SPDX-License-Identifier: AGPL-3.0-or-later

package hostnginx

import (
	"context"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
)

const (
	activationTestRevision = "019c1234-5678-7abc-8def-0123456789ab"
	activationTestPrevious = "019c1234-5678-7abc-8def-0123456789ac"
	activationTestDesired  = "019c1234-5678-7abc-8def-0123456789aa"
	activationTestAccount  = "019c1234-5678-7abc-8def-0123456789ad"
)

type fakeActivationStore struct{ workspace *fakeActivationWorkspace }

func (store fakeActivationStore) Begin() (activationWorkspace, error) { return store.workspace, nil }

type fakeActivationWorkspace struct {
	current    string
	recovery   *fakeActivationRecovery
	change     *fakeActivationChange
	active     activeRevision
	stageErr   error
	stageCount int
}

func (workspace *fakeActivationWorkspace) Recover() (activationRecovery, error) {
	if workspace.recovery == nil {
		return nil, nil
	}
	workspace.recovery.workspace = workspace
	return workspace.recovery, nil
}

func (workspace *fakeActivationWorkspace) Active(candidate activationCandidate) (activeRevision, error) {
	if workspace.active.matched {
		return workspace.active, nil
	}
	return activeRevision{revisionID: workspace.current}, nil
}

func (workspace *fakeActivationWorkspace) Stage(
	_ nginxbaseline.Spec,
	candidate activationCandidate,
) (activationChange, error) {
	workspace.stageCount++
	if workspace.stageErr != nil {
		return nil, workspace.stageErr
	}
	if workspace.change == nil {
		workspace.change = &fakeActivationChange{}
	}
	workspace.change.workspace = workspace
	workspace.change.candidate = candidate.revisionID
	workspace.change.previous = workspace.current
	return workspace.change, nil
}

func (*fakeActivationWorkspace) Close() error { return nil }

type fakeActivationRecovery struct {
	workspace      *fakeActivationWorkspace
	reloadRequired bool
	completed      bool
}

func (recovery *fakeActivationRecovery) ReloadRequired() bool { return recovery.reloadRequired }
func (recovery *fakeActivationRecovery) Complete() error {
	recovery.completed = true
	recovery.workspace.recovery = nil
	return nil
}

type fakeActivationChange struct {
	workspace         *fakeActivationWorkspace
	candidate         string
	previous          string
	fail              string
	aborted           bool
	restored          bool
	rollbackCompleted bool
	committed         bool
}

func (change *fakeActivationChange) PreviousRevisionID() string { return change.previous }
func (change *fakeActivationChange) MarkValidated() error       { return change.maybeFail("validated") }
func (change *fakeActivationChange) MarkReloaded() error        { return change.maybeFail("reloaded") }
func (change *fakeActivationChange) MarkHealthy() error         { return change.maybeFail("healthy") }
func (change *fakeActivationChange) Promote() error {
	if err := change.maybeFail("promote"); err != nil {
		return err
	}
	change.workspace.current = change.candidate
	return nil
}
func (change *fakeActivationChange) Abort() error {
	change.aborted = true
	return change.maybeFail("abort")
}
func (change *fakeActivationChange) RestorePrevious() error {
	change.workspace.current = change.previous
	change.restored = true
	return change.maybeFail("restore")
}
func (change *fakeActivationChange) CompleteRollback() error {
	change.rollbackCompleted = true
	return change.maybeFail("complete-rollback")
}
func (change *fakeActivationChange) Commit() error {
	if err := change.maybeFail("commit"); err != nil {
		return err
	}
	change.committed = true
	return nil
}
func (change *fakeActivationChange) maybeFail(stage string) error {
	if change.fail == stage {
		return errors.New("injected activation failure")
	}
	return nil
}

type fakeSiteHealth struct{ fail bool }

func (health fakeSiteHealth) Check(context.Context, string, string) error {
	if health.fail {
		return errors.New("injected health failure")
	}
	return nil
}

type fakeSiteLogs struct {
	calls   int
	domains []core.NormalizedDomainName
	err     error
}

func (logs *fakeSiteLogs) Ensure(_ context.Context, _ hostingidentity.Spec, domains []core.NormalizedDomainName) error {
	logs.calls++
	logs.domains = append([]core.NormalizedDomainName(nil), domains...)
	return logs.err
}

func TestActivatePromotesValidatedHealthyRevision(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{active: true, enabled: true}
	workspace := &fakeActivationWorkspace{current: activationTestPrevious}
	activator := testActivator(runner, workspace, fakeSiteHealth{})

	result, err := activator.Activate(t.Context(), activationTestSpec(t))
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if !result.Changed || !result.ConfigurationTested || !result.ReloadPerformed ||
		!result.HealthChecked || result.ActiveRevisionID != activationTestRevision ||
		result.PreviousRevisionID != activationTestPrevious || workspace.current != activationTestRevision ||
		workspace.change == nil || !workspace.change.committed {
		t.Fatalf("result=%#v workspace=%#v change=%#v", result, workspace, workspace.change)
	}
}

func TestActivatePreparesRootOwnedLogsBeforeStaging(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{active: true, enabled: true}
	workspace := &fakeActivationWorkspace{current: activationTestPrevious}
	logs := &fakeSiteLogs{}
	activator := testActivator(runner, workspace, fakeSiteHealth{})
	activator.logs = logs
	if _, err := activator.Activate(t.Context(), activationTestSpec(t)); err != nil || logs.calls != 1 ||
		len(logs.domains) != 1 || logs.domains[0].ASCII != "activation.example" {
		t.Fatalf("err=%v logs=%#v", err, logs)
	}
	logs.err = errors.New("injected log storage conflict")
	workspace.stageCount = 0
	if _, err := activator.Activate(t.Context(), activationTestSpec(t)); !errors.Is(err, ErrActivationFailed) || workspace.stageCount != 0 {
		t.Fatalf("log failure err=%v stageCount=%d", err, workspace.stageCount)
	}
}

func TestActivateIsIdempotentAfterAgentCacheRestart(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{active: true, enabled: true}
	workspace := &fakeActivationWorkspace{
		current: activationTestRevision,
		active: activeRevision{
			matched: true, revisionID: activationTestRevision, previousRevisionID: activationTestPrevious,
		},
	}
	activator := testActivator(runner, workspace, fakeSiteHealth{})

	result, err := activator.Activate(t.Context(), activationTestSpec(t))
	if err != nil || result.Changed || result.ReloadPerformed || !result.HealthChecked || workspace.stageCount != 0 {
		t.Fatalf("result=%#v error=%v stageCount=%d", result, err, workspace.stageCount)
	}
}

func TestActivateRecoversInterruptedPromotionBeforeRetry(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{active: true, enabled: true}
	recovery := &fakeActivationRecovery{reloadRequired: true}
	workspace := &fakeActivationWorkspace{current: activationTestPrevious, recovery: recovery}
	activator := testActivator(runner, workspace, fakeSiteHealth{})

	result, err := activator.Activate(t.Context(), activationTestSpec(t))
	if err != nil || !result.RecoveryPerformed || !recovery.completed || !result.Changed ||
		runner.calls[agentexec.ProfileSystemdReloadNGINX] != 2 {
		t.Fatalf("result=%#v error=%v recovery=%#v calls=%#v", result, err, recovery, runner.calls)
	}
}

func TestActivateFailureInjectionConvergesToPreviousRevision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		changeFailure  string
		runnerFailure  map[agentexec.ProfileID]int
		healthFailure  bool
		wantValidation bool
		wantHealth     bool
		beforePromote  bool
	}{
		{name: "candidate validation", runnerFailure: map[agentexec.ProfileID]int{agentexec.ProfileNGINXTestCandidate: 1}, wantValidation: true, beforePromote: true},
		{name: "validation checkpoint", changeFailure: "validated", beforePromote: true},
		{name: "promotion", changeFailure: "promote"},
		{name: "reload", runnerFailure: map[agentexec.ProfileID]int{agentexec.ProfileSystemdReloadNGINX: 1}},
		{name: "reload checkpoint", changeFailure: "reloaded"},
		{name: "service inspection", runnerFailure: map[agentexec.ProfileID]int{agentexec.ProfileSystemctlShow: 2}},
		{name: "health", healthFailure: true, wantHealth: true},
		{name: "health checkpoint", changeFailure: "healthy"},
		{name: "commit", changeFailure: "commit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{active: true, enabled: true, failCall: test.runnerFailure}
			workspace := &fakeActivationWorkspace{
				current: activationTestPrevious, change: &fakeActivationChange{fail: test.changeFailure},
			}
			activator := testActivator(runner, workspace, fakeSiteHealth{fail: test.healthFailure})
			_, err := activator.Activate(t.Context(), activationTestSpec(t))
			if err == nil || test.wantValidation && !errors.Is(err, ErrValidationFailed) ||
				test.wantHealth && !errors.Is(err, ErrHealthCheckFailed) {
				t.Fatalf("Activate() error = %v", err)
			}
			if workspace.current != activationTestPrevious {
				t.Fatalf("active revision = %q", workspace.current)
			}
			if test.beforePromote {
				if !workspace.change.aborted {
					t.Fatal("pre-promotion failure did not abort staging")
				}
			} else if !workspace.change.restored || !workspace.change.rollbackCompleted {
				t.Fatalf("rollback state = %#v", workspace.change)
			}
		})
	}
}

func testActivator(
	runner *fakeRunner,
	workspace *fakeActivationWorkspace,
	health siteHealthChecker,
) *Activator {
	return &Activator{
		platform: fakePlatformInspector{supportedPlatform()}, runner: runner,
		store: fakeActivationStore{workspace: workspace}, health: health,
	}
}

func activationTestSpec(t *testing.T) ActivationSpec {
	t.Helper()
	username, _ := hostingidentity.UsernameForAccount(activationTestAccount)
	home, _ := hostingidentity.HomeDirectoryForAccount(activationTestAccount)
	name, err := core.NormalizeDomainName("activation.example")
	if err != nil {
		t.Fatal(err)
	}
	return ActivationSpec{
		Identity: hostingidentity.Spec{
			AccountID: activationTestAccount, Username: username,
			UID: 200_000, GID: 200_000, HomeDirectory: home,
		},
		RevisionID: activationTestRevision, DesiredStateRevisionID: activationTestDesired,
		Domains: []nginxconfig.DomainSpec{{
			Name: name, Status: core.DomainActive, CanonicalMode: core.CanonicalServeBoth,
			Target: nginxconfig.TargetSpec{Type: core.DomainTargetStatic, DocumentRoot: "public_html"},
		}},
		Options: nginxconfig.DefaultOptions(),
	}
}
