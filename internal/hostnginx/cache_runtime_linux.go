// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"errors"
	"os"
	"os/user"
	"strconv"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

// PrepareCacheRuntime creates only the fixed, worker-owned cache directory.
// Existing unsafe ownership, permissions or symlinks are conflicts, not repaired.
// Cached bytes are disposable but never recursively deleted by reconciliation.
func PrepareCacheRuntime(spec nginxbaseline.Spec) (bool, error) {
	return (&linuxConfigurationManager{root: "/"}).cacheRuntime(spec, true)
}

func VerifyCacheRuntime(spec nginxbaseline.Spec) error {
	_, err := (&linuxConfigurationManager{root: "/"}).cacheRuntime(spec, false)
	return err
}

func (manager *linuxConfigurationManager) cacheRuntime(spec nginxbaseline.Spec, create bool) (bool, error) {
	validated, err := nginxbaseline.ForDistribution(spec.DistributionID)
	if err != nil || validated.WorkerUser != spec.WorkerUser {
		return false, ErrConflict
	}
	worker, err := user.Lookup(spec.WorkerUser)
	if err != nil {
		return false, ErrConflict
	}
	uid, uidErr := strconv.ParseUint(worker.Uid, 10, 32)
	gid, gidErr := strconv.ParseUint(worker.Gid, 10, 32)
	if uidErr != nil || gidErr != nil || uid == 0 || gid == 0 {
		return false, ErrConflict
	}
	for _, anchor := range []string{"/var", "/var/cache"} {
		info, err := os.Lstat(manager.rooted(anchor))
		if err != nil || !safeRootDirectory(info) {
			return false, ErrConflict
		}
	}
	path := manager.rooted(cacheconfig.FastCGIDirectory)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, 0o700); err != nil {
			return false, err
		}
		// #nosec G115 -- numeric IDs were parsed with a 32-bit bound on supported amd64 hosts.
		if err := os.Chown(path, int(uid), int(gid)); err != nil {
			return true, err
		}
		return true, nil
	}
	if err != nil {
		return false, ErrConflict
	}
	actualUID, actualGID, ok := rootOwnership(info)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 ||
		uint64(actualUID) != uid || uint64(actualGID) != gid {
		return false, ErrConflict
	}
	return false, nil
}
