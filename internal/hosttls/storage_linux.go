// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hosttls

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/RTBGG/stackfort/internal/tlsartifact"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

type linuxStorage struct{}

func newStorage() storage { return linuxStorage{} }

func (linuxStorage) Stage(
	ctx context.Context,
	operationID string,
	bundle tlsartifact.Bundle,
) (Result, error) {
	parsed, err := uuid.Parse(operationID)
	if err != nil || parsed.String() != operationID || parsed.Version() != uuid.Version(7) {
		return Result{}, ErrMutationFailed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	directory, err := openCertificateDirectory(bundle.CertificateID)
	if err != nil {
		return Result{}, err
	}
	defer unix.Close(directory)
	certificateChanged, err := reconcileFileAt(
		directory, operationID+"-chain", tlsartifact.FullChainFileName, []byte(bundle.FullChainPEM), 0o644,
	)
	if err != nil {
		return Result{}, err
	}
	keyChanged, err := reconcileFileAt(
		directory, operationID+"-key", tlsartifact.PrivateKeyFileName, []byte(bundle.PrivateKeyPEM), 0o600,
	)
	if err != nil {
		return Result{}, err
	}
	return Result{Changed: certificateChanged || keyChanged, CertificateID: bundle.CertificateID}, nil
}

func openCertificateDirectory(certificateID string) (int, error) {
	cleaned := filepath.Clean(tlsartifact.Directory)
	if !filepath.IsAbs(cleaned) || strings.Contains(certificateID, "/") {
		return -1, ErrMutationFailed
	}
	descriptor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, ErrUnavailable
	}
	components := append(strings.Split(strings.TrimPrefix(cleaned, "/"), "/"), certificateID)
	for index, component := range components {
		isManagedChild := index >= len(components)-2
		next, openErr := unix.Openat(
			descriptor, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
		)
		if errors.Is(openErr, unix.ENOENT) && isManagedChild {
			mkdirErr := unix.Mkdirat(descriptor, component, 0o711)
			if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(descriptor)
				return -1, ErrMutationFailed
			}
			next, openErr = unix.Openat(
				descriptor, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0,
			)
		}
		_ = unix.Close(descriptor)
		if openErr != nil {
			if errors.Is(openErr, unix.ENOENT) {
				return -1, ErrUnavailable
			}
			return -1, ErrConflict
		}
		descriptor = next
		var status unix.Stat_t
		if unix.Fstat(descriptor, &status) != nil || status.Uid != 0 || status.Gid != 0 ||
			status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Mode&0o022 != 0 {
			_ = unix.Close(descriptor)
			return -1, ErrConflict
		}
		if isManagedChild && (unix.Fchown(descriptor, 0, 0) != nil || unix.Fchmod(descriptor, 0o711) != nil) {
			_ = unix.Close(descriptor)
			return -1, ErrMutationFailed
		}
	}
	return descriptor, nil
}

func reconcileFileAt(directory int, temporaryID, name string, content []byte, mode uint32) (bool, error) {
	existing, exists, err := readFileAt(directory, name, int64(len(content)+1), mode)
	if err != nil {
		return false, err
	}
	if exists {
		if bytes.Equal(existing, content) {
			return false, nil
		}
		return false, ErrConflict
	}
	temporaryName := "." + temporaryID + ".tmp"
	_ = unix.Unlinkat(directory, temporaryName, 0)
	descriptor, err := unix.Openat(directory, temporaryName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, mode)
	if err != nil {
		return false, ErrMutationFailed
	}
	cleanup := true
	defer func() {
		if descriptor >= 0 {
			_ = unix.Close(descriptor)
		}
		if cleanup {
			_ = unix.Unlinkat(directory, temporaryName, 0)
		}
	}()
	remaining := content
	for len(remaining) > 0 {
		written, writeErr := unix.Write(descriptor, remaining)
		if writeErr != nil || written <= 0 {
			return false, ErrMutationFailed
		}
		remaining = remaining[written:]
	}
	if unix.Fchown(descriptor, 0, 0) != nil || unix.Fchmod(descriptor, mode) != nil ||
		unix.Fsync(descriptor) != nil || unix.Close(descriptor) != nil {
		descriptor = -1
		return false, ErrMutationFailed
	}
	descriptor = -1
	if unix.Renameat(directory, temporaryName, directory, name) != nil || unix.Fsync(directory) != nil {
		return false, ErrMutationFailed
	}
	cleanup = false
	return true, nil
}

func readFileAt(directory int, name string, maximum int64, mode uint32) ([]byte, bool, error) {
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
		status.Uid != 0 || status.Gid != 0 || status.Mode&0o7777 != mode ||
		status.Nlink != 1 || status.Size < 1 || status.Size > maximum {
		return nil, false, ErrConflict
	}
	content := make([]byte, status.Size)
	offset := 0
	for offset < len(content) {
		count, readErr := unix.Read(descriptor, content[offset:])
		offset += count
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || count == 0 {
			return nil, false, ErrMutationFailed
		}
	}
	if offset != len(content) {
		return nil, false, ErrMutationFailed
	}
	return content, true, nil
}
