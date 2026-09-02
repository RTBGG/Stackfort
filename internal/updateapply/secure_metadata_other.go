// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux

package updateapply

import (
	"errors"
	"io"
	"os"
)

func requireSecureDirectory(path string, _ os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, errors.New("directory has unsafe metadata"))
	}
	return nil
}

func readSecureRegular(path string, _ os.FileMode, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 1 || info.Size() > maximum {
		return nil, errors.Join(err, errors.New("file has unsafe metadata"))
	}
	// #nosec G304 -- the non-Linux implementation serves local tests; production Linux uses O_NOFOLLOW.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(content)) > maximum {
		return nil, errors.New("read bounded private file")
	}
	return content, nil
}
