// SPDX-License-Identifier: AGPL-3.0-or-later

package phpmyadminbroker

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// LoadSharedKey reads the installer-owned key without changing its group-read
// permission, which is required by the dedicated phpMyAdmin service account.
func LoadSharedKey(path string) ([SharedKeyBytes]byte, error) {
	var key [SharedKeyBytes]byte
	if path == "" || !filepath.IsAbs(path) {
		return key, errors.New("phpMyAdmin broker key path must be absolute")
	}
	cleanPath := filepath.Clean(path)
	info, err := os.Lstat(cleanPath)
	if err != nil {
		return key, fmt.Errorf("inspect phpMyAdmin broker key: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return key, errors.New("phpMyAdmin broker key must be a regular file and not a symbolic link")
	}
	if info.Size() != SharedKeyBytes {
		return key, fmt.Errorf("phpMyAdmin broker key must contain exactly %d bytes", SharedKeyBytes)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o137 != 0 {
		return key, errors.New("phpMyAdmin broker key permissions are too broad")
	}
	root, err := os.OpenRoot(filepath.Dir(cleanPath))
	if err != nil {
		return key, fmt.Errorf("open phpMyAdmin broker key directory: %w", err)
	}
	file, err := root.Open(filepath.Base(cleanPath))
	if err != nil {
		return key, errors.Join(fmt.Errorf("open phpMyAdmin broker key: %w", err), root.Close())
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return key, errors.Join(fmt.Errorf("inspect opened phpMyAdmin broker key: %w", err), file.Close(), root.Close())
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return key, errors.Join(errors.New("phpMyAdmin broker key changed while being opened"), file.Close(), root.Close())
	}
	if _, err := io.ReadFull(file, key[:]); err != nil {
		return [SharedKeyBytes]byte{}, errors.Join(fmt.Errorf("read phpMyAdmin broker key: %w", err), file.Close(), root.Close())
	}
	var trailing [1]byte
	if count, readErr := file.Read(trailing[:]); readErr != nil && !errors.Is(readErr, io.EOF) {
		return [SharedKeyBytes]byte{}, errors.Join(fmt.Errorf("verify phpMyAdmin broker key length: %w", readErr), file.Close(), root.Close())
	} else if count != 0 {
		return [SharedKeyBytes]byte{}, errors.Join(errors.New("phpMyAdmin broker key contains trailing data"), file.Close(), root.Close())
	}
	if err := errors.Join(file.Close(), root.Close()); err != nil {
		return [SharedKeyBytes]byte{}, fmt.Errorf("close phpMyAdmin broker key: %w", err)
	}
	return key, nil
}
