// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostacme

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/RTBGG/stackfort/internal/acmehttp01"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

type linuxStorage struct{}

func newStorage() storage { return linuxStorage{} }

func (linuxStorage) Reconcile(
	ctx context.Context,
	operationID string,
	intent acmehttp01.Intent,
) (Result, error) {
	parsed, err := uuid.Parse(operationID)
	if err != nil || parsed.String() != operationID || parsed.Version() != uuid.Version(7) {
		return Result{}, ErrMutationFailed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	directory, err := openManagedChallengeDirectory()
	if err != nil {
		return Result{}, err
	}
	defer unix.Close(directory)
	switch intent.Action {
	case acmehttp01.ActionPresent:
		changed, presentErr := presentAt(directory, operationID, intent.Token, []byte(intent.KeyAuthorization))
		return Result{Changed: changed, Presented: presentErr == nil}, presentErr
	case acmehttp01.ActionCleanup:
		changed, cleanupErr := cleanupAt(directory, intent.Token)
		return Result{Changed: changed, Presented: false}, cleanupErr
	default:
		return Result{}, ErrMutationFailed
	}
}

func openManagedChallengeDirectory() (int, error) {
	cleaned := filepath.Clean(acmehttp01.ChallengeDirectory)
	if !filepath.IsAbs(cleaned) {
		return -1, ErrMutationFailed
	}
	descriptor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, ErrMutationFailed
	}
	components := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	for index, component := range components {
		created := false
		next, openErr := unix.Openat(descriptor, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) {
			mkdirErr := unix.Mkdirat(descriptor, component, 0o755)
			if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(descriptor)
				return -1, ErrMutationFailed
			}
			created = mkdirErr == nil
			next, openErr = unix.Openat(descriptor, component,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		_ = unix.Close(descriptor)
		if openErr != nil {
			return -1, ErrConflict
		}
		descriptor = next
		if created && (unix.Fchown(descriptor, 0, 0) != nil || unix.Fchmod(descriptor, 0o755) != nil) {
			_ = unix.Close(descriptor)
			return -1, ErrMutationFailed
		}
		var status unix.Stat_t
		if unix.Fstat(descriptor, &status) != nil || status.Uid != 0 || status.Gid != 0 || status.Mode&0o022 != 0 ||
			(index >= len(components)-2 && status.Mode&0o7777 != 0o755) {
			_ = unix.Close(descriptor)
			return -1, ErrConflict
		}
	}
	return descriptor, nil
}

func presentAt(directory int, operationID, token string, content []byte) (bool, error) {
	existing, exists, err := readManagedFileAt(directory, token)
	if err != nil {
		return false, err
	}
	if exists {
		if string(existing) == string(content) {
			return false, nil
		}
		return false, ErrConflict
	}
	temporaryName := "." + operationID + ".tmp"
	_ = unix.Unlinkat(directory, temporaryName, 0)
	temporary, err := unix.Openat(directory, temporaryName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return false, ErrMutationFailed
	}
	cleanup := true
	defer func() {
		_ = unix.Close(temporary)
		if cleanup {
			_ = unix.Unlinkat(directory, temporaryName, 0)
		}
	}()
	for len(content) > 0 {
		written, writeErr := unix.Write(temporary, content)
		if writeErr != nil || written <= 0 {
			return false, ErrMutationFailed
		}
		content = content[written:]
	}
	if unix.Fchmod(temporary, 0o644) != nil || unix.Fsync(temporary) != nil || unix.Close(temporary) != nil {
		temporary = -1
		return false, ErrMutationFailed
	}
	temporary = -1
	if unix.Renameat(directory, temporaryName, directory, token) != nil || unix.Fsync(directory) != nil {
		return false, ErrMutationFailed
	}
	cleanup = false
	return true, nil
}

func cleanupAt(directory int, token string) (bool, error) {
	_, exists, err := readManagedFileAt(directory, token)
	if err != nil || !exists {
		return false, err
	}
	if unix.Unlinkat(directory, token, 0) != nil || unix.Fsync(directory) != nil {
		return false, ErrMutationFailed
	}
	return true, nil
}

func readManagedFileAt(directory int, name string) ([]byte, bool, error) {
	descriptor, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, ErrConflict
	}
	defer unix.Close(descriptor)
	var status unix.Stat_t
	if unix.Fstat(descriptor, &status) != nil || status.Mode&unix.S_IFMT != unix.S_IFREG ||
		status.Uid != 0 || status.Gid != 0 || status.Mode&0o7777 != 0o644 || status.Size > 512 {
		return nil, false, ErrConflict
	}
	file := make([]byte, status.Size)
	read := 0
	for read < len(file) {
		count, readErr := unix.Read(descriptor, file[read:])
		read += count
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || count == 0 {
			return nil, false, ErrMutationFailed
		}
	}
	if read != len(file) {
		return nil, false, ErrMutationFailed
	}
	return file, true, nil
}
