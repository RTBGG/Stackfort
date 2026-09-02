// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package updateapply

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	DefaultStateDirectory   = "/var/lib/stackfort-updater"
	DefaultJournalPath      = DefaultStateDirectory + "/update-state.json"
	DefaultLockPath         = DefaultStateDirectory + "/update.lock"
	DefaultReleaseDirectory = DefaultStateDirectory + "/releases"
	DefaultBackupDirectory  = DefaultStateDirectory + "/backups"
	maximumJournalBytes     = 1 << 20
)

type FileStore struct {
	directory string
	path      string
}

type Lock struct{ file *os.File }

func NewFileStore() *FileStore {
	return &FileStore{directory: DefaultStateDirectory, path: DefaultJournalPath}
}

func (store *FileStore) AcquireLock() (*Lock, error) {
	if err := secureRootDirectory(store.directory); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(store.directory, filepath.Base(DefaultLockPath))
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != 0 || stat.Gid != 0 ||
		stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o777 != 0o600 {
		_ = unix.Close(fd)
		return nil, errors.New("updater lock has unsafe metadata")
	}
	file := os.NewFile(uintptr(fd), lockPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open updater lock")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.New("another Stackfort update is active")
	}
	return &Lock{file: file}, nil
}

func (lock *Lock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	_ = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	return lock.file.Close()
}

func (store *FileStore) Load() (Journal, bool, error) {
	exists, err := inspectSecureRootDirectory(store.directory)
	if err != nil {
		return Journal{}, false, err
	}
	if !exists {
		return Journal{}, false, nil
	}
	fd, err := unix.Open(store.path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Journal{}, false, nil
	}
	if err != nil {
		return Journal{}, false, err
	}
	file := os.NewFile(uintptr(fd), store.path)
	if file == nil {
		_ = unix.Close(fd)
		return Journal{}, false, errors.New("open updater journal")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Journal{}, false, errors.New("inspect updater journal")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || !info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 || info.Size() > maximumJournalBytes {
		return Journal{}, false, errors.New("updater journal has unsafe metadata")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumJournalBytes+1))
	if err != nil || len(content) > maximumJournalBytes {
		return Journal{}, false, errors.New("updater journal exceeds size limit")
	}
	var journal Journal
	decoder := json.NewDecoder(newSliceReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return Journal{}, false, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Journal{}, false, errors.New("updater journal contains trailing data")
	}
	return journal, true, nil
}

func (store *FileStore) Save(journal Journal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	if err := secureRootDirectory(store.directory); err != nil {
		return err
	}
	content, err := json.MarshalIndent(journal, "", "  ")
	if err != nil || len(content) > maximumJournalBytes-1 {
		return errors.New("encode updater journal")
	}
	content = append(content, '\n')
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := filepath.Join(store.directory, ".update-state-"+hex.EncodeToString(random[:]))
	// #nosec G304 -- the random exclusive file is below a fixed root-owned directory.
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
	if _, err := file.Write(content); err != nil || file.Sync() != nil || file.Close() != nil {
		return errors.New("write updater journal")
	}
	if err := os.Rename(temporary, store.path); err != nil {
		return err
	}
	cleanup = false
	directory, err := os.Open(store.directory)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func secureRootDirectory(path string) error {
	exists, err := inspectSecureRootDirectory(path)
	if err != nil {
		return err
	}
	if !exists {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("create updater state directory: %w", err)
		}
		_, err = inspectSecureRootDirectory(path)
	}
	return err
}

func inspectSecureRootDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("updater state directory conflicts with host state")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != 0o700 {
		return false, errors.New("updater state directory has unsafe metadata")
	}
	return true, nil
}

type sliceReader struct{ content []byte }

func newSliceReader(content []byte) *sliceReader { return &sliceReader{content: content} }
func (reader *sliceReader) Read(target []byte) (int, error) {
	if len(reader.content) == 0 {
		return 0, io.EOF
	}
	count := copy(target, reader.content)
	reader.content = reader.content[count:]
	return count, nil
}
