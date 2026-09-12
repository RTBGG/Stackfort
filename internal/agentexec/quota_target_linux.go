// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package agentexec

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/RTBGG/stackfort/internal/quotastate"
	"golang.org/x/sys/unix"
)

// quota-tools excludes subtree bind mounts when locating quota filesystems.
// Keep the prepared hosting-filesystem path unchanged; allow the literal root
// target only when descriptor-verified hosting storage is on that same device
// and the kernel reports an ext4 root with project quotas. Never accept a path
// or block-device name from an RPC caller or fall back after a setquota error.
func projectQuotaTarget() (string, error) {
	target, err := projectQuotaTargetAt("/", "/proc/self/mountinfo", 0)
	if err != nil || target != "/" {
		return target, err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	state, err := quotastate.ReadProject(fd)
	if err != nil {
		return "", err
	}
	if !state.Ready() {
		return "", ErrInvalidInvocation
	}
	return target, nil
}

func projectQuotaTargetAt(root, mountInfo string, owner uint32) (string, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	rootStat, err := trustedQuotaDirectory(fd, owner)
	if err != nil {
		return "", err
	}
	srv, err := unix.Openat(fd, "srv", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(srv)
	if _, err := trustedQuotaDirectory(srv, owner); err != nil {
		return "", err
	}
	hosting, err := unix.Openat(srv, "hosting", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(hosting)
	hostingStat, err := trustedQuotaDirectory(hosting, owner)
	if err != nil {
		return "", err
	}
	if hostingStat.Dev != rootStat.Dev {
		return "/srv/hosting", nil
	}
	// #nosec G304 -- private helper: production supplies literal /proc/self/mountinfo; only tests supply a fixture path.
	file, err := os.Open(mountInfo)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(info) > 1<<20 {
		return "", ErrInvalidInvocation
	}
	return rootProjectQuotaTarget(uint64(rootStat.Dev), string(info))
}

func trustedQuotaDirectory(fd int, owner uint32) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return stat, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != owner || stat.Mode&0022 != 0 {
		return stat, ErrInvalidInvocation
	}
	return stat, nil
}

func rootProjectQuotaTarget(device uint64, mountInfo string) (string, error) {
	majorMinor := fmt.Sprintf("%d:%d", unix.Major(device), unix.Minor(device))
	count := 0
	for _, line := range strings.Split(mountInfo, "\n") {
		if line == "" {
			continue
		}
		before, after, ok := strings.Cut(line, " - ")
		left, right := strings.Fields(before), strings.Fields(after)
		if !ok || len(left) < 6 || len(right) != 3 {
			return "", ErrInvalidInvocation
		}
		if left[4] != "/" {
			continue
		}
		count++
		if left[2] != majorMinor || left[3] != "/" || right[0] != "ext4" {
			return "", ErrInvalidInvocation
		}
		options := "," + left[5] + "," + right[2] + ","
		if !strings.Contains(options, ",prjquota,") || strings.Contains(options, ",ro,") {
			return "", ErrInvalidInvocation
		}
	}
	if count != 1 {
		return "", ErrInvalidInvocation
	}
	return "/", nil
}
