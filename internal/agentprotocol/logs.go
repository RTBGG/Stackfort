// SPDX-License-Identifier: AGPL-3.0-or-later

package agentprotocol

import (
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
)

const (
	MaximumHostingLogEntries      = 50
	MaximumHostingLogMessageBytes = 768
	MaximumHostingLogPathBytes    = 512
)

type HostingLogKind string

const (
	HostingLogAccess HostingLogKind = "access"
	HostingLogError  HostingLogKind = "error"
)

type HostingLogReadRequest struct {
	Identity hostingidentity.Spec      `json:"identity"`
	Domain   core.NormalizedDomainName `json:"domain"`
	Kind     HostingLogKind            `json:"kind"`
	Cursor   string                    `json:"cursor,omitempty"`
	Limit    uint16                    `json:"limit"`
}

type HostingLogRecord struct {
	Timestamp     string `json:"timestamp"`
	Level         string `json:"level"`
	ClientAddress string `json:"clientAddress,omitempty"`
	Host          string `json:"host,omitempty"`
	Method        string `json:"method,omitempty"`
	Path          string `json:"path,omitempty"`
	Status        uint16 `json:"status,omitempty"`
	Bytes         uint64 `json:"bytes,omitempty"`
	DurationMS    uint64 `json:"durationMs,omitempty"`
	Message       string `json:"message,omitempty"`
}

type HostingLogReadResponse struct {
	Domain             core.NormalizedDomainName `json:"domain"`
	Kind               HostingLogKind            `json:"kind"`
	Records            []HostingLogRecord        `json:"records"`
	Next               string                    `json:"next,omitempty"`
	RetentionDays      uint16                    `json:"retentionDays"`
	MaximumActiveBytes uint64                    `json:"maximumActiveBytes"`
	SensitiveRedaction bool                      `json:"sensitiveRedaction"`
	QueryStringsStored bool                      `json:"queryStringsStored"`
}

var hostingLogCursorPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}:[1-9][0-9]{0,19}$`)
var hostingLogMethodPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,31}$`)
var hostingLogHostPattern = regexp.MustCompile(`^[A-Za-z0-9.:-]{1,253}$`)

func validateHostingLogReadRequest(request HostingLogReadRequest) error {
	if hostingidentity.Validate(request.Identity) != nil || !validHostingLogDomain(request.Domain) ||
		!validHostingLogKind(request.Kind) || !validHostingLogCursor(request.Cursor) ||
		request.Limit == 0 || request.Limit > MaximumHostingLogEntries {
		return errors.New("hosting log read request is malformed")
	}
	return nil
}

func validateHostingLogReadResponse(response HostingLogReadResponse, expected Operation) error {
	if expected != OperationReadHostingLogs || !validHostingLogDomain(response.Domain) ||
		!validHostingLogKind(response.Kind) || !validHostingLogCursor(response.Next) ||
		len(response.Records) > MaximumHostingLogEntries ||
		response.RetentionDays != hostinglogs.RetentionDays ||
		response.MaximumActiveBytes != hostinglogs.MaximumActiveBytes ||
		!response.SensitiveRedaction || response.QueryStringsStored {
		return errors.New("hosting log read response is malformed")
	}
	for _, record := range response.Records {
		if validateHostingLogRecord(record, response.Kind) != nil {
			return errors.New("hosting log record is malformed")
		}
	}
	return nil
}

func validateHostingLogRecord(record HostingLogRecord, kind HostingLogKind) error {
	if parsed, err := time.Parse(time.RFC3339, record.Timestamp); err != nil || parsed.Format(time.RFC3339) != record.Timestamp {
		return errors.New("invalid hosting log timestamp")
	}
	if !oneOf(record.Level, "info", "notice", "warn", "error", "crit", "alert", "emerg") ||
		len(record.Message) > MaximumHostingLogMessageBytes || !utf8.ValidString(record.Message) ||
		strings.ContainsAny(record.Message, "\r\n\t\x00") || len(record.Path) > MaximumHostingLogPathBytes ||
		!utf8.ValidString(record.Path) || strings.ContainsAny(record.Path, "\r\n\x00?") {
		return errors.New("invalid hosting log text")
	}
	if kind == HostingLogError {
		if record.ClientAddress != "" || record.Host != "" || record.Method != "" ||
			record.Path != "" || record.Status != 0 || record.Bytes != 0 || record.DurationMS != 0 || record.Message == "" {
			return errors.New("invalid error log union")
		}
		return nil
	}
	if record.Level != "info" || record.Message != "" || !hostingLogHostPattern.MatchString(record.Host) ||
		!hostingLogMethodPattern.MatchString(record.Method) || record.Path == "" || record.Path[0] != '/' ||
		record.Status < 100 || record.Status > 599 {
		return errors.New("invalid access log union")
	}
	if address, err := netip.ParseAddr(record.ClientAddress); err != nil || address.String() != record.ClientAddress {
		return errors.New("invalid access log client address")
	}
	return nil
}

func validHostingLogDomain(domain core.NormalizedDomainName) bool {
	normalized, err := core.NormalizeDomainName(domain.ASCII)
	if err != nil || normalized.ASCII != domain.ASCII {
		return false
	}
	display, err := core.NormalizeDomainName(domain.Display)
	return err == nil && display == domain
}

func validHostingLogKind(kind HostingLogKind) bool {
	return kind == HostingLogAccess || kind == HostingLogError
}

func validHostingLogCursor(cursor string) bool {
	if cursor == "" {
		return true
	}
	if !hostingLogCursorPattern.MatchString(cursor) {
		return false
	}
	parts := strings.Split(cursor, ":")
	for _, part := range parts {
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil || value == 0 || strconv.FormatUint(value, 10) != part {
			return false
		}
	}
	return true
}
