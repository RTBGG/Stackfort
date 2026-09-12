// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativeBootPrivateConfigScripts(t *testing.T) {
	for _, name := range []string{"initramfs.conf", "01-network", "resume_foo", ".disabled"} {
		if !nativeBootConfigName(name) {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"", ".", "..", "two words", "a\nb", "a/b", "a\\b", "a\x00b", "ä", strings.Repeat("x", 101)} {
		if nativeBootConfigName(name) {
			t.Fatal(name)
		}
	}
	initial := []nativeBootConfigEntry{{Path: "initramfs.conf", Mode: 0644, data: []byte("MODULES=most\n")}, {Path: "modules", Mode: 0600}}
	for i := range initial {
		initial[i].SHA256 = admissionDigest(initial[i].data)
	}
	before := nativeBootJSON(initial)
	operation := testSourcePin().OperationID
	for _, guarded := range []bool{false, true} {
		entries, err := nativeBootConfigScripts(initial, operation, guarded)
		if err != nil || !bytes.Equal(before, nativeBootJSON(initial)) {
			t.Fatal("renderer changed original local configuration", err)
		}
		for _, entry := range initial {
			index := slices.IndexFunc(entries, func(other nativeBootConfigEntry) bool { return other.Path == entry.Path })
			if index < 0 || !bytes.Equal(entries[index].data, entry.data) || entries[index].Mode != entry.Mode {
				t.Fatal("ordinary configuration was not preserved")
			}
		}
		var scripts string
		for _, entry := range entries {
			if strings.HasSuffix(entry.Path, "/stackfort-native-quota") {
				scripts += string(entry.data)
			}
		}
		if !strings.Contains(scripts, NativeRuntimePath) || !strings.Contains(scripts, "--operation-id="+operation) || strings.Contains(scripts, ".test") || strings.Contains(scripts, "STACKFORT_DISPOSABLE") {
			t.Fatal("private configuration is not bound to the real installer/operation")
		}
		if guarded && !strings.Contains(scripts, "if [ $result -ne 0 ]; then") {
			t.Fatal("guarded failure policy lost")
		}
		if _, err := nativeBootConfigScripts(entries, operation, guarded); err == nil {
			t.Fatal("existing scripts adopted")
		}
	}
	if _, err := nativeBootConfigScripts(nil, operation, true); err == nil {
		t.Fatal("missing default configuration accepted")
	}
	if _, err := nativeBootConfigScripts(initial, operation+"\necho", true); err == nil {
		t.Fatal("operation injection accepted")
	}
	conflict := append(slices.Clone(initial), nativeBootConfigEntry{Path: "hooks", Mode: 0644})
	if _, err := nativeBootConfigScripts(conflict, operation, true); err == nil {
		t.Fatal("file used as hooks directory")
	}
}

func TestDisposableNativeBootPrivateBuild(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires disposable root host")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_NATIVE_BUILD_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeBootPrivateBuild$")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_BUILD_CHILD=1")
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == parent {
		t.Fatal("mount isolation required")
	}
	for _, scenario := range []string{"valid", "source-drift", "private-drift", "receipt-drift", "existing-build", "symlink", "directory-symlink", "hardlink", "fifo", "writable-file", "writable-directory", "foreign-owner", "unsafe-name", "oversized-file", "excess-entries", "reserved-hook", "missing-config", "cancelled-context"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, state := t.TempDir(), t.TempDir()
			if err := os.MkdirAll(filepath.Join(fixture, "conf.d"), 0755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{"initramfs.conf": "MODULES=most\nCOMPRESS=zstd\n", "modules": "ext4\n", "conf.d/local": "RESUME=none\n"} {
				if err := os.WriteFile(filepath.Join(fixture, name), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(filepath.Join(fixture, "modules"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mount(state, "/var/lib", "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			defer unix.Unmount("/var/lib", 0)
			if err := unix.Mount(fixture, nativeBootConfigSource, "", unix.MS_BIND, ""); err != nil {
				t.Fatal(err)
			}
			defer unix.Unmount(nativeBootConfigSource, 0)
			stage, err := OpenSourceStage()
			if err != nil {
				t.Fatal(err)
			}
			defer stage.Close()
			bad := filepath.Join(fixture, "unsafe")
			switch scenario {
			case "symlink":
				err = os.Symlink("modules", bad)
			case "directory-symlink":
				err = os.Symlink("conf.d", bad)
			case "hardlink":
				err = os.Link(filepath.Join(fixture, "modules"), bad)
			case "fifo":
				err = unix.Mkfifo(bad, 0600)
			case "writable-file":
				err = os.Chmod(filepath.Join(fixture, "modules"), 0664)
			case "writable-directory":
				err = os.Chmod(filepath.Join(fixture, "conf.d"), 0775)
			case "foreign-owner":
				err = os.Chown(filepath.Join(fixture, "modules"), 65534, 65534)
			case "unsafe-name":
				err = os.WriteFile(filepath.Join(fixture, "two words"), nil, 0600)
			case "oversized-file":
				err = os.WriteFile(bad, bytes.Repeat([]byte{'x'}, nativeBootConfigMaximumFile+1), 0600)
			case "excess-entries":
				for i := 0; i < nativeBootConfigMaximumEntries; i++ {
					if err := os.WriteFile(filepath.Join(fixture, fmt.Sprintf("extra-%03d", i)), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "reserved-hook":
				if err := os.Mkdir(filepath.Join(fixture, "hooks"), 0755); err != nil {
					t.Fatal(err)
				}
				err = os.WriteFile(filepath.Join(fixture, "hooks", "stackfort-native-quota"), nil, 0755)
			case "missing-config":
				err = os.Remove(filepath.Join(fixture, "initramfs.conf"))
			case "existing-build":
				err = os.Mkdir(filepath.Join(DefaultJournalDirectory, nativeBootBuildName), 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if scenario == "cancelled-context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			beforeSource, beforeState := recoveryFixtureSnapshot(t, fixture), recoveryFixtureSnapshot(t, state)
			plan := pinPlan(testSourcePin())
			build, err := prepareNativeBootBuild(ctx, stage, plan, testBootIntent())
			valid := slices.Contains([]string{"valid", "source-drift", "private-drift", "receipt-drift"}, scenario)
			if (err == nil) != valid {
				t.Fatal(scenario, err)
			}
			if !bytes.Equal(beforeSource, recoveryFixtureSnapshot(t, fixture)) {
				t.Fatal("private build mutated ordinary system configuration")
			}
			if !valid {
				if !bytes.Equal(beforeState, recoveryFixtureSnapshot(t, state)) {
					t.Fatal("rejected input mutated build evidence")
				}
				return
			}
			if build.receipt.OperationID != plan.OperationID || build.receipt.Manifest != plan.ManifestDigest || build.receipt.Kernel != plan.Kernel || build.verify(t.Context()) != nil {
				t.Fatal("build receipt or copied configuration mismatch")
			}
			modules, err := os.Stat(filepath.Join(build.path, "config", "modules"))
			if err != nil || modules.Mode().Perm() != 0600 {
				t.Fatal("configuration permissions not preserved", err)
			}
			sealed := recoveryFixtureSnapshot(t, state)
			if _, err := prepareNativeBootBuild(t.Context(), stage, plan, testBootIntent()); err == nil || !bytes.Equal(sealed, recoveryFixtureSnapshot(t, state)) {
				t.Fatal("build replay changed evidence")
			}
			switch scenario {
			case "source-drift":
				err = os.WriteFile(filepath.Join(fixture, "modules"), []byte("drift\n"), 0600)
			case "private-drift":
				err = os.WriteFile(filepath.Join(build.path, "config", "modules"), []byte("drift\n"), 0600)
			case "receipt-drift":
				err = os.WriteFile(filepath.Join(build.path, "intent.json"), []byte("{}\n"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "valid" && build.verify(t.Context()) == nil {
				t.Fatal("source/private/receipt drift accepted")
			}
		})
	}
	t.Log("NATIVE_BOOT_PRIVATE_BUILD local config preserved; private operation-bound hooks only; unsafe input/replay/drift rejected")
}
