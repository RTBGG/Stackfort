// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

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
	DefaultJournalDirectory = "/var/lib/stackfort-installer"
	DefaultJournalPath      = DefaultJournalDirectory + "/install-state.json"
	DefaultLockPath         = DefaultJournalDirectory + "/install.lock"
	maximumJournalBytes     = 1 << 20
)

type FileStore struct {
	directory string
	path      string
}

type Lock struct{ file *os.File }

func NewFileStore() *FileStore {
	return &FileStore{directory: DefaultJournalDirectory, path: DefaultJournalPath}
}

func (store *FileStore) AcquireLock() (*Lock, error) {
	info, err := os.Lstat(store.directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(store.directory, 0o700); err != nil {
			return nil, fmt.Errorf("create installer state directory: %w", err)
		}
		info, err = os.Lstat(store.directory)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("installer state directory conflicts with host state")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != 0o700 {
		return nil, errors.New("secure installer state directory")
	}
	fd, err := unix.Open(DefaultLockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	var lockStat unix.Stat_t
	if unix.Fstat(fd, &lockStat) != nil || lockStat.Uid != 0 || lockStat.Gid != 0 ||
		lockStat.Mode&unix.S_IFMT != unix.S_IFREG || lockStat.Mode&0o777 != 0o600 {
		_ = unix.Close(fd)
		return nil, errors.New("installer lock has unsafe metadata")
	}
	file := os.NewFile(uintptr(fd), DefaultLockPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open installer lock")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.New("another Stackfort installer process is active")
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
		return Journal{}, false, errors.New("open installer journal")
	}
	defer file.Close()
	info, err := file.Stat()
	stat, ok := info.Sys().(*syscall.Stat_t)
	if err != nil || !ok || stat.Uid != 0 || stat.Gid != 0 || !info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 || info.Size() > maximumJournalBytes {
		return Journal{}, false, errors.New("installer journal has unsafe metadata")
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumJournalBytes+1))
	if err != nil || len(content) > maximumJournalBytes {
		return Journal{}, false, errors.New("installer journal exceeds size limit")
	}
	var journal Journal
	decoder := json.NewDecoder(bytesReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return Journal{}, false, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Journal{}, false, errors.New("installer journal contains trailing data")
	}
	return journal, true, nil
}

func (store *FileStore) Save(journal Journal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	content, err := json.MarshalIndent(journal, "", "  ")
	if err != nil || len(content) > maximumJournalBytes-1 {
		return errors.New("encode installer journal")
	}
	content = append(content, '\n')
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := filepath.Join(store.directory, ".install-state-"+hex.EncodeToString(random[:]))
	// #nosec G304 -- temporary is random, exclusive, and rooted in the fixed root-only installer state directory.
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
		return errors.New("write installer journal")
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

// bytesReader keeps the JSON decoder construction local without exposing a
// mutable backing file or accepting a second read.
func bytesReader(content []byte) io.Reader { return &sliceReader{content: content} }

type sliceReader struct{ content []byte }

func (reader *sliceReader) Read(target []byte) (int, error) {
	if len(reader.content) == 0 {
		return 0, io.EOF
	}
	count := copy(target, reader.content)
	reader.content = reader.content[count:]
	return count, nil
}
