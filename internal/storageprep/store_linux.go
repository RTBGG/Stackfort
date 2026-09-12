// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package storageprep

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const (
	JournalPath = "/var/lib/stackfort-installer/storage-state.json"
	journalName = "storage-state.json"
	lockName    = "install.lock" // Shared with the existing installer, not a second lock domain.
)

// FileStore owns a directory descriptor and the exclusive installation lock.
// No production CLI opens it for writing until the native backend is qualified.
type FileStore struct {
	directory, lock int
	closed          bool
}

func OpenFileStore() (*FileStore, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("storage preparation requires root")
	}
	directory, err := openStateDirectory(true)
	if err != nil {
		return nil, err
	}
	store, err := lockStoreAt(directory)
	if err != nil {
		_ = unix.Close(directory)
	}
	return store, err
}

func lockStoreAt(directory int) (*FileStore, error) {
	return lockStoreAtMode(directory, true)
}

// OpenExistingFileStore never creates a directory or lock. Missing state
// directories are ordinary absence; a missing/unsafe lock in existing state is
// an error, not permission to read an incoherent snapshot.
func OpenExistingFileStore() (*FileStore, bool, error) {
	if os.Geteuid() != 0 {
		return nil, false, errors.New("storage inspection requires root")
	}
	directory, err := openStateDirectory(false)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	store, err := lockStoreAtMode(directory, false)
	if err != nil {
		_ = unix.Close(directory)
		return nil, false, err
	}
	return store, true, nil
}

func lockStoreAtMode(directory int, create bool) (*FileStore, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if create {
		flags = unix.O_CREAT | unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	}
	lock, err := unix.Openat(directory, lockName, flags, 0o600)
	if err != nil {
		return nil, err
	}
	if err := trustedFile(lock); err != nil {
		_ = unix.Close(lock)
		return nil, err
	}
	if err := unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(lock)
		return nil, errors.New("another Stackfort installer process is active")
	}
	return &FileStore{directory: directory, lock: lock}, nil
}

func (store *FileStore) Close() error {
	if store == nil || store.closed {
		return nil
	}
	store.closed = true
	return errors.Join(unix.Close(store.lock), unix.Close(store.directory))
}

func (store *FileStore) Load() (State, bool, error) {
	if store == nil || store.closed {
		return State{}, false, errors.New("storage journal is not locked")
	}
	return readStateAt(store.directory)
}

func (store *FileStore) Save(state State) error {
	if store == nil || store.closed {
		return errors.New("storage journal is not locked")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	// Refuse an unsafe or corrupt existing target rather than overwriting it.
	previous, exists, err := readStateAt(store.directory)
	if err != nil {
		return err
	}
	if err := ValidateTransition(previous, exists, state); err != nil {
		return err
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil || len(content) >= maximumStateBytes {
		return errors.New("encode storage journal")
	}
	content = append(content, '\n')
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := ".storage-state-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(store.directory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temporary)
	defer file.Close()
	defer unix.Unlinkat(store.directory, temporary, 0)
	if err := trustedFile(fd); err != nil {
		return err
	}
	if n, err := file.Write(content); err != nil {
		return err
	} else if n != len(content) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat(store.directory, temporary, store.directory, journalName); err != nil {
		return err
	}
	return unix.Fsync(store.directory)
}

// CheckInactive is read-only: absence is allowed, but any preparation journal
// (including Ready) blocks production installation/update until qualified live
// resume exists. Malformed/unsafe state cannot be treated as absence.
func CheckInactive() error {
	directory, err := openStateDirectory(false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(directory)
	return checkInactiveAt(directory)
}

func checkInactiveAt(directory int) error {
	// Runtime sealing precedes the journal. Even a partial executable or a
	// dangling link is preparation evidence, never an ordinary fresh install.
	for _, name := range []string{"native-runtime-installer", "native-runtime-intent.json", "native-prerequisites.json", "native-prerequisite-installer", "native-recovery-choice.json", "native-boot-build", "native-onboarding-source.json", "native-setup.json", "native-setup-registered.json"} {
		var stat unix.Stat_t
		err := unix.Fstatat(directory, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if err == nil {
			return ErrNotQualified
		}
		if !errors.Is(err, unix.ENOENT) {
			return err
		}
	}
	_, exists, err := readStateAt(directory)
	if err != nil {
		return err
	}
	if exists {
		return ErrNotQualified
	}
	return nil
}

func readStateAt(directory int) (State, bool, error) {
	fd, err := unix.Openat(directory, journalName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	file := os.NewFile(uintptr(fd), journalName)
	defer file.Close()
	if err := trustedFile(fd); err != nil {
		return State{}, false, err
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumStateBytes+1))
	if err != nil || len(content) > maximumStateBytes {
		return State{}, false, errors.New("read bounded storage journal")
	}
	state, err := DecodeState(content)
	if err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

func trustedFile(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Uid != 0 || stat.Gid != 0 || stat.Mode != unix.S_IFREG|0o600 || stat.Nlink != 1 || stat.Size > maximumStateBytes {
		return errors.New("storage journal/lock has unsafe metadata")
	}
	return nil
}

func openStateDirectory(create bool) (int, error) {
	return openStateDirectoryAt("/", create)
}

// root is a private test seam; production always starts at the literal /.
func openStateDirectoryAt(root string, create bool) (int, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	for index, component := range []string{"var", "lib", "stackfort-installer"} {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o022 != 0 {
			_ = unix.Close(fd)
			return -1, errors.New("unsafe storage journal ancestor")
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && create && index == 2 {
			if err := unix.Mkdirat(fd, component, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
				_ = unix.Close(fd)
				return -1, err
			}
			if err := unix.Fsync(fd); err != nil {
				_ = unix.Close(fd)
				return -1, err
			}
			next, openErr = unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		_ = unix.Close(fd)
		if openErr != nil {
			return -1, openErr
		}
		fd = next
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Uid != 0 || stat.Gid != 0 || stat.Mode != unix.S_IFDIR|0o700 {
		_ = unix.Close(fd)
		return -1, errors.New("unsafe storage journal directory")
	}
	return fd, nil
}
