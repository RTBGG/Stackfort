// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDisposableNativePackageGuard(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_PACKAGE_GUARD_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativePackageGuard$")
		command.Env = append(os.Environ(), "STACKFORT_PACKAGE_GUARD_CHILD=1")
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	nativePackageTestNamespace(t)
	for _, scenario := range []string{"held", "busy-front", "busy-db", "nested", "close-unrelated", "cancelled", "nil-context", "missing", "symlink", "hardlink", "fifo", "directory", "writable", "foreign-owner", "replacement", "handoff-replacement", "handoff-busy", "guard-owner-exit", "real-clients", "apt-handoff"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := t.TempDir()
			for _, name := range []string{"lock-frontend", "lock", "status"} {
				if err := os.WriteFile(filepath.Join(fixture, name), nil, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(filepath.Join(fixture, "updates"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mount(fixture, "/var/lib/dpkg", "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := unix.Unmount("/var/lib/dpkg", 0); err != nil {
					t.Error(err)
				}
			}()
			path := "/var/lib/dpkg/lock"
			if scenario == "guard-owner-exit" {
				stop := nativePackageTestWriter(t, "guard")
				defer stop()
				nativePackageTestPOSIX(t, path, true)
				stop()
				nativePackageTestPOSIX(t, path, false)
				return
			}
			switch scenario {
			case "busy-front", "busy-db":
				busy := path
				if scenario == "busy-front" {
					busy += "-frontend"
				}
				stopWriter := nativePackageTestWriter(t, busy)
				defer stopWriter()
			case "missing", "symlink", "fifo", "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "symlink":
					if err := os.Symlink("lock-frontend", path); err != nil {
						t.Fatal(err)
					}
				case "fifo":
					if err := unix.Mkfifo(path, 0644); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatal(err)
					}
				}
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(path, 0666); err != nil {
					t.Fatal(err)
				}
			case "foreign-owner":
				if err := os.Chown(path, 12345, 12345); err != nil {
					t.Fatal(err)
				}
			}
			ctx := t.Context()
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if scenario == "nil-context" {
				ctx = nil
			}
			guard, err := acquireNativePackageGuard(ctx)
			valid := scenario == "held" || scenario == "nested" || scenario == "close-unrelated" || scenario == "replacement" || scenario == "handoff-replacement" || scenario == "handoff-busy" || scenario == "real-clients" || scenario == "apt-handoff"
			if (err == nil) != valid {
				t.Fatalf("unexpected acquire: %v", err)
			}
			if !valid {
				if scenario == "busy-front" || scenario == "busy-db" {
					_, manifest, intent, _ := testRecoveryBoot(t)
					plan, err := manifest.Plan()
					if err != nil {
						t.Fatal(err)
					}
					backend := nativeBootBackend{nativeReadyBackend: nativeReadyBackend{plan: plan, intent: intent}}
					// No source backend or writable boot fixture is supplied: a busy
					// package manager must reject before any boot artifact access.
					if err := backend.Arm(t.Context(), plan); err == nil || !strings.Contains(err.Error(), "package manager busy") {
						t.Fatalf("arm did not stop at package guard: %v", err)
					}
				}
				if scenario != "busy-front" {
					nativePackageTestPOSIX(t, "/var/lib/dpkg/lock-frontend", false)
				}
				if scenario == "missing" {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("missing lock was created")
					}
				}
				return
			}
			defer guard.close()
			if err := guard.check(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := guard.acquire(t.Context()); err == nil {
				t.Fatal("double acquire accepted")
			}
			for _, file := range guard.files {
				flags, err := unix.FcntlInt(uintptr(file.fd), unix.F_GETFD, 0)
				if err != nil || flags&unix.FD_CLOEXEC == 0 {
					t.Fatal("guard descriptor could leak into exec", err)
				}
			}
			if scenario == "nested" {
				other, err := acquireNativePackageGuard(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				other.close()
			}
			if scenario == "close-unrelated" {
				file, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				file.Close()
			}
			for _, name := range []string{"lock-frontend", "lock"} {
				nativePackageTestPOSIX(t, "/var/lib/dpkg/"+name, true)
			}
			if scenario == "handoff-busy" {
				guard.close()
				stop := nativePackageTestWriter(t, path)
				defer stop()
				if err := guard.acquire(t.Context()); err == nil {
					t.Fatal("competing writer admitted on handoff")
				}
				nativePackageTestPOSIX(t, "/var/lib/dpkg/lock-frontend", false)
				stop()
				if err := guard.acquire(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "replacement" || scenario == "handoff-replacement" {
				if scenario == "handoff-replacement" {
					guard.close()
				}
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0644); err != nil {
					t.Fatal(err)
				}
				if scenario == "replacement" && guard.check(t.Context()) == nil {
					t.Fatal("replacement accepted")
				}
				if scenario == "handoff-replacement" && guard.acquire(t.Context()) == nil {
					t.Fatal("replacement adopted on handoff")
				}
				return
			}
			if scenario == "real-clients" {
				for _, binary := range []string{"/usr/bin/apt-get", "/usr/bin/dpkg"} {
					args := []string{"--configure", "--pending"}
					if strings.HasSuffix(binary, "apt-get") {
						args = []string{"-o", "DPkg::Lock::Timeout=0", "--no-download", "--assume-no", "install", "stackfort-package-guard-nonexistent"}
					}
					command := exec.CommandContext(t.Context(), binary, args...)
					command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
					output, err := command.CombinedOutput()
					if err == nil || !strings.Contains(strings.ToLower(string(output)), "lock") {
						t.Fatalf("real client was not excluded: %s: %s: %v", binary, output, err)
					}
					t.Logf("%s rejected while guard held: %s", binary, output)
				}
				// Read-only dpkg audit must remain available while locks are held.
				if output, err := exec.CommandContext(t.Context(), "/usr/bin/dpkg", "--audit").CombinedOutput(); err != nil {
					t.Fatalf("audit: %s: %v", output, err)
				}
			}
			if scenario == "apt-handoff" {
				// An intentionally unavailable package cannot change the fixture.
				// This must fail for package resolution, not for our own locks.
				_, err := guard.apt(t.Context(), "-o", "DPkg::Lock::Timeout=0", "--no-download", "--assume-no", "install", "stackfort-package-guard-nonexistent")
				if err == nil || !strings.Contains(err.Error(), "Unable to locate package") {
					t.Fatalf("APT handoff: %v", err)
				}
				if err := guard.check(t.Context()); err != nil {
					t.Fatal(err)
				}
				nativePackageTestPOSIX(t, path, true)
			}
			guard.close()
			guard.close()
			if guard.check(t.Context()) == nil {
				t.Fatal("released guard accepted")
			}
			if _, err := guard.apt(t.Context(), "--version"); err == nil {
				t.Fatal("APT ran without a held guard")
			}
			for _, name := range []string{"lock-frontend", "lock"} {
				nativePackageTestPOSIX(t, "/var/lib/dpkg/"+name, false)
			}
			if err := guard.acquire(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDisposableNativePackageBaseline(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	guard, err := acquireNativePackageGuard(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.close()
	packages, err := nativeHostPackages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"matching", "changed-inventory", "changed-receipt", "wrong-operation", "missing", "incomplete", "unsafe-mode", "no-stage"} {
		t.Run(scenario, func(t *testing.T) {
			_, manifest, intent, record := testRecoveryBoot(t)
			plan, err := manifest.Plan()
			if err != nil {
				t.Fatal(err)
			}
			record.After.PackagesSHA256 = admissionDigest(nativeBootJSON(packages))
			if scenario == "changed-inventory" {
				record.After.PackagesSHA256 = strings.Repeat("e", 64)
			}
			if scenario == "wrong-operation" {
				record.OperationID = "30000000-0000-4000-8000-000000000077"
			}
			if scenario == "incomplete" {
				record.After = nil
				record.Phase = "checking"
			}
			intent.Offline.PrerequisitesSHA256 = admissionDigest(nativeBootJSON(record))
			if scenario == "changed-receipt" {
				intent.Offline.PrerequisitesSHA256 = strings.Repeat("e", 64)
			}
			fixture := t.TempDir()
			fd, err := unix.Open(fixture, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fd)
			if scenario != "missing" {
				mode := os.FileMode(0600)
				if scenario == "unsafe-mode" {
					mode = 0644
				}
				if err := os.WriteFile(filepath.Join(fixture, nativePrerequisiteName), nativeBootJSON(record), mode); err != nil {
					t.Fatal(err)
				}
			}
			backend := nativeBootBackend{nativeReadyBackend: nativeReadyBackend{plan: plan, intent: intent}, stage: &SourceStage{dir: fd}}
			if scenario == "no-stage" {
				backend.stage = nil
			}
			err = backend.checkPackageBaseline(t.Context())
			if (err == nil) != (scenario == "matching") {
				t.Fatalf("baseline decision: %v", err)
			}
			if scenario == "changed-inventory" && !strings.Contains(err.Error(), "packages changed after native preparation") {
				t.Fatalf("wrong drift rejection: %v", err)
			}
		})
	}
}

func nativePackageTestNamespace(t *testing.T) {
	t.Helper()
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == parent {
		t.Fatal("private mount namespace required")
	}
}

// Real POSIX locks conflict with OFD read guards even from this process.
func nativePackageTestPOSIX(t *testing.T, path string, busy bool) {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
	err = unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock)
	if busy {
		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
			t.Fatalf("writer admitted: %v", err)
		}
	} else if err != nil {
		t.Fatalf("lock leaked: %v", err)
	}
}

func nativePackageTestWriter(t *testing.T, path string) func() {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, self, "-test.run=^TestNativePackageLockWriter$")
	command.Env = append(os.Environ(), "STACKFORT_PACKAGE_WRITER_PATH="+path)
	in, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	stop := func() {
		if done {
			return
		}
		done = true
		in.Close()
		if err := command.Wait(); err != nil {
			t.Error(err)
		}
		cancel()
	}
	t.Cleanup(stop)
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("writer handshake: %q: %v", line, err)
	}
	return stop
}

func TestNativePackageLockWriter(t *testing.T) {
	path := os.Getenv("STACKFORT_PACKAGE_WRITER_PATH")
	if path == "" {
		return
	}
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" || os.Getenv("STACKFORT_PACKAGE_GUARD_CHILD") != "1" || (path != "/var/lib/dpkg/lock" && path != "/var/lib/dpkg/lock-frontend" && path != "guard") {
		t.Fatal("invalid writer fixture")
	}
	nativePackageTestNamespace(t)
	if path == "guard" {
		guard, err := acquireNativePackageGuard(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		// Deliberately leave descriptors open. The child exits after stdin EOF;
		// the parent then verifies kernel release, not just our close method.
		if err := guard.check(t.Context()); err != nil {
			t.Fatal(err)
		}
		fmt.Println("locked")
		_, _ = os.Stdin.Read(make([]byte, 1))
		return
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
	if err := unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	fmt.Println("locked")
	_, _ = os.Stdin.Read(make([]byte, 1))
}
