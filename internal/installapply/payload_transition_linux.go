// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

// AuthorizePayloadTransition pins exactly two inspected root-owned source
// trees. It is required for both forward activation and interrupted rollback.
// Existing destination content must still match one of these trees at each write.
func (runner *LinuxRunner) AuthorizePayloadTransition(first, second Source) error {
	if !runner.allowPackageTransition || first.Digest == second.Digest {
		return errors.New("payload transition requires a distinct verified update pair")
	}
	for _, source := range []Source{first, second} {
		if err := ValidateSourceTrust(source); err != nil {
			return err
		}
		inspected, err := InspectSource(source.Root)
		if err != nil || inspected.Version != source.Version || inspected.Digest != source.Digest {
			return errors.New("payload source changed after staging")
		}
	}
	runner.payloadSources = []Source{first, second}
	return nil
}

func (runner *LinuxRunner) payloadCounterpart(source Source) (Source, bool, error) {
	if !runner.allowPackageTransition {
		return Source{}, false, nil
	}
	if len(runner.payloadSources) != 2 {
		return Source{}, false, errors.New("update payload has no authorized source pair")
	}
	for index, approved := range runner.payloadSources {
		if source.Root == approved.Root && source.Digest == approved.Digest && source.Version == approved.Version {
			return runner.payloadSources[1-index], true, nil
		}
	}
	return Source{}, false, errors.New("update payload has no authorized source pair")
}

func transitionPayloadFile(desiredPath, otherPath, destination string, mode fs.FileMode) (bool, error) {
	desired, err := readBoundedRegular(desiredPath, maximumSingleFile)
	if err != nil {
		return false, err
	}
	if err := rejectSymlinkComponents(filepath.Dir(destination)); err != nil {
		return false, err
	}
	existing, exists, info, err := readExistingFile(destination)
	if err != nil {
		return false, err
	}
	if exists {
		if err := checkPayloadMetadata(info, mode); err != nil {
			return false, err
		}
		if bytes.Equal(existing, desired) {
			return false, nil
		}
		other, err := readBoundedRegular(otherPath, maximumSingleFile)
		if err != nil || !bytes.Equal(existing, other) {
			return false, fmt.Errorf("update refuses unrecognized payload: %s", destination)
		}
	}
	if err := atomicWriteFile(destination, desired, 0, 0, mode); err != nil {
		return false, err
	}
	return true, nil
}

func checkPayloadMetadata(info fs.FileInfo, mode fs.FileMode) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != mode ||
		info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return errors.New("installed payload metadata is not the expected root-owned regular file/directory")
	}
	return nil
}

func transitionPayloadTree(desiredRoot, otherRoot, destination string) (bool, error) {
	if err := rejectSymlinkComponents(destination); err != nil {
		return false, err
	}
	// Validate the whole installed union before mutation, including files that
	// disappear in the desired release. Tenant/unrecognized content is refused.
	var obsolete []string
	err := filepath.WalkDir(destination, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info.IsDir() {
			mode = 0o755
		}
		if err := checkPayloadMetadata(info, mode); err != nil {
			return err
		}
		if path == destination {
			return nil
		}
		relative, err := filepath.Rel(destination, path)
		if err != nil {
			return err
		}
		known := false
		for _, sourceRoot := range []string{desiredRoot, otherRoot} {
			sourcePath := filepath.Join(sourceRoot, relative)
			sourceInfo, err := os.Lstat(sourcePath)
			if err != nil || sourceInfo.IsDir() != info.IsDir() {
				continue
			}
			if info.IsDir() {
				known = true
				break
			}
			content, err := readBoundedRegular(sourcePath, maximumSingleFile)
			if err != nil {
				return err
			}
			if err := verifyFile(path, content, 0, 0, 0o644); err == nil {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("update refuses unrecognized web payload: %s", relative)
		}
		desiredInfo, err := os.Lstat(filepath.Join(desiredRoot, relative))
		if errors.Is(err, os.ErrNotExist) {
			obsolete = append(obsolete, relative)
			return nil
		}
		if err != nil {
			return err
		}
		if desiredInfo.IsDir() != info.IsDir() {
			return errors.New("payload file/directory transitions require an intermediate release")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	changed := false
	// Children sort after parents; removing in reverse order avoids recursion.
	sort.Sort(sort.Reverse(sort.StringSlice(obsolete)))
	root, err := os.OpenRoot(destination)
	if err != nil {
		return false, err
	}
	defer root.Close()
	for _, relative := range obsolete {
		if err := root.Remove(relative); err != nil {
			return changed, err
		}
		changed = true
		parent, err := root.Open(filepath.Dir(relative))
		if err != nil {
			return changed, err
		}
		if err := errors.Join(parent.Sync(), parent.Close()); err != nil {
			return changed, err
		}
	}
	err = filepath.WalkDir(desiredRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == desiredRoot {
			return nil
		}
		relative, err := filepath.Rel(desiredRoot, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			created, err := ensureDirectory(directorySpec{filepath.Join(destination, relative), 0, 0, 0o755})
			changed = changed || created
			if err == nil && created {
				return syncPayloadDirectory(filepath.Dir(filepath.Join(destination, relative)))
			}
			return err
		}
		updated, err := transitionPayloadFile(path, filepath.Join(otherRoot, relative), filepath.Join(destination, relative), 0o644)
		changed = changed || updated
		return err
	})
	if err != nil {
		return changed, err
	}
	if err := syncPayloadDirectory(destination); err != nil {
		return changed, err
	}
	return changed, verifyWebTree(desiredRoot, destination)
}

func syncPayloadDirectory(path string) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
