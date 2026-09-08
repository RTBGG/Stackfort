// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostidentity

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingoci"
	"golang.org/x/sys/unix"
)

// A newly mounted hosting filesystem can have an unlabeled root. Seed only
// the empty, account-private container store, so its descendants inherit the
// container type before rootless Podman creates image layers. Never relabel
// populated stores or recurse through tenant-controlled trees.
func labelEmptyContainerStorage(spec hostingoci.Spec) (bool, error) {
	if hostingoci.Validate(spec) != nil {
		return false, ErrIdentityConflict
	}
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false, err
	}
	defer unix.Close(root)
	current := root
	for _, component := range strings.Split(strings.TrimPrefix(spec.StorageRoot, "/"), "/") {
		next, err := openDirectoryAt(current, component)
		if err != nil {
			return false, err
		}
		defer unix.Close(next)
		current = next
	}
	return labelEmptyStorageFD(current, spec.Identity.UID, spec.Identity.GID, unix.Fgetxattr, unix.Fsetxattr)
}

func labelEmptyStorageFD(current int, uid, gid uint32,
	getAttribute func(int, string, []byte) (int, error),
	setAttribute func(int, string, []byte, int) error,
) (bool, error) {
	var status unix.Stat_t
	if err := unix.Fstat(current, &status); err != nil || status.Uid != uid ||
		status.Gid != gid || status.Mode&unix.S_IFMT != unix.S_IFDIR || status.Mode&0o7777 != 0o700 {
		return false, ErrIdentityConflict
	}
	context := make([]byte, 4096)
	size, err := getAttribute(current, "security.selinux", context)
	if err != nil && !errors.Is(err, unix.ENODATA) {
		return false, err
	}
	if err == nil && strings.TrimRight(string(context[:size]), "\x00") == hostingoci.StorageSELinuxContext {
		return false, nil
	}
	// Duplicate the already anchored descriptor for ReadDir; os.File owns it.
	duplicate, err := unix.Openat(current, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	directory := os.NewFile(uintptr(duplicate), "private-container-storage")
	defer directory.Close()
	entries, readErr := directory.ReadDir(1)
	if len(entries) != 0 || !errors.Is(readErr, io.EOF) {
		return false, ErrIdentityConflict
	}
	wanted := []byte(hostingoci.StorageSELinuxContext + "\x00")
	if err := setAttribute(current, "security.selinux", wanted, 0); err != nil {
		return false, err
	}
	size, err = getAttribute(current, "security.selinux", context)
	if err != nil || strings.TrimRight(string(context[:size]), "\x00") != hostingoci.StorageSELinuxContext {
		return false, ErrMutationFailed
	}
	return true, nil
}
