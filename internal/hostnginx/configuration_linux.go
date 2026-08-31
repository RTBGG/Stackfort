// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/RTBGG/stackfort/internal/nginxbaseline"
	"github.com/RTBGG/stackfort/internal/wafconfig"
)

type linuxConfigurationManager struct{ root string }

func newConfigurationManager() configurationManager { return &linuxConfigurationManager{root: "/"} }

func (manager *linuxConfigurationManager) rooted(path string) string {
	return filepath.Join(manager.root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
}

func (manager *linuxConfigurationManager) Managed() (bool, error) {
	root := manager.rooted(nginxbaseline.ManagedRoot)
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil || !safeRootDirectory(info) {
		return false, ErrConflict
	}
	marker := manager.rooted(nginxbaseline.MarkerPath)
	content, info, err := readSafeRootFile(marker)
	if errors.Is(err, fs.ErrNotExist) {
		return false, ErrConflict
	}
	if err != nil || !bytes.Equal(content, []byte(nginxbaseline.OwnershipMarker)) || info.Mode().Perm() != 0o640 {
		return false, ErrConflict
	}
	return true, nil
}

type fileIntent struct {
	path    string
	content []byte
	mode    fs.FileMode
}

type pathSnapshot struct {
	path    string
	existed bool
	isDir   bool
	content []byte
	mode    fs.FileMode
	uid     uint32
	gid     uint32
}

func (manager *linuxConfigurationManager) Prepare(spec nginxbaseline.Spec) (*configurationChange, error) {
	previouslyManaged, err := manager.Managed()
	if err != nil {
		return nil, err
	}
	if err := manager.rejectForeignDropIns(previouslyManaged); err != nil {
		return nil, err
	}
	if err := manager.validateAnchors(); err != nil {
		return nil, err
	}
	directories := []struct {
		path string
		mode fs.FileMode
	}{
		// Coraza resolves SecLang includes in the unprivileged NGINX worker.
		// Managed files retain their individual least-privilege modes, while
		// the common root must therefore permit directory traversal.
		{nginxbaseline.ManagedRoot, 0o755},
		{nginxbaseline.GlobalDirectory, 0o750},
		{nginxbaseline.DefaultDirectory, 0o750},
		{nginxbaseline.PanelDirectory, 0o750},
		{nginxbaseline.SitesDirectory, 0o750},
		// coraza-nginx creates each WAF after dropping to the NGINX worker
		// identity, so every directory in the SecLang include chain must be
		// traversable by that unprivileged worker.
		{wafconfig.ConfigurationRoot, 0o755},
		{wafconfig.ProfilesDirectory, 0o755},
		{nginxbaseline.SystemdDropInDir, 0o755},
	}
	files := []fileIntent{
		{nginxbaseline.MarkerPath, []byte(nginxbaseline.OwnershipMarker), 0o640},
		{nginxbaseline.MainConfiguration, []byte(nginxbaseline.Main(spec)), 0o640},
		{nginxbaseline.TrustedProxiesPath, []byte(nginxbaseline.TrustedProxies()), 0o640},
		{nginxbaseline.DefaultHTTPPath, []byte(nginxbaseline.DefaultHTTP()), 0o640},
		{nginxbaseline.DefaultHTTPSPath, []byte(nginxbaseline.DefaultHTTPS()), 0o640},
		{wafconfig.EnginePath, []byte(wafconfig.Engine()), 0o644},
		{wafconfig.BasePL1Path, []byte(wafconfig.BasePL1()), 0o644},
		{wafconfig.DetectionPL1Path, []byte(wafconfig.DetectionPL1()), 0o644},
		{wafconfig.BlockingPL1Path, []byte(wafconfig.BlockingPL1()), 0o644},
		{wafconfig.SharedPL1Path, []byte(wafconfig.SharedPL1()), 0o640},
		{nginxbaseline.SystemdDropInPath, []byte(nginxbaseline.SystemdDropIn()), 0o644},
	}

	snapshots := make([]pathSnapshot, 0, len(directories)+len(files))
	changed, dropInChanged := false, false
	rollback := func() error { return restoreSnapshots(snapshots) }
	for _, directory := range directories {
		path := manager.rooted(directory.path)
		snapshot, err := snapshotPath(path)
		if err != nil {
			_ = rollback()
			return nil, err
		}
		if snapshot.existed && (!snapshot.isDir || snapshot.uid != 0 || snapshot.gid != 0) {
			_ = rollback()
			return nil, ErrConflict
		}
		snapshots = append(snapshots, snapshot)
		directoryChanged, err := ensureRootDirectory(path, directory.mode)
		if err != nil {
			_ = rollback()
			return nil, err
		}
		changed = changed || directoryChanged
	}
	for _, file := range files {
		path := manager.rooted(file.path)
		snapshot, err := snapshotPath(path)
		if err != nil {
			_ = rollback()
			return nil, err
		}
		if snapshot.existed && (snapshot.isDir || snapshot.uid != 0 || snapshot.gid != 0) {
			_ = rollback()
			return nil, ErrConflict
		}
		snapshots = append(snapshots, snapshot)
		fileChanged, err := ensureRootFile(path, file.content, file.mode)
		if err != nil {
			_ = rollback()
			return nil, err
		}
		changed = changed || fileChanged
		if file.path == nginxbaseline.SystemdDropInPath {
			dropInChanged = fileChanged
		}
	}
	return &configurationChange{
		Changed: changed, DropInChanged: dropInChanged, PreviouslyManaged: previouslyManaged,
		commit: func() error { snapshots = nil; return nil }, rollback: rollback,
	}, nil
}

func (manager *linuxConfigurationManager) validateAnchors() error {
	for _, anchor := range []string{"/etc", "/etc/nginx", "/etc/systemd", "/etc/systemd/system"} {
		info, err := os.Lstat(manager.rooted(anchor))
		if err != nil || !safeRootDirectory(info) {
			return ErrConflict
		}
	}
	return nil
}

func (manager *linuxConfigurationManager) rejectForeignDropIns(previouslyManaged bool) error {
	directory := manager.rooted(nginxbaseline.SystemdDropInDir)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".conf") && entry.Name() != filepath.Base(nginxbaseline.SystemdDropInPath) {
			return ErrConflict
		}
	}
	if !previouslyManaged {
		if _, err := os.Lstat(manager.rooted(nginxbaseline.SystemdDropInPath)); err == nil {
			return ErrConflict
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func snapshotPath(path string) (pathSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return pathSnapshot{path: path}, nil
	}
	if err != nil {
		return pathSnapshot{}, err
	}
	uid, gid, safe := rootOwnership(info)
	if !safe || info.Mode()&os.ModeSymlink != 0 {
		return pathSnapshot{}, ErrConflict
	}
	snapshot := pathSnapshot{
		path: path, existed: true, isDir: info.IsDir(), mode: info.Mode().Perm(), uid: uid, gid: gid,
	}
	if !snapshot.isDir {
		if !info.Mode().IsRegular() {
			return pathSnapshot{}, ErrConflict
		}
		// #nosec G304 -- path is selected from the fixed nginxbaseline intent and rooted below the manager test/host root.
		content, err := os.ReadFile(path)
		if err != nil {
			return pathSnapshot{}, err
		}
		snapshot.content = content
	}
	return snapshot, nil
}

func restoreSnapshots(snapshots []pathSnapshot) error {
	var failures []error
	for index := len(snapshots) - 1; index >= 0; index-- {
		snapshot := snapshots[index]
		if !snapshot.existed {
			if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				failures = append(failures, err)
			}
			continue
		}
		if snapshot.isDir {
			if err := os.Chmod(snapshot.path, snapshot.mode); err != nil {
				failures = append(failures, err)
			}
			if err := os.Chown(snapshot.path, int(snapshot.uid), int(snapshot.gid)); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if err := atomicWrite(snapshot.path, snapshot.content, snapshot.mode, snapshot.uid, snapshot.gid); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func ensureRootDirectory(path string, mode fs.FileMode) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(path, mode); err != nil {
			return false, err
		}
		if err := os.Chown(path, 0, 0); err != nil {
			return false, err
		}
		return true, os.Chmod(path, mode)
	}
	if err != nil || !safeRootDirectory(info) {
		return false, ErrConflict
	}
	changed := info.Mode().Perm() != mode
	if changed {
		return true, os.Chmod(path, mode)
	}
	return false, nil
}

func ensureRootFile(path string, content []byte, mode fs.FileMode) (bool, error) {
	existing, info, err := readSafeRootFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, atomicWrite(path, content, mode, 0, 0)
	}
	if err != nil {
		return false, err
	}
	if bytes.Equal(existing, content) && info.Mode().Perm() == mode {
		return false, nil
	}
	return true, atomicWrite(path, content, mode, 0, 0)
}

func atomicWrite(path string, content []byte, mode fs.FileMode, uid, gid uint32) error {
	directory := filepath.Dir(path)
	// #nosec G304 -- directory comes from fixed nginxbaseline paths below validated root-owned anchors.
	temporary, err := os.CreateTemp(directory, ".stackfort-nginx-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chown(int(uid), int(gid)); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	// #nosec G304 -- directory is the validated fixed parent used for the just-committed managed file.
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}

func readSafeRootFile(path string) ([]byte, fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	uid, gid, safe := rootOwnership(info)
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !safe || uid != 0 || gid != 0 || !ok || stat.Nlink != 1 || !info.Mode().IsRegular() {
		return nil, nil, ErrConflict
	}
	// #nosec G304 -- callers provide fixed nginxbaseline paths rooted below the manager test/host root.
	content, err := os.ReadFile(path)
	return content, info, err
}

func safeRootDirectory(info fs.FileInfo) bool {
	uid, gid, safe := rootOwnership(info)
	return safe && uid == 0 && gid == 0 && info.IsDir() && info.Mode()&os.ModeSymlink == 0 &&
		info.Mode().Perm()&0o022 == 0
}

func rootOwnership(info fs.FileInfo) (uint32, uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return stat.Uid, stat.Gid, true
}
