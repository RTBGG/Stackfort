// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"regexp"
	"time"
)

var canonicalPlatformUpdateVersion = regexp.MustCompile(
	`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-beta\.([1-9][0-9]*))?$`,
)

type PlatformUpdateStartRequest struct {
	Version string `json:"version"`
}

type PlatformUpdateInspectRequest struct{}

type PlatformUpdateStartResponse struct {
	Version  string `json:"version"`
	Accepted bool   `json:"accepted"`
}

type PlatformUpdateStatusResponse struct {
	State          string `json:"state"`
	CurrentVersion string `json:"currentVersion,omitempty"`
	TargetVersion  string `json:"targetVersion,omitempty"`
	StartedAt      string `json:"startedAt,omitempty"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
	CompletedAt    string `json:"completedAt,omitempty"`
	ErrorCode      string `json:"errorCode,omitempty"`
}

func validatePlatformUpdateStartRequest(
	correlation *AuditCorrelation,
	request PlatformUpdateStartRequest,
) error {
	if correlation == nil || correlation.AccountID != "" ||
		!canonicalPlatformUpdateVersion.MatchString(request.Version) {
		return errors.New("platform update request is malformed")
	}
	return nil
}

func validatePlatformUpdateStartResponse(response PlatformUpdateStartResponse, expected Operation) error {
	if expected != OperationStartPlatformUpdate || !response.Accepted ||
		!canonicalPlatformUpdateVersion.MatchString(response.Version) {
		return errors.New("platform update start response is malformed")
	}
	return nil
}

func validatePlatformUpdateStatusResponse(response PlatformUpdateStatusResponse, expected Operation) error {
	if expected != OperationInspectPlatformUpdate {
		return errors.New("platform update status response is unexpected")
	}
	if response.State == "idle" {
		if response.CurrentVersion != "" || response.TargetVersion != "" || response.StartedAt != "" ||
			response.UpdatedAt != "" || response.CompletedAt != "" || response.ErrorCode != "" {
			return errors.New("idle platform update status carries journal data")
		}
		return nil
	}
	if response.State != "applying" && response.State != "rolling_back" &&
		response.State != "rolled_back" && response.State != "rollback_failed" && response.State != "complete" {
		return errors.New("platform update status is invalid")
	}
	if !canonicalPlatformUpdateVersion.MatchString(response.CurrentVersion) ||
		!canonicalPlatformUpdateVersion.MatchString(response.TargetVersion) ||
		!validRFC3339Nano(response.StartedAt) || !validRFC3339Nano(response.UpdatedAt) ||
		len(response.ErrorCode) > 80 || (response.ErrorCode != "" && !validBoundedIdentifier(response.ErrorCode)) {
		return errors.New("platform update journal summary is malformed")
	}
	if response.CompletedAt != "" && !validRFC3339Nano(response.CompletedAt) {
		return errors.New("platform update completion time is malformed")
	}
	if (response.State == "complete" || response.State == "rolled_back" || response.State == "rollback_failed") &&
		response.CompletedAt == "" {
		return errors.New("terminal platform update status has no completion time")
	}
	return nil
}

func validRFC3339Nano(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !parsed.IsZero()
}
