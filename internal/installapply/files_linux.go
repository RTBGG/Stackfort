// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

type directorySpec struct {
	path     string
	uid, gid int
	mode     fs.FileMode
}

func verifyRootOwnedSource(source Source) error {
	return filepath.WalkDir(source.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("release source entry has unsafe ownership or mode: %s", path)
		}
		return nil
	})
}

func ValidateSourceTrust(source Source) error { return verifyRootOwnedSource(source) }

func ensureDirectory(spec directorySpec) (bool, error) {
	if !filepath.IsAbs(spec.path) || spec.path == "/" || spec.mode.Perm() != spec.mode {
		return false, errors.New("invalid fixed installer directory")
	}
	if err := rejectSymlinkComponents(filepath.Dir(spec.path)); err != nil {
		return false, err
	}
	changed := false
	info, err := os.Lstat(spec.path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(spec.path, spec.mode); err != nil {
			return false, err
		}
		changed = true
		info, err = os.Lstat(spec.path)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("installer directory conflicts with host state: %s", spec.path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("inspect installer directory ownership")
	}
	if int(stat.Uid) != spec.uid || int(stat.Gid) != spec.gid {
		if err := os.Chown(spec.path, spec.uid, spec.gid); err != nil {
			return false, err
		}
		changed = true
	}
	if info.Mode().Perm() != spec.mode.Perm() {
		if err := os.Chmod(spec.path, spec.mode); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func rejectSymlinkComponents(path string) error {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return errors.New("installer path must be absolute")
	}
	current := string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("installer path traverses unsafe component: %s", current)
		}
	}
	return nil
}

func reconcileFile(path string, content []byte, uid, gid int, mode fs.FileMode, replaceManaged bool) (bool, error) {
	if !filepath.IsAbs(path) || len(content) > maximumSingleFile || mode.Perm() != mode {
		return false, errors.New("invalid fixed installer file")
	}
	if err := rejectSymlinkComponents(filepath.Dir(path)); err != nil {
		return false, err
	}
	existing, exists, info, err := readExistingFile(path)
	if err != nil {
		return false, err
	}
	contentMatches := exists && bytes.Equal(existing, content)
	if exists && !contentMatches && (!replaceManaged || !isManagedInstallerContent(existing)) {
		return false, fmt.Errorf("installer file conflicts with unmanaged host state: %s", path)
	}
	if !contentMatches {
		if err := atomicWriteFile(path, content, uid, gid, mode); err != nil {
			return false, err
		}
		return true, nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("inspect installed file ownership")
	}
	changed := false
	if int(stat.Uid) != uid || int(stat.Gid) != gid {
		if err := os.Chown(path, uid, gid); err != nil {
			return false, err
		}
		changed = true
	}
	if info.Mode().Perm() != mode.Perm() {
		if err := os.Chmod(path, mode); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func isManagedInstallerContent(content []byte) bool {
	return bytes.HasPrefix(content, []byte(managedHeader)) ||
		bytes.HasPrefix(content, []byte("<?php\n// SPDX-License-Identifier: AGPL-3.0-or-later\n// Managed by Stackfort. Do not edit.\n"))
}

func verifyFile(path string, content []byte, uid, gid int, mode fs.FileMode) error {
	existing, exists, info, err := readExistingFile(path)
	if err != nil || !exists || !bytes.Equal(existing, content) || info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("installed file does not match immutable intent: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid || int(stat.Gid) != gid {
		return fmt.Errorf("installed file ownership does not match immutable intent: %s", path)
	}
	return nil
}

func readExistingFile(path string) ([]byte, bool, fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maximumSingleFile {
		return nil, false, nil, fmt.Errorf("installed path is not a bounded regular file: %s", path)
	}
	// #nosec G304 -- callers accept only absolute installer-managed paths after rejecting symlink components.
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false, nil, err
	}
	return content, true, info, nil
}

func atomicWriteFile(path string, content []byte, uid, gid int, mode fs.FileMode) error {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".stackfort-"+hex.EncodeToString(random[:]))
	// #nosec G304 -- temporary is a random exclusive file beside an absolute, symlink-checked managed destination.
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chown(uid, gid); err != nil || file.Chmod(mode) != nil {
		return errors.New("set staged installer file metadata")
	}
	if _, err := file.Write(content); err != nil || file.Sync() != nil || file.Close() != nil {
		return errors.New("write staged installer file")
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	cleanup = false
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func ensureRandomSecret(path string, size, uid, gid int, mode fs.FileMode) (bool, error) {
	if size < 1 || size > 4096 {
		return false, errors.New("invalid installer secret size")
	}
	content, exists, info, err := readExistingFile(path)
	if err != nil {
		return false, err
	}
	if exists {
		if len(content) != size || info.Mode().Perm() != mode.Perm() {
			return false, fmt.Errorf("installed secret metadata conflicts with immutable intent: %s", path)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != uid || int(stat.Gid) != gid {
			return false, fmt.Errorf("installed secret ownership conflicts with immutable intent: %s", path)
		}
		return false, nil
	}
	secret := make([]byte, size)
	if _, err := rand.Read(secret); err != nil {
		return false, err
	}
	defer clear(secret)
	if err := atomicWriteFile(path, secret, uid, gid, mode); err != nil {
		return false, err
	}
	return true, nil
}

func verifySecret(path string, size, uid, gid int, mode fs.FileMode) error {
	content, exists, info, err := readExistingFile(path)
	if err != nil || !exists || len(content) != size || info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("installed secret does not match immutable metadata: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid || int(stat.Gid) != gid {
		return fmt.Errorf("installed secret ownership does not match immutable intent: %s", path)
	}
	return nil
}

func deployWebTree(sourceRoot, destinationRoot string) (bool, error) {
	changed := false
	paths := make([]string, 0, 128)
	if err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == sourceRoot {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	}); err != nil {
		return false, err
	}
	sort.Slice(paths, func(left, right int) bool {
		leftDepth := strings.Count(paths[left], string(filepath.Separator))
		rightDepth := strings.Count(paths[right], string(filepath.Separator))
		return leftDepth < rightDepth || leftDepth == rightDepth && paths[left] < paths[right]
	})
	for _, relative := range paths {
		sourcePath := filepath.Join(sourceRoot, relative)
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return false, err
		}
		destination := filepath.Join(destinationRoot, relative)
		if info.IsDir() {
			directoryChanged, err := ensureDirectory(directorySpec{destination, 0, 0, 0o755})
			if err != nil {
				return false, err
			}
			changed = changed || directoryChanged
			continue
		}
		content, err := readBoundedRegular(sourcePath, maximumSingleFile)
		if err != nil {
			return false, err
		}
		fileChanged, err := reconcileFile(destination, content, 0, 0, 0o644, false)
		if err != nil {
			return false, err
		}
		changed = changed || fileChanged
	}
	if err := rejectUnexpectedTreeEntries(sourceRoot, destinationRoot); err != nil {
		return false, err
	}
	return changed, nil
}

func verifyWebTree(sourceRoot, destinationRoot string) error {
	if err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceRoot {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(destinationRoot, relative)
		if entry.IsDir() {
			return verifyDirectory(directorySpec{destination, 0, 0, 0o755})
		}
		content, err := readBoundedRegular(path, maximumSingleFile)
		if err != nil {
			return err
		}
		return verifyFile(destination, content, 0, 0, 0o644)
	}); err != nil {
		return err
	}
	return rejectUnexpectedTreeEntries(sourceRoot, destinationRoot)
}

func rejectUnexpectedTreeEntries(sourceRoot, destinationRoot string) error {
	return filepath.WalkDir(destinationRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == destinationRoot {
			return nil
		}
		relative, err := filepath.Rel(destinationRoot, path)
		if err != nil {
			return err
		}
		sourcePath := filepath.Join(sourceRoot, relative)
		sourceInfo, err := os.Lstat(sourcePath)
		if err != nil {
			return fmt.Errorf("unexpected immutable web entry: %s", path)
		}
		destinationInfo, err := entry.Info()
		if err != nil || sourceInfo.IsDir() != destinationInfo.IsDir() {
			return fmt.Errorf("web entry type mismatch: %s", path)
		}
		if sourceInfo.IsDir() {
			if destinationInfo.Mode().Perm() != 0o755 {
				return fmt.Errorf("web directory mode mismatch: %s", path)
			}
			return nil
		}
		sourceContent, err := readBoundedRegular(sourcePath, maximumSingleFile)
		if err != nil {
			return err
		}
		return verifyFile(path, sourceContent, 0, 0, 0o644)
	})
}

func copySourceFile(source, destination string, mode fs.FileMode) (bool, error) {
	content, err := readBoundedRegular(source, maximumSingleFile)
	if err != nil {
		return false, err
	}
	return reconcileFile(destination, content, 0, 0, mode, false)
}
