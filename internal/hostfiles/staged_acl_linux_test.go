// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build linux

package hostfiles

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"golang.org/x/sys/unix"
)

const aclTestWorker = 65534

func aclFixture(t *testing.T) (string, int, hostingidentity.Spec, uint64) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	fd := aclDirectory(t, root, 0o755)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		t.Fatal(err)
	}
	return root, fd, hostingidentity.Spec{UID: st.Uid, GID: st.Gid}, uint64(st.Dev)
}

func aclDirectory(t *testing.T, path string, mode os.FileMode) int {
	t.Helper()
	if err := os.MkdirAll(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func aclFile(t *testing.T, path string) int {
	t.Helper()
	if err := os.WriteFile(path, []byte("fixture"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func workerACL(permission uint16) []byte {
	var buffer bytes.Buffer
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(2))
	for _, entry := range []struct {
		tag, perm uint16
		id        uint32
	}{
		{1, 7, ^uint32(0)}, {2, permission, aclTestWorker},
		{4, 5, ^uint32(0)}, {16, 5, ^uint32(0)}, {32, 0, ^uint32(0)},
	} {
		_ = binary.Write(&buffer, binary.LittleEndian, entry.tag)
		_ = binary.Write(&buffer, binary.LittleEndian, entry.perm)
		_ = binary.Write(&buffer, binary.LittleEndian, entry.id)
	}
	return buffer.Bytes()
}

func setWorkerDirectoryACL(t *testing.T, fd int, permission uint16, defaults bool) {
	t.Helper()
	if err := setDescriptorACL(fd, accessACLAttribute, workerACL(permission)); err != nil {
		t.Fatal(err)
	}
	if defaults {
		if err := setDescriptorACL(fd, defaultACLAttribute, workerACL(permission)); err != nil {
			t.Fatal(err)
		}
	}
}

func assertWorkerACL(t *testing.T, fd int, want bool) {
	t.Helper()
	data, err := readDescriptorACL(fd, accessACLAttribute)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	var permission, mask uint16
	for offset := 4; offset+8 <= len(data); offset += 8 {
		tag := binary.LittleEndian.Uint16(data[offset:])
		if tag == 16 {
			mask = binary.LittleEndian.Uint16(data[offset+2:])
		}
		if tag == 2 && binary.LittleEndian.Uint32(data[offset+4:]) == aclTestWorker {
			found = true
			permission = binary.LittleEndian.Uint16(data[offset+2:])
		}
	}
	if got := found && permission&mask&4 != 0; got != want {
		t.Fatalf("worker read ACL=%v, want %v", got, want)
	}
}

func assertWorkerRead(t *testing.T, root int, relative string, want bool) {
	t.Helper()
	if os.Geteuid() != 0 {
		return
	} // The privileged CI pass additionally tests the actual kernel decision.
	fd, err := unix.Dup(root)
	if err != nil {
		t.Fatal(err)
	}
	handle := os.NewFile(uintptr(fd), "acl-fixture-root")
	defer handle.Close()
	command := exec.Command("/bin/cat", "/proc/self/fd/3/"+relative) // #nosec G204 -- fixed test command and generated fixture path.
	command.ExtraFiles = []*os.File{handle}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: aclTestWorker, Gid: aclTestWorker, Groups: []uint32{aclTestWorker}}}
	output, err := command.Output()
	if want && (err != nil || string(output) != "fixture") {
		t.Fatalf("worker cannot read published fixture: %v", err)
	}
	if !want && err == nil {
		t.Fatal("worker can read private fixture")
	}
}

func TestStagedUploadInheritsOnlyDestinationACL(t *testing.T) {
	root, rootFD, _, _ := aclFixture(t)
	public := aclDirectory(t, filepath.Join(root, "public"), 0o750)
	private := aclDirectory(t, filepath.Join(root, "private"), 0o750)
	staging := aclDirectory(t, filepath.Join(root, "staging"), 0o700)
	setWorkerDirectoryACL(t, public, 5, true)
	part := aclFile(t, filepath.Join(root, "staging", "part"))
	// Regression control: the old chmod+rename path creates the observed 404.
	if err := unix.Renameat2(staging, "part", public, "old", unix.RENAME_NOREPLACE); err != nil {
		t.Fatal(err)
	}
	assertWorkerACL(t, part, false)
	assertWorkerRead(t, rootFD, "public/old", false)
	if err := unix.Renameat2(public, "old", staging, "part", unix.RENAME_NOREPLACE); err != nil {
		t.Fatal(err)
	}
	if err := inheritStagedAccess(part, public, 0o640, false); err != nil {
		t.Fatal(err)
	}
	assertWorkerACL(t, part, true)
	assertWorkerRead(t, rootFD, "staging/part", false)
	if err := unix.Renameat2(staging, "part", public, "new", unix.RENAME_NOREPLACE); err != nil {
		t.Fatal(err)
	}
	assertWorkerRead(t, rootFD, "public/new", true)
	// A destination without a default ACL must not inherit a stale public grant.
	if err := inheritStagedAccess(part, private, 0o640, false); err != nil {
		t.Fatal(err)
	}
	assertWorkerACL(t, part, false)
	assertWorkerRead(t, rootFD, "public/new", false)
	var status unix.Stat_t
	if unix.Fstat(part, &status) != nil || status.Mode&0o7777 != 0o640 {
		t.Fatal("unsafe uploaded mode")
	}
}

func TestStagedCopyAndExtractionInheritWithoutOpeningStaging(t *testing.T) {
	root, rootFD, identity, device := aclFixture(t)
	public := aclDirectory(t, filepath.Join(root, "public"), 0o750)
	staging := aclDirectory(t, filepath.Join(root, "staging"), 0o700)
	source := aclDirectory(t, filepath.Join(root, "source"), 0o750)
	aclDirectory(t, filepath.Join(root, "source", "tree"), 0o750)
	aclFile(t, filepath.Join(root, "source", "tree", "index.txt"))
	setWorkerDirectoryACL(t, public, 5, true)
	if err := seedStagingDefaultACL(staging, public); err != nil {
		t.Fatal(err)
	}
	if err := copyManagedEntryAt(t.Context(), source, "tree", staging, "copy", identity, device, &fileOperationBudget{}, 0); err != nil {
		t.Fatal(err)
	}
	assertWorkerRead(t, rootFD, "staging/copy/index.txt", false)
	if err := unix.Renameat2(staging, "copy", public, "copy", unix.RENAME_NOREPLACE); err != nil {
		t.Fatal(err)
	}
	assertWorkerRead(t, rootFD, "public/copy/index.txt", true)
	extracted := aclDirectory(t, filepath.Join(root, "staging", "extract"), 0o700)
	if err := writeExtractedArchiveFile(t.Context(), extracted, []string{"nested", "index.txt"}, bytes.NewBufferString("fixture"), 7, identity, device); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fchmod(extracted, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := unix.Renameat2(staging, "extract", public, "extract", unix.RENAME_NOREPLACE); err != nil {
		t.Fatal(err)
	}
	assertWorkerRead(t, rootFD, "public/extract/nested/index.txt", true)
	var status unix.Stat_t
	if unix.Fstat(staging, &status) != nil || status.Mode&0o7777 != 0o700 {
		t.Fatal("staging was opened to worker")
	}
}

func TestStagedInheritanceBoundsPermissiveDefaults(t *testing.T) {
	root, _, _, _ := aclFixture(t)
	parent := aclDirectory(t, filepath.Join(root, "parent"), 0o750)
	defaults := workerACL(7)
	// A tenant may change its own default ACL. Publication still caps mask,
	// other permissions and special bits to the controlled file mode.
	binary.LittleEndian.PutUint16(defaults[4+3*8+2:], 7)
	binary.LittleEndian.PutUint16(defaults[4+4*8+2:], 7)
	if err := setDescriptorACL(parent, defaultACLAttribute, defaults); err != nil {
		t.Fatal(err)
	}
	file := aclFile(t, filepath.Join(root, "file"))
	if err := inheritStagedAccess(file, parent, 0o640, false); err != nil {
		t.Fatal(err)
	}
	var status unix.Stat_t
	if unix.Fstat(file, &status) != nil || status.Mode&0o7777 != 0o640 {
		t.Fatal("default ACL broadened controlled permissions")
	}
	assertWorkerACL(t, file, true)
}

func TestStagedRestoreKeepsNestedLiveWebAndPrivatePolicies(t *testing.T) {
	root, rootFD, identity, device := aclFixture(t)
	live := aclDirectory(t, filepath.Join(root, "live"), 0o750)
	setWorkerDirectoryACL(t, live, 1, false)
	domains := aclDirectory(t, filepath.Join(root, "live", "domains"), 0o750)
	setWorkerDirectoryACL(t, domains, 1, false)
	web := aclDirectory(t, filepath.Join(root, "live", "domains", "custom"), 0o750)
	setWorkerDirectoryACL(t, web, 5, true)
	aclDirectory(t, filepath.Join(root, "live", "private"), 0o700)
	staged := aclDirectory(t, filepath.Join(root, "staged"), 0o700)
	aclDirectory(t, filepath.Join(root, "staged", "domains", "custom", "new"), 0o750)
	aclDirectory(t, filepath.Join(root, "staged", "private"), 0o750)
	aclDirectory(t, filepath.Join(root, "staged", "new-private"), 0o750)
	publicFile := aclFile(t, filepath.Join(root, "staged", "domains", "custom", "new", "index.txt"))
	privateFile := aclFile(t, filepath.Join(root, "staged", "private", "secret.txt"))
	aclFile(t, filepath.Join(root, "staged", "new-private", "secret.txt"))
	if err := restoreStagedDirectoryAccess(t.Context(), staged, live, identity, device, &fileOperationBudget{}, 0); err != nil {
		t.Fatal(err)
	}
	assertWorkerACL(t, publicFile, true)
	assertWorkerACL(t, privateFile, false)
	assertWorkerRead(t, rootFD, "staged/domains/custom/new/index.txt", true)
	assertWorkerRead(t, rootFD, "staged/private/secret.txt", false)
	assertWorkerRead(t, rootFD, "staged/new-private/secret.txt", false)
	if data, err := readDescriptorACL(staged, defaultACLAttribute); err != nil || len(data) != 0 {
		t.Fatal("account-wide default ACL introduced")
	}
	info, err := os.Stat(filepath.Join(root, "staged", "private"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("private directory mode changed")
	}
}

func TestStagedRestoreRejectsLinksAndBoundedFailures(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "cancelled", "entry-limit", "bad-descriptor"} {
		t.Run(kind, func(t *testing.T) {
			root, _, identity, device := aclFixture(t)
			live := aclDirectory(t, filepath.Join(root, "live"), 0o750)
			staged := aclDirectory(t, filepath.Join(root, "staged"), 0o700)
			outside := aclFile(t, filepath.Join(root, "outside"))
			ctx := t.Context()
			budget := &fileOperationBudget{}
			switch kind {
			case "symlink":
				if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "staged", "link")); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(filepath.Join(root, "outside"), filepath.Join(root, "staged", "link")); err != nil {
					t.Fatal(err)
				}
				if err := inheritStagedAccess(outside, live, 0o640, false); !errors.Is(err, ErrConflict) {
					t.Fatalf("hardlinked upload accepted: %v", err)
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "entry-limit":
				aclFile(t, filepath.Join(root, "staged", "file"))
				budget.entryLimit = 1
			case "bad-descriptor":
				live = -2
			}
			var err error
			if kind == "bad-descriptor" {
				_, err = readDescriptorACL(live, defaultACLAttribute)
			} else {
				err = restoreStagedDirectoryAccess(ctx, staged, live, identity, device, budget, 0)
			}
			if err == nil {
				t.Fatal("unsafe or incomplete permission pass accepted")
			}
			if kind == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			assertWorkerACL(t, outside, false)
		})
	}
}
