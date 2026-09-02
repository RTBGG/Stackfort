// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package updateapply

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func requireSecureDirectory(path string, mode os.FileMode) error {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("directory has unsafe metadata")
	}
	defer unix.Close(descriptor)
	var stat unix.Stat_t
	if unix.Fstat(descriptor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		stat.Uid != 0 || stat.Gid != 0 || os.FileMode(stat.Mode).Perm() != mode.Perm() {
		return errors.New("directory is not root-owned")
	}
	return nil
}

func readSecureRegular(path string, mode os.FileMode, maximum int64) ([]byte, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.Join(err, errors.New("file has unsafe metadata"))
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, errors.New("open secure regular file")
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(descriptor, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != 0 || stat.Gid != 0 || os.FileMode(stat.Mode).Perm() != mode.Perm() ||
		stat.Size < 1 || stat.Size > maximum {
		return nil, errors.New("file is not root-owned")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(content)) > maximum {
		return nil, errors.New("read bounded private file")
	}
	return content, nil
}
