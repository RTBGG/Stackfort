// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

func TestScheduledJobMutationIsClosedAndAccountCorrelated(t *testing.T) {
	t.Parallel()
	identity := validHostingIdentitySpec()
	correlation := validIdentityAuditCorrelation()
	definition := scheduledjobs.Definition{
		ID: correlation.OperationID, Runtime: scheduledjobs.RuntimeShell, ScriptPath: "jobs/cache.sh",
		Schedule: scheduledjobs.Schedule{Kind: scheduledjobs.ScheduleHourly, MinuteUTC: 12}, Enabled: true,
	}
	request := Request{
		ProtocolVersion: WireVersion, RequestID: "job-request-1", IdempotencyKey: "job-key-1",
		Operation: OperationReconcileScheduledJob, Correlation: &correlation,
		ReconcileScheduledJob: &ScheduledJobReconcileRequest{
			Identity: identity, Definition: definition, Present: true,
		},
	}
	if err := ValidateRequest(request); err != nil {
		t.Fatalf("valid scheduled job request: %v", err)
	}
	if !RequiresAuditCorrelation(OperationReconcileScheduledJob) {
		t.Fatal("scheduled job mutation omitted its audit policy")
	}
	invalid := request
	payload := *request.ReconcileScheduledJob
	payload.Definition.ScriptPath = "jobs/cache.sh;id"
	invalid.ReconcileScheduledJob = &payload
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("raw command error = %v", err)
	}
	invalid = request
	badCorrelation := correlation
	badCorrelation.AccountID = "019c1234-5678-7abc-8def-1123456789ab"
	invalid.Correlation = &badCorrelation
	if err := ValidateRequest(invalid); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-account correlation error = %v", err)
	}

	compactID := strings.ReplaceAll(definition.ID, "-", "")
	base := "stackfort-job-200000-" + compactID
	response := Response{
		ProtocolVersion: WireVersion, RequestID: request.RequestID,
		ScheduledJob: &ScheduledJobReconcileResponse{
			JobID: definition.ID, Changed: true, Present: true, Enabled: true,
			ServiceUnit: base + ".service", TimerUnit: base + ".timer",
			Capability: Capability{Status: CapabilityAvailable},
		},
	}
	if err := ValidateResponse(response, response.RequestID, OperationReconcileScheduledJob); err != nil {
		t.Fatalf("valid scheduled job response: %v", err)
	}
	response.ScheduledJob.Present = false
	if err := ValidateResponse(response, response.RequestID, OperationReconcileScheduledJob); err == nil {
		t.Fatal("absent but enabled response was accepted")
	}
}
