// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostcache

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/agentprotocol"
	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostinglogs"
	"golang.org/x/sys/unix"
)

type linuxManager struct{}

type cacheAccessLine struct {
	Host  string `json:"host"`
	Cache string `json:"cache"`
}

func newPlatformManager() platformManager { return linuxManager{} }

func (linuxManager) Metrics(ctx context.Context, request agentprotocol.CacheMetricsRequest) (agentprotocol.CacheMetricsResponse, error) {
	if hostingidentity.Validate(request.Identity) != nil {
		return agentprotocol.CacheMetricsResponse{}, ErrInvalid
	}
	domain, err := core.NormalizeDomainName(request.DomainASCII)
	if err != nil || domain.ASCII != request.DomainASCII || domain.Display != request.DomainASCII {
		return agentprotocol.CacheMetricsResponse{}, ErrInvalid
	}
	response := agentprotocol.CacheMetricsResponse{DomainASCII: request.DomainASCII}
	base := hostinglogs.DomainFile(request.Identity.AccountID, request.DomainASCII, "access")
	for _, path := range []string{base + ".1", base} {
		if err := countCacheLog(ctx, path, request.DomainASCII, &response); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return agentprotocol.CacheMetricsResponse{}, err
		}
	}
	response.WindowRecords = response.Hits + response.Misses + response.Bypasses
	return response, nil
}

func (linuxManager) Purge(
	ctx context.Context,
	request agentprotocol.CachePurgeRequest,
	runner *agentexec.Runner,
) (agentprotocol.CachePurgeResponse, error) {
	if hostingidentity.Validate(request.Identity) != nil {
		return agentprotocol.CachePurgeResponse{}, ErrInvalid
	}
	domain, err := core.NormalizeDomainName(request.DomainASCII)
	pathPrefix, pathErr := cacheconfig.NormalizePurgePath(request.PathPrefix)
	if err != nil || domain.ASCII != request.DomainASCII || domain.Display != request.DomainASCII ||
		pathErr != nil || pathPrefix != request.PathPrefix {
		return agentprotocol.CachePurgeResponse{}, ErrInvalid
	}
	result, err := runner.Run(ctx, agentexec.Invocation{
		Profile: agentexec.ProfileVinylBan, Values: []string{request.DomainASCII, request.PathPrefix},
	})
	if err != nil {
		if errors.Is(err, agentexec.ErrInvalidInvocation) {
			return agentprotocol.CachePurgeResponse{}, ErrInvalid
		}
		return agentprotocol.CachePurgeResponse{}, fmt.Errorf("%w: vinyladm execution: %v", ErrUnavailable, err)
	}
	if result.ExitCode != 0 {
		return agentprotocol.CachePurgeResponse{}, fmt.Errorf(
			"%w: vinyladm exit code %d: %s", ErrUnavailable, result.ExitCode, result.Stderr,
		)
	}
	return agentprotocol.CachePurgeResponse{
		DomainASCII: request.DomainASCII, PathPrefix: request.PathPrefix, Accepted: true,
	}, nil
}

func countCacheLog(ctx context.Context, path, domain string, response *agentprotocol.CacheMetricsResponse) error {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return os.NewSyscallError("open managed cache log", err)
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return ErrUnavailable
	}
	defer file.Close()
	var stat syscall.Stat_t
	if err := syscall.Fstat(descriptor, &stat); err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		stat.Size < 0 || stat.Size > hostinglogs.MaximumActiveBytes {
		return ErrConflict
	}
	scanner := bufio.NewScanner(io.LimitReader(file, hostinglogs.MaximumActiveBytes+1))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var line cacheAccessLine
		if json.Unmarshal(scanner.Bytes(), &line) != nil ||
			(line.Host != domain && line.Host != "www."+domain) {
			continue
		}
		switch line.Cache {
		case "HIT":
			response.Hits++
		case "MISS":
			response.Misses++
		case "BYPASS":
			response.Bypasses++
		}
	}
	if err := scanner.Err(); err != nil {
		return ErrUnavailable
	}
	return nil
}
