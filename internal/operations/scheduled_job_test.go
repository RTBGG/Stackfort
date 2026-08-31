// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

func TestScheduledJobLifecycleHandlerDerivesHostIntentAndCompletesMutation(t *testing.T) {
	t.Parallel()
	accountID, actorID, operationID := testID(t), testID(t), testID(t)
	username, _ := hostingidentity.UsernameForAccount(string(accountID))
	home, _ := hostingidentity.HomeDirectoryForAccount(string(accountID))
	account := core.HostingAccount{
		ID: accountID,
		UnixIdentity: core.HostingUnixIdentity{
			AccountID: accountID, Username: username, UID: 234567, GID: 234567,
			HomeDirectory: home, State: core.HostingUnixIdentityReconciled,
		},
	}
	job := core.ScheduledJob{
		ID: operationID, AccountID: accountID, Name: "Refresh cache",
		Runtime: scheduledjobs.RuntimeShell, ScriptPath: "jobs/refresh.sh",
		Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleHourly, MinuteUTC: 12},
		Enabled:  true, Status: core.ScheduledJobPending, Revision: 1,
	}
	mutation := core.ScheduledJobMutation{
		Operation: core.Operation{
			ID: operationID, AccountID: &accountID, ActorID: &actorID,
			Kind: ScheduledJobLifecycleKind, RequestID: "scheduled-job-request",
		},
		Job: job, Action: core.ScheduledJobMutationCreate, DesiredRevision: 1,
	}
	repository := &fakeScheduledJobRepository{mutation: mutation, account: account}
	identity, _ := account.UnixIdentity.HostSpec()
	service, timer, _ := scheduledjobs.UnitNames(identity, string(job.ID))
	client := &fakeScheduledJobClient{response: agentprotocol.ScheduledJobReconcileResponse{
		JobID: string(job.ID), Changed: true, Present: true, Enabled: true,
		ServiceUnit: service, TimerUnit: timer,
		Capability: agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}}
	handler, err := NewScheduledJobLifecycleHandler(repository, client)
	if err != nil {
		t.Fatalf("NewScheduledJobLifecycleHandler: %v", err)
	}
	reporter := &fakeNGINXReporter{}
	result, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: mutation.Operation}, reporter)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 1 || client.identity.AccountID != string(accountID) || client.definition != (scheduledjobs.Definition{
		ID: string(job.ID), Runtime: job.Runtime, ScriptPath: job.ScriptPath,
		Schedule: job.Schedule, Enabled: true,
	}) || !client.present {
		t.Fatalf("derived host call = %#v", client)
	}
	if repository.completed == nil || repository.completed.OperationID != operationID ||
		result["hostChanged"] != true || result["status"] != string(core.ScheduledJobActive) {
		t.Fatalf("completion/result = %#v / %#v", repository.completed, result)
	}
	if len(reporter.stages) != 3 || reporter.stages[0] != "loading" ||
		reporter.stages[1] != "reconciling" || reporter.stages[2] != "recording" {
		t.Fatalf("progress stages = %#v", reporter.stages)
	}
}

func TestScheduledJobLifecycleHandlerRejectsSubstitutedAgentResponse(t *testing.T) {
	t.Parallel()
	accountID, operationID := testID(t), testID(t)
	username, _ := hostingidentity.UsernameForAccount(string(accountID))
	home, _ := hostingidentity.HomeDirectoryForAccount(string(accountID))
	operation := core.Operation{ID: operationID, AccountID: &accountID, Kind: ScheduledJobLifecycleKind}
	mutation := core.ScheduledJobMutation{
		Operation: operation,
		Job: core.ScheduledJob{
			ID: operationID, AccountID: accountID, Runtime: scheduledjobs.RuntimeShell,
			ScriptPath: "jobs/run.sh", Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleHourly},
			Status: core.ScheduledJobPending, Revision: 1,
		},
		Action: core.ScheduledJobMutationCreate, DesiredRevision: 1,
	}
	repository := &fakeScheduledJobRepository{
		mutation: mutation,
		account: core.HostingAccount{ID: accountID, UnixIdentity: core.HostingUnixIdentity{
			AccountID: accountID, Username: username, UID: 234568, GID: 234568, HomeDirectory: home,
		}},
	}
	client := &fakeScheduledJobClient{response: agentprotocol.ScheduledJobReconcileResponse{
		JobID: string(operationID), Present: true,
		ServiceUnit: "stackfort-job-234568-" + "00000000000000000000000000000000.service",
		TimerUnit:   "stackfort-job-234568-" + "00000000000000000000000000000000.timer",
		Capability:  agentprotocol.Capability{Status: agentprotocol.CapabilityAvailable},
	}}
	handler, _ := NewScheduledJobLifecycleHandler(repository, client)
	_, err := handler.Run(context.Background(), core.ClaimedOperation{Operation: operation}, &fakeNGINXReporter{})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "scheduled_job.agent_response_invalid" || !failure.Retryable ||
		repository.completed != nil {
		t.Fatalf("substituted response error/completion = %#v / %#v", err, repository.completed)
	}
}

func TestScheduledJobAgentFailureClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err       error
		code      string
		retryable bool
	}{
		{errors.New("socket unavailable"), "scheduled_job.agent_unavailable", true},
		{&agentclient.RemoteError{Code: agentprotocol.ErrorScheduledJobNotFound}, "scheduled_job.script_not_found", false},
		{&agentclient.RemoteError{Code: agentprotocol.ErrorScheduledJobConflict}, "scheduled_job.host_state_conflict", false},
		{&agentclient.RemoteError{Code: agentprotocol.ErrorScheduledJobUnavailable}, "scheduled_job.host_unavailable", true},
	} {
		var failure *Failure
		if err := classifyScheduledJobAgentFailure(test.err); !errors.As(err, &failure) ||
			failure.Code != test.code || failure.Retryable != test.retryable {
			t.Errorf("classification for %T = %#v", test.err, err)
		}
	}
}

type fakeScheduledJobRepository struct {
	mutation  core.ScheduledJobMutation
	account   core.HostingAccount
	completed *core.CompleteScheduledJobMutationParams
}

func (repository *fakeScheduledJobRepository) LoadScheduledJobMutation(
	context.Context, core.ID, core.ID,
) (core.ScheduledJobMutation, error) {
	return repository.mutation, nil
}

func (repository *fakeScheduledJobRepository) CompleteScheduledJobMutation(
	_ context.Context, params core.CompleteScheduledJobMutationParams,
) (core.ScheduledJobMutation, error) {
	repository.completed = &params
	repository.mutation.Job.Status = core.ScheduledJobActive
	return repository.mutation, nil
}

func (repository *fakeScheduledJobRepository) GetHostingAccount(
	context.Context, core.ID,
) (core.HostingAccount, error) {
	return repository.account, nil
}

type fakeScheduledJobClient struct {
	response   agentprotocol.ScheduledJobReconcileResponse
	err        error
	calls      int
	identity   hostingidentity.Spec
	definition scheduledjobs.Definition
	present    bool
}

func (client *fakeScheduledJobClient) ReconcileScheduledJob(
	_ context.Context, _ string, _ agentprotocol.AuditCorrelation, identity hostingidentity.Spec,
	definition scheduledjobs.Definition, present bool,
) (agentprotocol.ScheduledJobReconcileResponse, error) {
	client.calls++
	client.identity, client.definition, client.present = identity, definition, present
	return client.response, client.err
}
