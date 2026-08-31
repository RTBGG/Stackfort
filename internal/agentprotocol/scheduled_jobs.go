// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"regexp"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/scheduledjobs"
)

type ScheduledJobReconcileRequest struct {
	Identity   hostingidentity.Spec     `json:"identity"`
	Definition scheduledjobs.Definition `json:"definition"`
	Present    bool                     `json:"present"`
}

type ScheduledJobReconcileResponse struct {
	JobID       string     `json:"jobId"`
	Changed     bool       `json:"changed"`
	Present     bool       `json:"present"`
	Enabled     bool       `json:"enabled"`
	ServiceUnit string     `json:"serviceUnit"`
	TimerUnit   string     `json:"timerUnit"`
	Capability  Capability `json:"capability"`
}

var scheduledJobUnitPattern = regexp.MustCompile(`^stackfort-job-[0-9]{6}-[0-9a-f]{32}\.(service|timer)$`)

func validateScheduledJobRequest(correlation *AuditCorrelation, request ScheduledJobReconcileRequest) error {
	if validateHostingIdentityMutation(correlation, request.Identity) != nil ||
		scheduledjobs.Validate(scheduledjobs.Spec{Identity: request.Identity, Definition: request.Definition}) != nil {
		return errors.New("scheduled job intent is malformed")
	}
	return nil
}

func validateScheduledJobResponse(response ScheduledJobReconcileResponse, operation Operation) error {
	if operation != OperationReconcileScheduledJob ||
		!validCanonicalUUIDv7(response.JobID) ||
		!validScheduledJobUnit(response.ServiceUnit, response.JobID, ".service") ||
		!validScheduledJobUnit(response.TimerUnit, response.JobID, ".timer") ||
		validateCapability(response.Capability) != nil ||
		response.Capability.Status != CapabilityAvailable ||
		(!response.Present && response.Enabled) {
		return errors.New("scheduled job response is malformed")
	}
	return nil
}

func validScheduledJobUnit(value, jobID, suffix string) bool {
	return scheduledJobUnitPattern.MatchString(value) && strings.HasSuffix(value, suffix) &&
		strings.HasSuffix(strings.TrimSuffix(value, suffix), strings.ReplaceAll(jobID, "-", ""))
}
