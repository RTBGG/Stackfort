// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

// Reuse the retained beta.3 candidate as data, never as an executable or package.
// Neither the actual host /var/lib nor its boot/storage configuration is changed.
func TestDisposableResumeSourceStage(t *testing.T) {
	requireNativeLab(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("STACKFORT_RESUME_SOURCE_CHILD") != "1" {
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private",
			executable, "-test.v", "-test.timeout=3m", "-test.run=^TestDisposableResumeSourceStage$")
		command.Env = append(os.Environ(), "STACKFORT_RESUME_SOURCE_CHILD=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("source staging namespace: %v\n%s", err, output)
		}
		t.Logf("%s", output)
		return
	}
	self, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		t.Fatal(err)
	}
	init, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || self == init {
		t.Fatal("source staging requires a private mount namespace")
	}
	if encoded := os.Getenv("STACKFORT_RESUME_SOURCE_PIN"); encoded != "" {
		var pin installapply.SourcePin
		if err := json.Unmarshal([]byte(encoded), &pin); err != nil {
			t.Fatal(err)
		}
		stage, err := installapply.OpenSourceStage()
		if err != nil {
			t.Fatal(err)
		}
		defer stage.Close()
		source, err := stage.VerifyForPlan(t.Context(), resumeSourcePlan(pin), pin)
		if err != nil || source.Digest != pin.SourceDigest {
			t.Fatalf("fresh process verification: %v", err)
		}
		if _, err := installapply.NewLinuxRunner(io.Discard); !errors.Is(err, storageprep.ErrNotQualified) {
			t.Fatalf("source verification bypassed storage gate: %v", err)
		}
		t.Log("RESUME_SOURCE independent process reverified retained release; production storage gate still blocks")
		return
	}
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	if err := unix.Mount(fixture, "/var/lib", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer unix.Unmount("/var/lib", 0)
	bootstrap := t.TempDir()
	archive, err := os.Open("/tmp/resume-source-beta3.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, io.LimitReader(archive, (512<<20)+1))
	if err != nil || size != 117210432 || fmt.Sprintf("%x", digest.Sum(nil)) != "3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0" {
		t.Fatal("unexpected qualification archive; candidate must match retained beta.3 checksum")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), "/usr/bin/tar", "--extract", "--gzip", "--file=-", "--directory", bootstrap, "--no-same-owner", "--no-same-permissions")
	command.Stdin = archive
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("extract known candidate: %v %s", err, output)
	}
	root := filepath.Join(bootstrap, "stackfort-0.1.0-beta.3-linux-amd64")
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	if lock, err := installapply.NewFileStore().AcquireLock(); err == nil {
		_ = lock.Close()
		t.Fatal("stager did not hold installer lock")
	}
	if store, err := storageprep.OpenFileStore(); err == nil {
		_ = store.Close()
		t.Fatal("stager did not hold storage preparation lock")
	}
	pin, err := stage.Prepare(t.Context(), root, "20000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if pin.Version != "0.1.0-beta.3" {
		t.Fatal("wrong source version")
	}
	if _, err := stage.VerifyForPlan(t.Context(), resumeSourcePlan(pin), pin); err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := storageprep.OpenFileStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(storageprep.State{SchemaVersion: storageprep.SchemaVersion, Plan: resumeSourcePlan(pin), Phase: storageprep.Planned}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	stage, err = installapply.OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Prepare(t.Context(), root, pin.OperationID); err == nil {
		t.Fatal("source staged after native preparation began")
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	// This is the test's own temporary extraction, not the persistent host source.
	if err := os.RemoveAll(bootstrap); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	command = exec.CommandContext(t.Context(), executable, "-test.v", "-test.timeout=1m", "-test.run=^TestDisposableResumeSourceStage$")
	command.Env = append(os.Environ(), "STACKFORT_RESUME_SOURCE_PIN="+string(encoded))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("reopen: %v\n%s", err, output)
	} else {
		t.Logf("%s", output)
	}
	t.Logf("RESUME_SOURCE pin=%s archiveSHA256=%x", encoded, digest.Sum(nil))
}

func resumeSourcePlan(pin installapply.SourcePin) storageprep.Plan {
	return storageprep.Plan{OperationID: pin.OperationID, Version: pin.Version, SourceDigest: pin.SourceDigest, Distribution: "debian",
		MachineID: "20000000-0000-4000-8000-000000000002", RootUUID: "20000000-0000-4000-8000-000000000003",
		PartitionUUID: "20000000-0000-4000-8000-000000000004", PreviousBootID: "20000000-0000-4000-8000-000000000005",
		Kernel: "6.12.107+deb13-cloud-amd64", ManifestDigest: strings.Repeat("e", 64)}
}
