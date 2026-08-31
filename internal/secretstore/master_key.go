// SPDX-License-Identifier: AGPL-3.0-or-later

// Package secretstore owns filesystem material that must remain outside the
// Stackfort SQLite state database.
package secretstore

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const MasterKeyBytes = 32

// LoadOrCreateMasterKey reads an exact 256-bit key from a private regular file,
// or atomically creates it once. The key must never be included in state backups.
func LoadOrCreateMasterKey(path string) ([MasterKeyBytes]byte, error) {
	var key [MasterKeyBytes]byte
	if path == "" || !filepath.IsAbs(path) {
		return key, errors.New("master key path must be absolute")
	}
	cleanPath := filepath.Clean(path)
	info, err := os.Lstat(cleanPath)
	switch {
	case err == nil:
		return loadExistingMasterKey(cleanPath, info)
	case !errors.Is(err, os.ErrNotExist):
		return key, fmt.Errorf("inspect master key: %w", err)
	}

	directory := filepath.Dir(cleanPath)
	if err := ensureKeyDirectory(directory); err != nil {
		return key, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return key, fmt.Errorf("open master key directory: %w", err)
	}
	file, err := root.OpenFile(filepath.Base(cleanPath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = root.Close()
		if errors.Is(err, os.ErrExist) {
			info, inspectErr := os.Lstat(cleanPath)
			if inspectErr != nil {
				return key, fmt.Errorf("inspect raced master key: %w", inspectErr)
			}
			return loadExistingMasterKey(cleanPath, info)
		}
		return key, fmt.Errorf("create master key: %w", err)
	}
	if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
		return key, errors.Join(fmt.Errorf("generate master key: %w", err), file.Close(), root.Close())
	}
	if _, err := file.Write(key[:]); err != nil {
		return [MasterKeyBytes]byte{}, errors.Join(fmt.Errorf("write master key: %w", err), file.Close(), root.Close())
	}
	if err := file.Sync(); err != nil {
		return [MasterKeyBytes]byte{}, errors.Join(fmt.Errorf("sync master key: %w", err), file.Close(), root.Close())
	}
	if err := errors.Join(file.Close(), root.Close()); err != nil {
		return [MasterKeyBytes]byte{}, fmt.Errorf("close master key: %w", err)
	}
	if err := os.Chmod(cleanPath, 0o600); err != nil {
		return [MasterKeyBytes]byte{}, fmt.Errorf("protect master key: %w", err)
	}
	return key, nil
}

func loadExistingMasterKey(path string, info os.FileInfo) ([MasterKeyBytes]byte, error) {
	var key [MasterKeyBytes]byte
	if info.Mode()&os.ModeSymlink != 0 {
		return key, errors.New("master key must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return key, errors.New("master key must be a regular file")
	}
	if info.Size() != MasterKeyBytes {
		return key, fmt.Errorf("master key must contain exactly %d bytes", MasterKeyBytes)
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return key, fmt.Errorf("open master key directory: %w", err)
	}
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return key, errors.Join(fmt.Errorf("open master key: %w", err), root.Close())
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return key, errors.Join(fmt.Errorf("inspect opened master key: %w", err), file.Close(), root.Close())
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return key, errors.Join(errors.New("master key changed while being opened"), file.Close(), root.Close())
	}
	if err := file.Chmod(0o600); err != nil {
		return key, errors.Join(fmt.Errorf("protect master key: %w", err), file.Close(), root.Close())
	}
	if _, err := io.ReadFull(file, key[:]); err != nil {
		return key, errors.Join(fmt.Errorf("read master key: %w", err), file.Close(), root.Close())
	}
	var trailing [1]byte
	if count, err := file.Read(trailing[:]); err != nil && !errors.Is(err, io.EOF) {
		return key, errors.Join(fmt.Errorf("verify master key length: %w", err), file.Close(), root.Close())
	} else if count != 0 {
		return key, errors.Join(errors.New("master key contains trailing data"), file.Close(), root.Close())
	}
	if err := errors.Join(file.Close(), root.Close()); err != nil {
		return key, fmt.Errorf("close master key: %w", err)
	}
	return key, nil
}

func ensureKeyDirectory(path string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("master key parent must be a real directory")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
			return errors.New("master key parent must not be writable by group or others")
		}
		return nil
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create master key directory: %w", err)
		}
		// #nosec G302 -- this target is a private directory and needs owner execute permission.
		return os.Chmod(path, 0o700)
	default:
		return fmt.Errorf("inspect master key directory: %w", err)
	}
}
