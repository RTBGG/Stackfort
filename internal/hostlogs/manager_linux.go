// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostlogs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
	"golang.org/x/sys/unix"
)

const (
	maximumLogScanBytes = 256 << 10
)

type linuxManager struct{}

type logSource struct {
	file  *os.File
	inode uint64
	size  uint64
}

type accessLine struct {
	Timestamp string `json:"timestamp"`
	Client    string `json:"client"`
	Host      string `json:"host"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Status    uint16 `json:"status"`
	Bytes     uint64 `json:"bytes"`
	Duration  string `json:"duration"`
}

var (
	errorLevelPattern    = regexp.MustCompile(`\[(debug|info|notice|warn|error|crit|alert|emerg)\]`)
	accessHostPattern    = regexp.MustCompile(`^[A-Za-z0-9.:-]{1,253}$`)
	accessMethodPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,31}$`)
	queryPattern         = regexp.MustCompile(`\?[^\s"'<>#]*`)
	credentialPattern    = regexp.MustCompile(`(?i)\b(authorization|cookie|set-cookie|password|passwd|token|secret|api[_-]?key|session)(\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^,\r\n]+)`)
	sensitivePathPattern = regexp.MustCompile(`(?i)/(token|secret|password|reset|session|auth|key)/[^/?\s"']+`)
)

func newPlatformManager() platformManager { return linuxManager{} }

func (linuxManager) Ensure(
	ctx context.Context, identity hostingidentity.Spec, domains []core.NormalizedDomainName,
) error {
	if err := ensureLogDirectory("/var/log/stackfort", 0o750); err != nil {
		return err
	}
	if err := ensureLogDirectory(hostinglogs.RootDirectory, 0o700); err != nil {
		return err
	}
	accountDirectory := filepath.Join(hostinglogs.RootDirectory, identity.AccountID)
	if err := ensureLogDirectory(accountDirectory, 0o700); err != nil {
		return err
	}
	for _, domain := range domains {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, kind := range []string{string(agentprotocol.HostingLogAccess), string(agentprotocol.HostingLogError)} {
			if err := ensureLogFile(hostinglogs.DomainFile(identity.AccountID, domain.ASCII, kind)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (linuxManager) Read(
	ctx context.Context, request agentprotocol.HostingLogReadRequest,
) (agentprotocol.HostingLogReadResponse, error) {
	if hostingidentity.Validate(request.Identity) != nil || request.Limit == 0 ||
		request.Limit > agentprotocol.MaximumHostingLogEntries ||
		(request.Kind != agentprotocol.HostingLogAccess && request.Kind != agentprotocol.HostingLogError) {
		return agentprotocol.HostingLogReadResponse{}, ErrInvalid
	}
	normalized, normalizeErr := core.NormalizeDomainName(request.Domain.ASCII)
	display, displayErr := core.NormalizeDomainName(request.Domain.Display)
	if normalizeErr != nil || displayErr != nil || normalized.ASCII != request.Domain.ASCII || display != request.Domain {
		return agentprotocol.HostingLogReadResponse{}, ErrInvalid
	}
	paths := retainedPaths(hostinglogs.DomainFile(request.Identity.AccountID, request.Domain.ASCII, string(request.Kind)))
	if err := verifyLogDirectory(filepath.Dir(hostinglogs.RootDirectory), 0o750); err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	if err := verifyLogDirectory(hostinglogs.RootDirectory, 0o700); err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	if err := verifyLogDirectory(filepath.Dir(paths[0]), 0o700); err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
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
			return agentprotocol.HostingLogReadResponse{}, err
		}
		sources = append(sources, source)
	}
	response := agentprotocol.HostingLogReadResponse{
		Domain: request.Domain, Kind: request.Kind,
		Records:       make([]agentprotocol.HostingLogRecord, 0, request.Limit),
		RetentionDays: hostinglogs.RetentionDays, MaximumActiveBytes: hostinglogs.MaximumActiveBytes,
		SensitiveRedaction: true, QueryStringsStored: false,
	}
	if len(sources) == 0 {
		return response, ErrNotFound
	}
	start, offset, err := locateCursor(sources, request.Cursor)
	if err != nil {
		return agentprotocol.HostingLogReadResponse{}, err
	}
	for index := start; index < len(sources) && len(response.Records) < int(request.Limit); index++ {
		if err := ctx.Err(); err != nil {
			return agentprotocol.HostingLogReadResponse{}, err
		}
		if index != start {
			offset = sources[index].size
		}
		remaining := int(request.Limit) - len(response.Records)
		records, nextOffset, err := readPreviousRecords(sources[index], offset, remaining, request.Kind)
		if err != nil {
			return agentprotocol.HostingLogReadResponse{}, err
		}
		response.Records = append(response.Records, records...)
		if nextOffset > 0 {
			response.Next = encodeCursor(sources[index].inode, nextOffset)
			return response, nil
		}
	}
	return response, nil
}

func ensureLogDirectory(path string, mode os.FileMode) error {
	created := false
	if err := os.Mkdir(path, mode); err == nil {
		created = true
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%w: create managed log directory", ErrUnavailable)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrConflict
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return ErrConflict
	}
	if info.Mode().Perm() != mode {
		if !created {
			return ErrConflict
		}
		if err := os.Chmod(path, mode); err != nil {
			return ErrUnavailable
		}
	}
	return nil
}

func verifyLogDirectory(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return ErrUnavailable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode ||
		!ok || stat.Uid != 0 || stat.Gid != 0 {
		return ErrConflict
	}
	return nil
}

func ensureLogFile(path string) error {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o640)
	if err != nil {
		return fmt.Errorf("%w: create managed log file", ErrUnavailable)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o640 {
		return ErrConflict
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 {
		return ErrConflict
	}
	return nil
}

func retainedPaths(active string) []string {
	// The active and delay-compressed prior rotation support stable, bounded
	// reverse pagination without decompressing attacker-influenced traffic in
	// the privileged agent. Older compressed rotations remain under the fixed
	// local retention policy for incident response by an administrator.
	return []string{active, active + ".1"}
}

func openLogSource(path string) (logSource, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return logSource{}, os.ErrNotExist
		}
		return logSource{}, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), path)
	closeOnError := func(err error) (logSource, error) { _ = file.Close(); return logSource{}, err }
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o640 {
		return closeOnError(ErrConflict)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || stat.Ino == 0 {
		return closeOnError(ErrConflict)
	}
	return logSource{file: file, inode: stat.Ino, size: uint64(info.Size())}, nil // #nosec G115 -- regular file size is non-negative.
}

func locateCursor(sources []logSource, cursor string) (int, uint64, error) {
	if cursor == "" {
		return 0, sources[0].size, nil
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return 0, 0, ErrInvalid
	}
	inode, inodeErr := strconv.ParseUint(parts[0], 10, 64)
	offset, offsetErr := strconv.ParseUint(parts[1], 10, 64)
	if inodeErr != nil || offsetErr != nil || inode == 0 || offset == 0 ||
		strconv.FormatUint(inode, 10) != parts[0] || strconv.FormatUint(offset, 10) != parts[1] {
		return 0, 0, ErrInvalid
	}
	for index, source := range sources {
		if source.inode == inode {
			if offset > source.size {
				return 0, 0, ErrConflict
			}
			return index, offset, nil
		}
	}
	return 0, 0, ErrConflict
}

func readPreviousRecords(
	source logSource, end uint64, limit int, kind agentprotocol.HostingLogKind,
) ([]agentprotocol.HostingLogRecord, uint64, error) {
	if end == 0 || limit == 0 {
		return []agentprotocol.HostingLogRecord{}, 0, nil
	}
	start := uint64(0)
	if end > maximumLogScanBytes {
		start = end - maximumLogScanBytes
	}
	length := end - start
	buffer := make([]byte, int(length))                                                            // #nosec G115 -- length is capped at 256 KiB.
	if _, err := source.file.ReadAt(buffer, int64(start)); err != nil && !errors.Is(err, io.EOF) { // #nosec G115 -- source size is stat-bounded.
		return nil, 0, ErrUnavailable
	}
	if len(buffer) != 0 && buffer[len(buffer)-1] == '\n' {
		buffer = buffer[:len(buffer)-1]
	}
	base := start
	if start > 0 {
		separator := bytes.IndexByte(buffer, '\n')
		if separator < 0 {
			return []agentprotocol.HostingLogRecord{}, start, nil
		}
		buffer = buffer[separator+1:]
		base += uint64(separator + 1)
	}
	lines := bytes.Split(buffer, []byte{'\n'})
	records := make([]agentprotocol.HostingLogRecord, 0, min(limit, len(lines)))
	positions := make([]uint64, len(lines))
	position := base
	for index, line := range lines {
		positions[index] = position
		position += uint64(len(line) + 1)
	}
	next := uint64(0)
	for index := len(lines) - 1; index >= 0 && len(records) < limit; index-- {
		line := bytes.TrimSuffix(lines[index], []byte{'\r'})
		if len(line) == 0 {
			if index == 0 {
				break
			}
			continue
		}
		records = append(records, parseLogRecord(line, kind))
		next = positions[index]
		if index == 0 {
			break
		}
	}
	if next == 0 && start > 0 {
		next = start
	}
	return records, next, nil
}

func parseLogRecord(line []byte, kind agentprotocol.HostingLogKind) agentprotocol.HostingLogRecord {
	if kind == agentprotocol.HostingLogError {
		return parseErrorRecord(line)
	}
	var value accessLine
	if len(line) > 4<<10 || json.Unmarshal(line, &value) != nil {
		return malformedAccessRecord()
	}
	timestamp, timestampErr := time.Parse(time.RFC3339, value.Timestamp)
	address, addressErr := netip.ParseAddr(value.Client)
	duration, durationErr := strconv.ParseFloat(value.Duration, 64)
	if timestampErr != nil || addressErr != nil || durationErr != nil || math.IsNaN(duration) || math.IsInf(duration, 0) ||
		duration < 0 || duration > 86_400 ||
		value.Status < 100 || value.Status > 599 || !accessHostPattern.MatchString(value.Host) ||
		!accessMethodPattern.MatchString(value.Method) || value.Path == "" || value.Path[0] != '/' {
		return malformedAccessRecord()
	}
	path := value.Path
	if separator := strings.IndexByte(path, '?'); separator >= 0 {
		path = path[:separator] + "[REDACTED]"
	}
	path = redactText(path, agentprotocol.MaximumHostingLogPathBytes)
	return agentprotocol.HostingLogRecord{
		Timestamp: timestamp.Format(time.RFC3339), Level: "info", ClientAddress: address.String(),
		Host: value.Host, Method: value.Method,
		Path: path, Status: value.Status, Bytes: value.Bytes,
		DurationMS: uint64(duration*1000 + 0.5),
	}
}

func malformedAccessRecord() agentprotocol.HostingLogRecord {
	return agentprotocol.HostingLogRecord{
		Timestamp: time.Unix(0, 0).UTC().Format(time.RFC3339), Level: "info",
		ClientAddress: "0.0.0.0", Host: "withheld.invalid", Method: "GET",
		Path: "/[MALFORMED-RECORD-WITHHELD]", Status: 500,
	}
}

func parseErrorRecord(line []byte) agentprotocol.HostingLogRecord {
	text := string(bytes.ToValidUTF8(line, []byte("?")))
	timestamp := time.Unix(0, 0).UTC()
	if len(text) >= 19 {
		if parsed, err := time.ParseInLocation("2006/01/02 15:04:05", text[:19], time.Local); err == nil {
			timestamp = parsed
		}
	}
	level := "error"
	if match := errorLevelPattern.FindStringSubmatch(text); len(match) == 2 && match[1] != "debug" {
		level = match[1]
	}
	message := redactText(text, agentprotocol.MaximumHostingLogMessageBytes)
	if strings.Contains(strings.ToLower(text), "coraza:") {
		// Coraza diagnostic text may contain the exact matched request
		// value. Account-facing error logs therefore expose no part of the
		// native line; the structured WAF event view is populated separately.
		message = "Web application firewall event details withheld."
	}
	return agentprotocol.HostingLogRecord{
		Timestamp: timestamp.Format(time.RFC3339), Level: level,
		Message: message,
	}
}

func redactText(value string, maximum int) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 {
			return ' '
		}
		return character
	}, value)
	value = queryPattern.ReplaceAllString(value, "?[REDACTED]")
	value = credentialPattern.ReplaceAllString(value, "$1$2[REDACTED]")
	value = sensitivePathPattern.ReplaceAllString(value, "/$1/[REDACTED]")
	return boundedText(value, maximum)
}

func boundedText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum-3]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "..."
}

func encodeCursor(inode, offset uint64) string {
	if inode == 0 || offset == 0 {
		return ""
	}
	return strconv.FormatUint(inode, 10) + ":" + strconv.FormatUint(offset, 10)
}
