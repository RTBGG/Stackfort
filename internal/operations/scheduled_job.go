// SPDX-License-Identifier: AGPL-3.0-or-later

package operations

import (
	"context"
	"errors"

	"github.com/RTBGG/stackfort/internal/agentclient"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

const ScheduledJobLifecycleKind = "scheduled_job.lifecycle.apply"

type ScheduledJobRepository interface {
	LoadScheduledJobMutation(context.Context, core.ID, core.ID) (core.ScheduledJobMutation, error)
	CompleteScheduledJobMutation(context.Context, core.CompleteScheduledJobMutationParams) (core.ScheduledJobMutation, error)
	GetHostingAccount(context.Context, core.ID) (core.HostingAccount, error)
}

type ScheduledJobClient interface {
	ReconcileScheduledJob(
		context.Context, string, agentprotocol.AuditCorrelation, hostingidentity.Spec,
		scheduledjobs.Definition, bool,
	) (agentprotocol.ScheduledJobReconcileResponse, error)
}

type ScheduledJobLifecycleHandler struct {
	repository ScheduledJobRepository
	client     ScheduledJobClient
}

func NewScheduledJobLifecycleHandler(
	repository ScheduledJobRepository, client ScheduledJobClient,
) (*ScheduledJobLifecycleHandler, error) {
	if repository == nil || client == nil {
		return nil, errors.New("scheduled job lifecycle handler requires a repository and agent client")
	}
	return &ScheduledJobLifecycleHandler{repository: repository, client: client}, nil
}

func (handler *ScheduledJobLifecycleHandler) Run(
	ctx context.Context, claimed core.ClaimedOperation, reporter ProgressReporter,
) (map[string]any, error) {
	operation := claimed.Operation
	if operation.Kind != ScheduledJobLifecycleKind || operation.AccountID == nil || reporter == nil {
		return nil, &Failure{Code: "scheduled_job.lifecycle_operation_invalid"}
	}
	if err := reporter.Checkpoint(ctx, "loading", 10, "scheduled_job.lifecycle.loading", nil); err != nil {
		return nil, err
	}
	mutation, err := handler.repository.LoadScheduledJobMutation(ctx, *operation.AccountID, operation.ID)
	if err != nil {
		return nil, classifyScheduledJobRepositoryFailure(err)
	}
	if mutation.AppliedAt != nil {
		return scheduledJobResult(mutation), nil
	}
	account, err := handler.repository.GetHostingAccount(ctx, *operation.AccountID)
	if err != nil {
		return nil, classifyScheduledJobRepositoryFailure(err)
	}
	identity, err := account.UnixIdentity.HostSpec()
	if err != nil || identity.AccountID != string(*operation.AccountID) {
		return nil, &Failure{Code: "scheduled_job.host_identity_invalid"}
	}
	definition := scheduledjobs.Definition{
		ID: string(mutation.Job.ID), Runtime: mutation.Job.Runtime,
		ScriptPath: mutation.Job.ScriptPath, PHPVersion: mutation.Job.PHPVersion,
		Schedule: mutation.Job.Schedule, Enabled: mutation.Job.Enabled,
	}
	present := mutation.Action != core.ScheduledJobMutationDelete
	if err := reporter.Checkpoint(ctx, "reconciling", 35, "scheduled_job.lifecycle.reconciling", map[string]any{
		"action": mutation.Action, "revision": mutation.DesiredRevision,
	}); err != nil {
		return nil, err
	}
	response, err := handler.client.ReconcileScheduledJob(
		ctx, "scheduled-job-"+string(operation.ID), lifecycleCorrelation(operation), identity, definition, present,
	)
	if err != nil {
		return nil, classifyScheduledJobAgentFailure(err)
	}
	service, timer, _ := scheduledjobs.UnitNames(identity, definition.ID)
	if response.JobID != definition.ID || response.Present != present ||
		response.Enabled != (present && definition.Enabled) || response.ServiceUnit != service || response.TimerUnit != timer {
		return nil, &Failure{Code: "scheduled_job.agent_response_invalid", Retryable: true}
	}
	if err := reporter.Checkpoint(ctx, "recording", 85, "scheduled_job.lifecycle.recording", nil); err != nil {
		return nil, err
	}
	mutation, err = handler.repository.CompleteScheduledJobMutation(ctx, core.CompleteScheduledJobMutationParams{
		AccountID: *operation.AccountID, OperationID: operation.ID,
		ActorID: operation.ActorID, RequestID: operation.RequestID,
	})
	if err != nil {
		return nil, classifyScheduledJobRepositoryFailure(err)
	}
	result := scheduledJobResult(mutation)
	result["hostChanged"] = response.Changed
	return result, nil
}

func scheduledJobResult(mutation core.ScheduledJobMutation) map[string]any {
	return map[string]any{
		"jobId": string(mutation.Job.ID), "action": string(mutation.Action),
		"revision": mutation.DesiredRevision, "status": string(mutation.Job.Status),
	}
}

func classifyScheduledJobRepositoryFailure(err error) error {
	switch {
	case errors.Is(err, core.ErrInvalidInput), errors.Is(err, core.ErrNotFound):
		return &Failure{Code: "scheduled_job.lifecycle_state_invalid"}
	case errors.Is(err, core.ErrConflict):
		return &Failure{Code: "scheduled_job.lifecycle_state_conflict"}
	default:
		return &Failure{Code: "scheduled_job.lifecycle_state_unavailable", Retryable: true}
	}
}

func classifyScheduledJobAgentFailure(err error) error {
	var remote *agentclient.RemoteError
	if !errors.As(err, &remote) {
		return &Failure{Code: "scheduled_job.agent_unavailable", Retryable: true}
	}
	switch remote.Code {
	case agentprotocol.ErrorScheduledJobInvalid:
		return &Failure{Code: "scheduled_job.host_intent_invalid"}
	case agentprotocol.ErrorScheduledJobNotFound:
		return &Failure{Code: "scheduled_job.script_not_found"}
	case agentprotocol.ErrorScheduledJobConflict:
		return &Failure{Code: "scheduled_job.host_state_conflict"}
	case agentprotocol.ErrorScheduledJobUnavailable:
		return &Failure{Code: "scheduled_job.host_unavailable", Retryable: true}
	default:
		return &Failure{Code: "scheduled_job.host_mutation_failed", Retryable: true}
	}
}
