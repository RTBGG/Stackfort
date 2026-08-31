// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfilesystem

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/hostingstorage"
	"golang.org/x/sys/unix"
)

const (
	fsIOCFSGetXAttr    = 0x801c581f
	fsIOCFSSetXAttr    = 0x401c5820
	fsXFlagProjInherit = 0x00000200
)

type filesystemXAttr struct {
	XFlags     uint32
	ExtSize    uint32
	Nextents   uint32
	ProjectID  uint32
	CowExtSize uint32
	Pad        [8]byte
}

const filesystemXAttrSize = int(unsafe.Sizeof(filesystemXAttr{}))

var (
	_ [28 - filesystemXAttrSize]byte
	_ [filesystemXAttrSize - 28]byte
)

type layoutDirectory struct {
	name string
	mode uint32
}

var managedLayout = [...]layoutDirectory{
	{name: "applications", mode: 0o750},
	{name: "backups", mode: 0o700},
	{name: "domains", mode: 0o750},
	{name: "logs", mode: 0o750},
	{name: "public_html", mode: 0o750},
	{name: "tmp", mode: 0o700},
}

type linuxDirectoryManager struct{}

func newDirectoryManager() directoryManager { return linuxDirectoryManager{} }

func (linuxDirectoryManager) EnsureLayout(spec hostingstorage.Spec) (LayoutResult, error) {
	account, device, err := openAccountRoot(spec.Identity)
	if err != nil {
		return LayoutResult{}, err
	}
	defer unix.Close(account)
	assigned, err := ensureProject(account, spec.ProjectID)
	if err != nil {
		return LayoutResult{}, err
	}
	result := LayoutResult{ProjectAssigned: assigned, DirectoriesCreated: []string{}}
	for _, directory := range managedLayout {
		created, child, err := ensureDirectoryAt(
			account, directory.name, directory.mode, spec.Identity.UID, spec.Identity.GID, device,
		)
		if err != nil {
			return LayoutResult{}, err
		}
		_ = unix.Close(child)
		if created {
			result.DirectoriesCreated = append(result.DirectoriesCreated, directory.name)
		}
	}
	return result, nil
}

func (linuxDirectoryManager) EnsureDocumentRoot(identity hostingidentity.Spec, relativePath string) (bool, error) {
	account, device, err := openAccountRoot(identity)
	if err != nil {
		return false, err
	}
	defer unix.Close(account)
	attributes, err := getFilesystemXAttr(account)
	if err != nil {
		return false, ErrMutationFailed
	}
	if attributes.ProjectID != identity.UID || attributes.XFlags&fsXFlagProjInherit == 0 {
		return false, ErrMigrationRequired
	}
	return ensureRelativeDirectories(account, device, identity.UID, identity.GID, relativePath)
}

func ensureRelativeDirectories(account int, device uint64, uid, gid uint32, relativePath string) (bool, error) {
	current := account
	createdAny := false
	for _, component := range strings.Split(relativePath, "/") {
		created, child, err := ensureDirectoryAt(current, component, 0o750, uid, gid, device)
		if current != account {
			_ = unix.Close(current)
		}
		if err != nil {
			return false, err
		}
		createdAny = createdAny || created
		current = child
	}
	if current != account {
		_ = unix.Close(current)
	}
	return createdAny, nil
}

func openAccountRoot(identity hostingidentity.Spec) (int, uint64, error) {
	descriptor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, 0, ErrMutationFailed
	}
	var managedDevice uint64
	for index, component := range []string{"srv", "hosting", "accounts", identity.AccountID} {
		next, openErr := unix.Openat(descriptor, component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = unix.Close(descriptor)
		if openErr != nil {
			return -1, 0, ErrConflict
		}
		descriptor = next
		var componentStatus unix.Stat_t
		if err := unix.Fstat(descriptor, &componentStatus); err != nil {
			_ = unix.Close(descriptor)
			return -1, 0, ErrMutationFailed
		}
		if index < 3 && (componentStatus.Uid != 0 || componentStatus.Gid != 0 || componentStatus.Mode&0o022 != 0) {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
		if index == 1 {
			managedDevice = uint64(componentStatus.Dev)
		}
		if index > 1 && uint64(componentStatus.Dev) != managedDevice {
			_ = unix.Close(descriptor)
			return -1, 0, ErrConflict
		}
	}
	var status unix.Stat_t
	if err := unix.Fstat(descriptor, &status); err != nil {
		_ = unix.Close(descriptor)
		return -1, 0, ErrMutationFailed
	}
	if status.Uid != identity.UID || status.Gid != identity.GID || status.Mode&0o7777 != 0o750 {
		_ = unix.Close(descriptor)
		return -1, 0, ErrConflict
	}
	return descriptor, managedDevice, nil
}

func ensureDirectoryAt(parent int, name string, mode uint32, uid, gid uint32, device uint64) (bool, int, error) {
	created := false
	child, err := unix.Openat(parent, name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		mkdirErr := unix.Mkdirat(parent, name, mode)
		if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			return false, -1, ErrMutationFailed
		}
		created = mkdirErr == nil
		child, err = unix.Openat(parent, name,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return false, -1, ErrConflict
	}
	if created {
		if err := unix.Fchown(child, int(uid), int(gid)); err != nil {
			_ = unix.Close(child)
			return false, -1, ErrMutationFailed
		}
		if err := unix.Fchmod(child, mode); err != nil {
			_ = unix.Close(child)
			return false, -1, ErrMutationFailed
		}
	}
	var status unix.Stat_t
	if err := unix.Fstat(child, &status); err != nil {
		_ = unix.Close(child)
		return false, -1, ErrMutationFailed
	}
	if status.Uid != uid || status.Gid != gid || status.Mode&0o7777 != mode || uint64(status.Dev) != device {
		_ = unix.Close(child)
		return false, -1, ErrConflict
	}
	return created, child, nil
}

func ensureProject(descriptor int, projectID uint32) (bool, error) {
	attributes, err := getFilesystemXAttr(descriptor)
	if err != nil {
		return false, ErrMutationFailed
	}
	if attributes.ProjectID == projectID && attributes.XFlags&fsXFlagProjInherit != 0 {
		return false, nil
	}
	if attributes.ProjectID != 0 && attributes.ProjectID != projectID {
		return false, ErrConflict
	}
	empty, err := directoryEmpty(descriptor)
	if err != nil {
		return false, ErrMutationFailed
	}
	if !empty {
		return false, ErrMigrationRequired
	}
	attributes.ProjectID = projectID
	attributes.XFlags |= fsXFlagProjInherit
	if err := setFilesystemXAttr(descriptor, &attributes); err != nil {
		return false, ErrMutationFailed
	}
	verified, err := getFilesystemXAttr(descriptor)
	if err != nil || verified.ProjectID != projectID || verified.XFlags&fsXFlagProjInherit == 0 {
		return false, ErrMutationFailed
	}
	return true, nil
}

func directoryEmpty(descriptor int) (bool, error) {
	duplicate, err := unix.Dup(descriptor)
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(duplicate), "stackfort-account-root")
	if file == nil {
		_ = unix.Close(duplicate)
		return false, errors.New("create directory handle")
	}
	_, readErr := file.Readdirnames(1)
	closeErr := file.Close()
	if errors.Is(readErr, io.EOF) {
		return closeErr == nil, closeErr
	}
	if readErr != nil {
		return false, errors.Join(readErr, closeErr)
	}
	return false, closeErr
}

func getFilesystemXAttr(descriptor int) (filesystemXAttr, error) {
	var attributes filesystemXAttr
	// #nosec G103 -- the fixed FS_IOC_FSGETXATTR Linux ABI requires a pointer
	// to the compile-time size-checked 28-byte fsxattr structure above.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(descriptor), fsIOCFSGetXAttr,
		uintptr(unsafe.Pointer(&attributes)))
	runtime.KeepAlive(&attributes)
	if errno != 0 {
		return filesystemXAttr{}, errno
	}
	return attributes, nil
}

func setFilesystemXAttr(descriptor int, attributes *filesystemXAttr) error {
	// #nosec G103 -- the fixed FS_IOC_FSSETXATTR Linux ABI requires a pointer
	// to the compile-time size-checked 28-byte fsxattr structure above.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(descriptor), fsIOCFSSetXAttr,
		uintptr(unsafe.Pointer(attributes)))
	runtime.KeepAlive(attributes)
	if errno != 0 {
		return errno
	}
	return nil
}
