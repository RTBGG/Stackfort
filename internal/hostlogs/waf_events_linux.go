// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostlogs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
)

var (
	wafRuleIDPattern       = regexp.MustCompile(`(?i)\[id\s+["']?([0-9]{1,10})["']?\]`)
	wafSeverityPattern     = regexp.MustCompile(`(?i)\[severity\s+["']?([A-Z0-9_-]{1,16})["']?\]`)
	wafRequestPattern      = regexp.MustCompile(`(?i)request:\s*"([A-Z][A-Z0-9_-]{0,31})\s+(/[^\s"]*)\s+HTTP/[0-9.]+"`)
	wafCorrelationPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[(?:unique_id|transaction_id)\s+["']([A-Za-z0-9_-]{8,128})["']\]`),
		regexp.MustCompile(`(?i)transaction(?:\s+id)?[:=]\s*([A-Za-z0-9_-]{8,128})`),
	}
)

func (linuxManager) ReadWAFEvents(
	ctx context.Context, request agentprotocol.WAFEventReadRequest,
) (agentprotocol.WAFEventReadResponse, error) {
	if hostingidentity.Validate(request.Identity) != nil || request.Limit == 0 ||
		request.Limit > agentprotocol.MaximumWAFEventEntries {
		return agentprotocol.WAFEventReadResponse{}, ErrInvalid
	}
	normalized, normalizeErr := core.NormalizeDomainName(request.Domain.ASCII)
	display, displayErr := core.NormalizeDomainName(request.Domain.Display)
	if normalizeErr != nil || displayErr != nil || normalized.ASCII != request.Domain.ASCII || display != request.Domain {
		return agentprotocol.WAFEventReadResponse{}, ErrInvalid
	}
	paths := retainedPaths(hostinglogs.DomainFile(request.Identity.AccountID, request.Domain.ASCII, "error"))
	if err := verifyLogDirectory(filepath.Dir(hostinglogs.RootDirectory), 0o750); err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	if err := verifyLogDirectory(hostinglogs.RootDirectory, 0o700); err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	if err := verifyLogDirectory(filepath.Dir(paths[0]), 0o700); err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	sources := make([]logSource, 0, len(paths))
	defer func() {
		for index := range sources {
			if sources[index].file != nil {
				_ = sources[index].file.Close()
			}
		}
	}()
	for _, path := range paths {
		source, err := openLogSource(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return agentprotocol.WAFEventReadResponse{}, err
		}
		sources = append(sources, source)
	}
	response := agentprotocol.WAFEventReadResponse{
		Domain: request.Domain, Events: make([]agentprotocol.WAFEvent, 0, request.Limit),
		RetentionDays: hostinglogs.RetentionDays, MaximumActiveBytes: hostinglogs.MaximumActiveBytes,
		NativeDataWithheld: true, QueryStringsStored: false,
	}
	if len(sources) == 0 {
		return response, ErrNotFound
	}
	start, offset, err := locateCursor(sources, request.Cursor)
	if err != nil {
		return agentprotocol.WAFEventReadResponse{}, err
	}
	for index := start; index < len(sources) && len(response.Events) < int(request.Limit); index++ {
		if err := ctx.Err(); err != nil {
			return agentprotocol.WAFEventReadResponse{}, err
		}
		if index != start {
			offset = sources[index].size
		}
		remaining := int(request.Limit) - len(response.Events)
		events, nextOffset, readErr := readPreviousWAFEvents(sources[index], offset, remaining)
		if readErr != nil {
			return agentprotocol.WAFEventReadResponse{}, readErr
		}
		response.Events = append(response.Events, events...)
		if nextOffset > 0 {
			response.Next = encodeCursor(sources[index].inode, nextOffset)
			return response, nil
		}
	}
	return response, nil
}

func readPreviousWAFEvents(
	source logSource, end uint64, limit int,
) ([]agentprotocol.WAFEvent, uint64, error) {
	if end == 0 || limit == 0 {
		return []agentprotocol.WAFEvent{}, 0, nil
	}
	start := uint64(0)
	if end > maximumLogScanBytes {
		start = end - maximumLogScanBytes
	}
	buffer := make([]byte, int(end-start))                                                         // #nosec G115 -- scan length is capped above.
	if _, err := source.file.ReadAt(buffer, int64(start)); err != nil && !errors.Is(err, io.EOF) { // #nosec G115 -- offset is file-bounded.
		return nil, 0, ErrUnavailable
	}
	if len(buffer) != 0 && buffer[len(buffer)-1] == '\n' {
		buffer = buffer[:len(buffer)-1]
	}
	base := start
	if start > 0 {
		separator := bytes.IndexByte(buffer, '\n')
		if separator < 0 {
			return []agentprotocol.WAFEvent{}, start, nil
		}
		buffer = buffer[separator+1:]
		base += uint64(separator + 1)
	}
	lines := bytes.Split(buffer, []byte{'\n'})
	positions := make([]uint64, len(lines))
	position := base
	for index, line := range lines {
		positions[index] = position
		position += uint64(len(line) + 1)
	}
	events := make([]agentprotocol.WAFEvent, 0, min(limit, len(lines)))
	next := uint64(0)
	for index := len(lines) - 1; index >= 0; index-- {
		line := bytes.TrimSuffix(lines[index], []byte{'\r'})
		if event, ok := parseWAFEvent(line, source.inode, positions[index]); ok {
			events = append(events, event)
			if len(events) == limit {
				next = positions[index]
				break
			}
		}
		if index == 0 {
			break
		}
	}
	if next == 0 && start > 0 {
		next = start
	}
	return events, next, nil
}

func parseWAFEvent(line []byte, inode, offset uint64) (agentprotocol.WAFEvent, bool) {
	if len(line) == 0 || len(line) > 16<<10 || !bytes.Contains(bytes.ToLower(line), []byte("coraza:")) {
		return agentprotocol.WAFEvent{}, false
	}
	text := string(bytes.ToValidUTF8(line, []byte("?")))
	ruleMatch := wafRuleIDPattern.FindStringSubmatch(text)
	if len(ruleMatch) != 2 {
		return agentprotocol.WAFEvent{}, false
	}
	rule, err := strconv.ParseUint(ruleMatch[1], 10, 32)
	if err != nil || rule == 0 {
		return agentprotocol.WAFEvent{}, false
	}
	timestamp := time.Unix(0, 0).UTC()
	if len(text) >= 19 {
		if parsed, parseErr := time.ParseInLocation("2006/01/02 15:04:05", text[:19], time.Local); parseErr == nil {
			timestamp = parsed
		}
	}
	coordinate := strconv.FormatUint(inode, 10) + ":" + strconv.FormatUint(offset, 10)
	digest := sha256.Sum256([]byte(coordinate))
	event := agentprotocol.WAFEvent{
		ID: hex.EncodeToString(digest[:16]), Timestamp: timestamp.Format(time.RFC3339),
		RuleID: uint32(rule), Category: wafCategory(uint32(rule)),
		Severity: agentprotocol.WAFSeverityWarning, Outcome: agentprotocol.WAFEventDetected,
	}
	if severity := wafSeverityPattern.FindStringSubmatch(text); len(severity) == 2 {
		event.Severity = wafSeverity(severity[1])
	}
	request := wafRequestPattern.FindStringSubmatch(text)
	if len(request) == 3 {
		event.Method = request[1]
		path := request[2]
		if separator := strings.IndexByte(path, '?'); separator >= 0 {
			path = path[:separator]
		}
		event.Path = redactText(path, agentprotocol.MaximumHostingLogPathBytes)
	}
	for _, pattern := range wafCorrelationPatterns {
		if match := pattern.FindStringSubmatch(text); len(match) == 2 {
			event.CorrelationID = match[1]
			break
		}
	}
	lower := strings.ToLower(text)
	if rule/1000 == 949 || strings.Contains(lower, "intervention") ||
		strings.Contains(lower, "access denied") || strings.Contains(lower, "status 403") {
		event.Outcome = agentprotocol.WAFEventBlocked
	}
	return event, true
}

func wafCategory(rule uint32) agentprotocol.WAFEventCategory {
	switch rule / 1000 {
	case 920:
		return agentprotocol.WAFEventProtocol
	case 921:
		return agentprotocol.WAFEventProtocolAttack
	case 930:
		return agentprotocol.WAFEventLocalFile
	case 931:
		return agentprotocol.WAFEventRemoteFile
	case 932, 934:
		return agentprotocol.WAFEventRemoteExecution
	case 933:
		return agentprotocol.WAFEventPHPAttack
	case 941:
		return agentprotocol.WAFEventCrossSiteScript
	case 942:
		return agentprotocol.WAFEventSQLInjection
	case 943:
		return agentprotocol.WAFEventSessionAttack
	case 944:
		return agentprotocol.WAFEventJavaAttack
	case 949:
		return agentprotocol.WAFEventAnomalyThreshold
	}
	if rule >= 200000 && rule < 300000 {
		return agentprotocol.WAFEventRequestValidation
	}
	return agentprotocol.WAFEventOther
}

func wafSeverity(value string) agentprotocol.WAFEventSeverity {
	switch strings.ToUpper(value) {
	case "0", "EMERGENCY":
		return agentprotocol.WAFSeverityEmergency
	case "1", "ALERT":
		return agentprotocol.WAFSeverityAlert
	case "2", "CRITICAL":
		return agentprotocol.WAFSeverityCritical
	case "3", "ERROR":
		return agentprotocol.WAFSeverityError
	case "4", "WARNING", "WARN":
		return agentprotocol.WAFSeverityWarning
	case "5", "NOTICE":
		return agentprotocol.WAFSeverityNotice
	default:
		return agentprotocol.WAFSeverityInfo
	}
}
