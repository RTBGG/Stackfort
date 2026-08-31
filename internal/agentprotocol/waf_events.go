// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
)

const MaximumWAFEventEntries = 50

type WAFEventCategory string

const (
	WAFEventProtocol          WAFEventCategory = "protocol"
	WAFEventProtocolAttack    WAFEventCategory = "protocol_attack"
	WAFEventLocalFile         WAFEventCategory = "local_file_inclusion"
	WAFEventRemoteFile        WAFEventCategory = "remote_file_inclusion"
	WAFEventRemoteExecution   WAFEventCategory = "remote_code_execution"
	WAFEventPHPAttack         WAFEventCategory = "php_attack"
	WAFEventCrossSiteScript   WAFEventCategory = "cross_site_scripting"
	WAFEventSQLInjection      WAFEventCategory = "sql_injection"
	WAFEventSessionAttack     WAFEventCategory = "session_attack"
	WAFEventJavaAttack        WAFEventCategory = "java_attack"
	WAFEventAnomalyThreshold  WAFEventCategory = "anomaly_threshold"
	WAFEventRequestValidation WAFEventCategory = "request_validation"
	WAFEventOther             WAFEventCategory = "other"
)

type WAFEventSeverity string

const (
	WAFSeverityEmergency WAFEventSeverity = "emergency"
	WAFSeverityAlert     WAFEventSeverity = "alert"
	WAFSeverityCritical  WAFEventSeverity = "critical"
	WAFSeverityError     WAFEventSeverity = "error"
	WAFSeverityWarning   WAFEventSeverity = "warning"
	WAFSeverityNotice    WAFEventSeverity = "notice"
	WAFSeverityInfo      WAFEventSeverity = "info"
)

type WAFEventOutcome string

const (
	WAFEventDetected WAFEventOutcome = "detected"
	WAFEventBlocked  WAFEventOutcome = "blocked"
)

type WAFEventReadRequest struct {
	Identity hostingidentity.Spec      `json:"identity"`
	Domain   core.NormalizedDomainName `json:"domain"`
	Cursor   string                    `json:"cursor,omitempty"`
	Limit    uint16                    `json:"limit"`
}

// WAFEvent is the complete allowlisted host-to-control representation. It
// intentionally has no native message, matched value, headers, query, body,
// client address, file name, rule tags, or operator data fields.
type WAFEvent struct {
	ID            string           `json:"id"`
	Timestamp     string           `json:"timestamp"`
	RuleID        uint32           `json:"ruleId"`
	Category      WAFEventCategory `json:"category"`
	Severity      WAFEventSeverity `json:"severity"`
	Outcome       WAFEventOutcome  `json:"outcome"`
	Method        string           `json:"method,omitempty"`
	Path          string           `json:"path,omitempty"`
	CorrelationID string           `json:"correlationId,omitempty"`
}

type WAFEventReadResponse struct {
	Domain             core.NormalizedDomainName `json:"domain"`
	Events             []WAFEvent                `json:"events"`
	Next               string                    `json:"next,omitempty"`
	RetentionDays      uint16                    `json:"retentionDays"`
	MaximumActiveBytes uint64                    `json:"maximumActiveBytes"`
	NativeDataWithheld bool                      `json:"nativeDataWithheld"`
	QueryStringsStored bool                      `json:"queryStringsStored"`
}

var wafEventIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var wafCorrelationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func validateWAFEventReadRequest(request WAFEventReadRequest) error {
	if hostingidentity.Validate(request.Identity) != nil || !validHostingLogDomain(request.Domain) ||
		!validHostingLogCursor(request.Cursor) || request.Limit == 0 || request.Limit > MaximumWAFEventEntries {
		return errors.New("WAF event read request is malformed")
	}
	return nil
}

func validateWAFEventReadResponse(response WAFEventReadResponse, expected Operation) error {
	if expected != OperationReadWAFEvents || !validHostingLogDomain(response.Domain) ||
		!validHostingLogCursor(response.Next) || len(response.Events) > MaximumWAFEventEntries ||
		response.RetentionDays != hostinglogs.RetentionDays ||
		response.MaximumActiveBytes != hostinglogs.MaximumActiveBytes ||
		!response.NativeDataWithheld || response.QueryStringsStored {
		return errors.New("WAF event read response is malformed")
	}
	for _, event := range response.Events {
		if validateWAFEvent(event) != nil {
			return errors.New("WAF event is malformed")
		}
	}
	return nil
}

func validateWAFEvent(event WAFEvent) error {
	parsed, err := time.Parse(time.RFC3339, event.Timestamp)
	if err != nil || parsed.Format(time.RFC3339) != event.Timestamp ||
		!wafEventIDPattern.MatchString(event.ID) || event.RuleID == 0 ||
		!validWAFEventCategory(event.Category) || !validWAFEventSeverity(event.Severity) ||
		(event.Outcome != WAFEventDetected && event.Outcome != WAFEventBlocked) ||
		(event.Method != "" && !hostingLogMethodPattern.MatchString(event.Method)) ||
		(event.Path != "" && (len(event.Path) > MaximumHostingLogPathBytes || event.Path[0] != '/' ||
			strings.ContainsAny(event.Path, "\r\n\x00?"))) ||
		(event.CorrelationID != "" && !wafCorrelationIDPattern.MatchString(event.CorrelationID)) {
		return errors.New("invalid WAF event")
	}
	return nil
}

func validWAFEventCategory(value WAFEventCategory) bool {
	switch value {
	case WAFEventProtocol, WAFEventProtocolAttack, WAFEventLocalFile, WAFEventRemoteFile,
		WAFEventRemoteExecution, WAFEventPHPAttack, WAFEventCrossSiteScript,
		WAFEventSQLInjection, WAFEventSessionAttack, WAFEventJavaAttack,
		WAFEventAnomalyThreshold, WAFEventRequestValidation, WAFEventOther:
		return true
	default:
		return false
	}
}

func validWAFEventSeverity(value WAFEventSeverity) bool {
	switch value {
	case WAFSeverityEmergency, WAFSeverityAlert, WAFSeverityCritical, WAFSeverityError,
		WAFSeverityWarning, WAFSeverityNotice, WAFSeverityInfo:
		return true
	default:
		return false
	}
}
