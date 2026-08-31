// SPDX-License-Identifier: AGPL-3.0-or-later

package hostnginx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostlogs"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/nginxconfig"
)

var (
	ErrHealthCheckFailed = errors.New("activated NGINX site revision failed its health check")
	ErrRecoveryFailed    = errors.New("managed NGINX site revision recovery failed")
)

type ActivationSpec struct {
	Identity               hostingidentity.Spec
	RevisionID             string
	DesiredStateRevisionID string
	Domains                []nginxconfig.DomainSpec
	Options                nginxconfig.Options
}

type ActivationResult struct {
	Changed                bool
	ConfigurationTested    bool
	ReloadPerformed        bool
	HealthChecked          bool
	RecoveryPerformed      bool
	ActiveRevisionID       string
	PreviousRevisionID     string
	DesiredStateRevisionID string
	ConfigDigest           [32]byte
	RenderedDomains        int
}

type activationCandidate struct {
	revisionID             string
	desiredStateRevisionID string
	accountID              string
	fileName               string
	content                []byte
	digest                 [32]byte
	contentDigest          [32]byte
	renderedDomains        int
}

type activeRevision struct {
	matched            bool
	revisionID         string
	previousRevisionID string
}

type activationRecovery interface {
	ReloadRequired() bool
	Complete() error
}

type activationChange interface {
	PreviousRevisionID() string
	MarkValidated() error
	Promote() error
	MarkReloaded() error
	MarkHealthy() error
	Abort() error
	RestorePrevious() error
	CompleteRollback() error
	Commit() error
}

type activationWorkspace interface {
	Recover() (activationRecovery, error)
	Active(activationCandidate) (activeRevision, error)
	Stage(nginxbaseline.Spec, activationCandidate) (activationChange, error)
	Close() error
}

type activationStore interface {
	Begin() (activationWorkspace, error)
}

type siteHealthChecker interface {
	Check(context.Context, string, string) error
}

type Activator struct {
	platform platformInspector
	runner   commandRunner
	store    activationStore
	health   siteHealthChecker
	logs     siteLogStore
}

type siteLogStore interface {
	Ensure(context.Context, hostingidentity.Spec, []core.NormalizedDomainName) error
}

func NewActivator() *Activator {
	return &Activator{
		platform: hostcapabilities.NewInspector(), runner: agentexec.NewRunner(),
		store: newActivationStore(), health: httpSiteHealthChecker{}, logs: hostlogs.NewManager(),
	}
}

func (activator *Activator) Activate(ctx context.Context, spec ActivationSpec) (ActivationResult, error) {
	if spec.RevisionID == "" || spec.DesiredStateRevisionID == "" {
		return ActivationResult{}, ErrConflict
	}
	desired, err := nginxconfig.RenderSpecs(spec.Identity, spec.Domains, spec.Options)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("%w: render account sites", ErrValidationFailed)
	}
	options := spec.Options
	options.ActivationRevisionID = spec.RevisionID
	rendered, err := nginxconfig.RenderSpecs(spec.Identity, spec.Domains, options)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("%w: render account sites", ErrValidationFailed)
	}
	if desired.FileName != rendered.FileName || desired.RenderedDomains != rendered.RenderedDomains {
		return ActivationResult{}, fmt.Errorf("%w: render activation probe", ErrValidationFailed)
	}
	platform := activator.platform.InspectPlatform()
	if platform.Support.Status != agentprotocol.CapabilityAvailable {
		return ActivationResult{}, &CapabilityError{Capability: platform.Support}
	}
	baselineSpec, err := nginxbaseline.ForDistribution(platform.DistributionID)
	if err != nil {
		return ActivationResult{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnsupported, ReasonCode: "nginx-distribution-unsupported",
		}}
	}
	if result, runErr := activator.run(ctx, agentexec.ProfileNGINXVersion); runErr != nil || result.ExitCode != 0 {
		return ActivationResult{}, &CapabilityError{Capability: agentprotocol.Capability{
			Status: agentprotocol.CapabilityUnavailable, ReasonCode: "nginx-binary-unavailable",
		}}
	}
	before, err := serviceStateWithRunner(ctx, activator.runner)
	if err != nil || before.loadState != "loaded" || !before.active || !before.enabled {
		return ActivationResult{}, fmt.Errorf("%w: managed nginx.service is not active and enabled", ErrConflict)
	}
	if activator.logs != nil {
		domains := make([]core.NormalizedDomainName, 0, len(spec.Domains))
		for _, domain := range spec.Domains {
			domains = append(domains, domain.Name)
		}
		if err := activator.logs.Ensure(ctx, spec.Identity, domains); err != nil {
			return ActivationResult{}, fmt.Errorf("%w: prepare root-owned domain logs", ErrActivationFailed)
		}
	}

	workspace, err := activator.store.Begin()
	if err != nil {
		return ActivationResult{}, fmt.Errorf("%w: begin activation workspace", ErrConflict)
	}
	defer workspace.Close()
	recovered := false
	if recovery, recoverErr := workspace.Recover(); recoverErr != nil {
		return ActivationResult{}, fmt.Errorf("%w: inspect prior transaction", ErrRecoveryFailed)
	} else if recovery != nil {
		recovered = true
		if recovery.ReloadRequired() {
			if err := activator.validateReloadAndInspect(ctx); err != nil {
				return ActivationResult{}, fmt.Errorf("%w: restore prior active revision", ErrRecoveryFailed)
			}
		}
		if err := recovery.Complete(); err != nil {
			return ActivationResult{}, fmt.Errorf("%w: complete prior transaction cleanup", ErrRecoveryFailed)
		}
	}

	candidate := activationCandidate{
		revisionID: spec.RevisionID, desiredStateRevisionID: spec.DesiredStateRevisionID,
		accountID: spec.Identity.AccountID, fileName: rendered.FileName,
		content: rendered.Content, digest: desired.Digest, contentDigest: rendered.Digest,
		renderedDomains: rendered.RenderedDomains,
	}
	active, err := workspace.Active(candidate)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("%w: inspect active site revision", ErrConflict)
	}
	healthHost := activationHealthHost(spec.Domains)
	if active.matched {
		if result, runErr := activator.run(ctx, agentexec.ProfileNGINXTestBaseline); runErr != nil || result.ExitCode != 0 {
			return ActivationResult{}, ErrValidationFailed
		}
		if err := activator.checkHealth(ctx, healthHost, spec.RevisionID); err != nil {
			return ActivationResult{}, err
		}
		return activationResult(candidate, active.previousRevisionID, false, false, recovered), nil
	}

	change, err := workspace.Stage(baselineSpec, candidate)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("%w: stage site revision", ErrActivationFailed)
	}
	abort := func(original error) error {
		if abortErr := change.Abort(); abortErr != nil {
			return errors.Join(original, fmt.Errorf("%w: abort staged revision", ErrRecoveryFailed))
		}
		return original
	}
	if result, runErr := activator.run(ctx, agentexec.ProfileNGINXTestCandidate, spec.RevisionID); runErr != nil || result.ExitCode != 0 {
		return ActivationResult{}, abort(ErrValidationFailed)
	}
	if err := change.MarkValidated(); err != nil {
		return ActivationResult{}, abort(fmt.Errorf("%w: persist validation checkpoint", ErrActivationFailed))
	}
	if err := change.Promote(); err != nil {
		return ActivationResult{}, activator.rollback(ctx, change, fmt.Errorf("%w: promote site revision", ErrActivationFailed))
	}
	if result, runErr := activator.run(ctx, agentexec.ProfileSystemdReloadNGINX); runErr != nil || result.ExitCode != 0 {
		return ActivationResult{}, activator.rollback(ctx, change, ErrActivationFailed)
	}
	if err := change.MarkReloaded(); err != nil {
		return ActivationResult{}, activator.rollback(ctx, change, fmt.Errorf("%w: persist reload checkpoint", ErrActivationFailed))
	}
	after, err := serviceStateWithRunner(ctx, activator.runner)
	if err != nil || after.loadState != "loaded" || !after.active || !after.enabled {
		return ActivationResult{}, activator.rollback(ctx, change, ErrActivationFailed)
	}
	if err := activator.checkHealth(ctx, healthHost, spec.RevisionID); err != nil {
		return ActivationResult{}, activator.rollback(ctx, change, err)
	}
	if err := change.MarkHealthy(); err != nil {
		return ActivationResult{}, activator.rollback(ctx, change, fmt.Errorf("%w: persist health checkpoint", ErrActivationFailed))
	}
	if err := change.Commit(); err != nil {
		return ActivationResult{}, activator.rollback(ctx, change, fmt.Errorf("%w: commit site revision", ErrActivationFailed))
	}
	return activationResult(candidate, change.PreviousRevisionID(), true, true, recovered), nil
}

func (activator *Activator) rollback(ctx context.Context, change activationChange, original error) error {
	if err := change.RestorePrevious(); err != nil {
		return errors.Join(original, fmt.Errorf("%w: restore active pointer", ErrRecoveryFailed))
	}
	if err := activator.validateReloadAndInspect(ctx); err != nil {
		return errors.Join(original, fmt.Errorf("%w: reload restored revision", ErrRecoveryFailed))
	}
	if err := change.CompleteRollback(); err != nil {
		return errors.Join(original, fmt.Errorf("%w: clean rolled-back revision", ErrRecoveryFailed))
	}
	return original
}

func (activator *Activator) validateReloadAndInspect(ctx context.Context) error {
	if result, err := activator.run(ctx, agentexec.ProfileNGINXTestBaseline); err != nil || result.ExitCode != 0 {
		return ErrValidationFailed
	}
	if result, err := activator.run(ctx, agentexec.ProfileSystemdReloadNGINX); err != nil || result.ExitCode != 0 {
		return ErrActivationFailed
	}
	state, err := serviceStateWithRunner(ctx, activator.runner)
	if err != nil || state.loadState != "loaded" || !state.active || !state.enabled {
		return ErrActivationFailed
	}
	return nil
}

func (activator *Activator) checkHealth(ctx context.Context, host, revisionID string) error {
	if host == "" {
		return nil
	}
	if err := activator.health.Check(ctx, host, revisionID); err != nil {
		return fmt.Errorf("%w for %s: %v", ErrHealthCheckFailed, host, err)
	}
	return nil
}

func (activator *Activator) run(
	ctx context.Context,
	profile agentexec.ProfileID,
	values ...string,
) (agentexec.Result, error) {
	return activator.runner.Run(ctx, agentexec.Invocation{Profile: profile, Values: values})
}

func activationHealthHost(domains []nginxconfig.DomainSpec) string {
	for _, domain := range domains {
		if domain.Status == "pending" || domain.Status == "active" {
			return domain.Name.ASCII
		}
	}
	return ""
}

func activationResult(
	candidate activationCandidate,
	previous string,
	changed bool,
	reloaded bool,
	recovered bool,
) ActivationResult {
	return ActivationResult{
		Changed: changed, ConfigurationTested: true, ReloadPerformed: reloaded,
		HealthChecked: true, RecoveryPerformed: recovered,
		ActiveRevisionID: candidate.revisionID, PreviousRevisionID: previous,
		DesiredStateRevisionID: candidate.desiredStateRevisionID,
		ConfigDigest:           candidate.digest, RenderedDomains: candidate.renderedDomains,
	}
}

type httpSiteHealthChecker struct{}

func (httpSiteHealthChecker) Check(ctx context.Context, host, revisionID string) error {
	// systemctl reload returns after signaling the NGINX master. A WAF-enabled
	// worker still has to compile the pinned CRS before it can accept the new
	// server name, which takes materially longer than a plain static reload on
	// small guests. Keep the retry interval short but allow a bounded five
	// seconds for that worker-generation handover.
	var lastErr error
	for attempt := 0; attempt < 50; attempt++ {
		if err := probeHTTPHost(ctx, host, revisionID); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.Join(ErrHealthCheckFailed, lastErr)
}

func probeHTTPHost(ctx context.Context, host, revisionID string) error {
	dialer := net.Dialer{Timeout: time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:80")
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := fmt.Fprintf(connection,
		"GET /.__stackfort_activation_probe__ HTTP/1.0\r\nHost: %s\r\nConnection: close\r\n\r\n", host,
	); err != nil {
		return err
	}
	reader := bufio.NewReader(connection)
	status, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(status, "HTTP/1.0 ") && !strings.HasPrefix(status, "HTTP/1.1 ") {
		return errors.New("NGINX probe returned no HTTP status")
	}
	observedRevision := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		name, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if found && strings.EqualFold(name, "X-Stackfort-Activation") {
			observedRevision = strings.TrimSpace(value)
			if observedRevision == revisionID {
				return nil
			}
		}
	}
	return fmt.Errorf("NGINX probe returned activation revision %q with %s", observedRevision, strings.TrimSpace(status))
}
