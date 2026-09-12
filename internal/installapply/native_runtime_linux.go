// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// SealNativeRuntime is an internal preparation API, not a CLI activation path.
// It can never adopt an existing journal, ready host or foreign runtime. The
// dispatcher is a separately trusted qualification artifact until a signed
// native candidate includes this code. A partial stage is preserved on error.
func (stage *SourceStage) SealNativeRuntime(ctx context.Context, manifest NativeReleaseManifest, intent NativeRuntimeIntent, dispatcher string) error {
	if err := stage.check(); err != nil {
		return err
	}
	if ctx == nil || ctx.Err() != nil || stage.journal == nil {
		return errors.New("runtime sealing requires an active locked source stage")
	}
	if _, exists, err := stage.journal.Load(); err != nil || exists {
		return errors.Join(err, errors.New("runtime must be sealed before storage preparation"))
	}
	if _, err := manifest.Plan(); err != nil {
		return err
	}
	digest, err := intent.Digest()
	if err != nil || digest != manifest.BootSHA256 {
		return errors.Join(err, errors.New("runtime is not bound to the release boot intent"))
	}
	if _, err := stage.VerifyBinding(ctx, manifest.Release); err != nil {
		return err
	}
	if err := verifyNativePrerequisiteBinding(ctx, manifest, intent); err != nil {
		return err
	}
	if err := stage.verifyNativeSetupBinding(manifest, intent); err != nil {
		return err
	}
	parent, err := openSourceDirectory(filepath.Dir(dispatcher), false)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	file, err := openResumeFile(parent, filepath.Base(dispatcher), 64<<20)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if err != nil || len(data) > 64<<20 || admissionDigest(data) != intent.InstallerSHA256 {
		return errors.New("runtime dispatcher pin mismatch")
	}
	name := filepath.Base(NativeRuntimePath)
	var stat unix.Stat_t
	if err := unix.Fstatat(stage.dir, name, &stat, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		if err := writeNativeDispatcher(stage.dir, data); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := stage.verifyNativeDispatcher(ctx, intent, false); err != nil {
		return err
	}
	canonical, _ := json.MarshalIndent(intent, "", "  ")
	canonical = append(canonical, '\n')
	if _, found, err := admissionReadAt(stage.dir, nativeRuntimeName); err != nil {
		return err
	} else if !found {
		return writeOriginRecord(stage.dir, nativeRuntimeName, canonical)
	}
	return equalOriginRecord(stage.dir, nativeRuntimeName, canonical)
}

// Binary-specific bound; do not weaken the 64 KiB JSON-record writer. An
// interrupted executable is retained non-executable and cannot be overwritten.
func writeNativeDispatcher(dir int, data []byte) error {
	return writeNativeExecutable(dir, filepath.Base(NativeRuntimePath), data)
}

func writeNativeExecutable(dir int, name string, data []byte) error {
	if name != filepath.Base(NativeRuntimePath) && name != filepath.Base(NativePrerequisiteInstaller) {
		return errors.New("invalid native executable name")
	}
	if len(data) == 0 || len(data) > 64<<20 {
		return errors.New("native dispatcher exceeds bound")
	}
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "native-runtime-installer")
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Chmod(0500); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return unix.Fsync(dir)
}

func (stage *SourceStage) verifyNativeDispatcher(ctx context.Context, intent NativeRuntimeIntent, self bool) error {
	file, err := openResumeFile(stage.dir, filepath.Base(NativeRuntimePath), 64<<20)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Mode().Perm() != 0500 {
		return errors.New("unsafe native dispatcher mode")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if err != nil || len(data) > 64<<20 || admissionDigest(data) != intent.InstallerSHA256 {
		return errors.New("native dispatcher changed")
	}
	if self {
		path, err := os.Executable()
		if err != nil || path != NativeRuntimePath {
			return errors.New("native service must run from its sealed dispatcher")
		}
		// Hash the running inode as well; a pathname replacement cannot pass by
		// merely restoring the expected on-disk executable before inspection.
		running, err := os.Open("/proc/self/exe")
		if err != nil {
			return err
		}
		defer running.Close()
		live, err := io.ReadAll(io.LimitReader(running, (64<<20)+1))
		if err != nil || len(live) > 64<<20 || admissionDigest(live) != intent.InstallerSHA256 {
			return errors.New("running native dispatcher differs from pin")
		}
	}
	return ctx.Err()
}

func (stage *SourceStage) loadNativeRuntime(ctx context.Context, operation string) (NativeReleaseManifest, nativeReadyBackend, error) {
	return stage.loadNativeRuntimeMode(ctx, operation, false)
}

func (stage *SourceStage) loadNativeRuntimeMode(ctx context.Context, operation string, boot bool) (NativeReleaseManifest, nativeReadyBackend, error) {
	var manifest NativeReleaseManifest
	var backend nativeReadyBackend
	data, exists, err := admissionReadAt(stage.dir, nativeReleaseManifestName)
	if err != nil || !exists {
		return manifest, backend, errors.Join(err, errors.New("missing sealed release manifest"))
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, backend, err
	}
	canonical, _ := json.MarshalIndent(manifest, "", "  ")
	plan, err := manifest.Plan()
	if err != nil || plan.OperationID != operation || !bytes.Equal(data, append(canonical, '\n')) {
		return manifest, backend, errors.New("invalid runtime release manifest")
	}
	state, exists, err := stage.journal.Load()
	if err != nil {
		return manifest, backend, err
	}
	if !exists || state.Plan != plan {
		return manifest, backend, errors.New("native journal binding mismatch")
	}
	if err := requireNativeRuntimeReady(state, exists, plan); err != nil && !boot {
		return manifest, backend, err
	}
	data, exists, err = admissionReadAt(stage.dir, nativeRuntimeName)
	if err != nil || !exists {
		return manifest, backend, errors.Join(err, errors.New("missing sealed runtime intent"))
	}
	var intent NativeRuntimeIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return manifest, backend, err
	}
	canonical, _ = json.MarshalIndent(intent, "", "  ")
	digest, err := intent.Digest()
	if err != nil || digest != manifest.BootSHA256 || !bytes.Equal(data, append(canonical, '\n')) {
		return manifest, backend, errors.New("runtime intent differs from sealed boot manifest")
	}
	if boot && intent.Profile != NativeBootProfile {
		return manifest, backend, errors.New("runtime has no offline preparation capability")
	}
	if intent.Offline != nil {
		after, err := nativeBootFstab(intent.Offline.FstabBefore, plan.RootUUID, plan.PartitionUUID)
		if err != nil || after != intent.Offline.FstabAfter {
			return manifest, backend, errors.New("invalid sealed fstab transition")
		}
	}
	if err := stage.verifyNativeDispatcher(ctx, intent, true); err != nil {
		return manifest, backend, err
	}
	if err := verifyNativePrerequisiteBinding(ctx, manifest, intent); err != nil {
		return manifest, backend, err
	}
	if err := stage.verifyNativeSetupBinding(manifest, intent); err != nil {
		return manifest, backend, err
	}
	if err := verifyNativeProfileUnits(ctx, operation, intent.Profile); err != nil {
		return manifest, backend, err
	}
	return manifest, nativeReadyBackend{plan: plan, spec: intent.Ready, intent: intent, sourceStage: stage}, nil
}

func verifyNativeRuntimeUnits(ctx context.Context, operation string) error {
	return verifyNativeProfileUnits(ctx, operation, "debian-native-post-ready-qualification-v1")
}

func verifyNativeProfileUnits(ctx context.Context, operation, profile string) error {
	units, err := NativeRuntimeUnits(operation)
	if profile == NativeBootProfile {
		units, err = NativeBootUnits(operation)
	}
	if err != nil {
		return err
	}
	for name, expected := range units {
		path := "/etc/systemd/system/" + name
		if err := verifyNativeUnitFile(ctx, path, expected); err != nil {
			return err
		}
		if err := verifyFile(path, []byte(expected), 0, 0, 0644); err != nil {
			return err
		}
		// Ask the manager as well, detecting loaded overrides from any systemd
		// search path rather than inspecting just /etc/*.d.
		fragment, err := runAdmissionCommand(ctx, "", "/usr/bin/systemctl", "show", "--property=FragmentPath", "--value", name)
		if err != nil || strings.TrimSpace(fragment) != path {
			return errors.New("native unit fragment drift: " + name)
		}
		dropins, err := runAdmissionCommand(ctx, "", "/usr/bin/systemctl", "show", "--property=DropInPaths", "--value", name)
		if err != nil || strings.TrimSpace(dropins) != "" {
			return errors.New("native unit override: " + name)
		}
		reload, err := runAdmissionCommand(ctx, "", "/usr/bin/systemctl", "show", "--property=NeedDaemonReload", "--value", name)
		if err != nil || strings.TrimSpace(reload) != "no" {
			return errors.New("native unit needs manager reload: " + name)
		}
	}
	link, err := os.Readlink("/etc/systemd/system/nginx.service.requires/srv-hosting.mount")
	if err != nil || link != "/etc/systemd/system/srv-hosting.mount" {
		return errors.New("NGINX hosting dependency drift")
	}
	for _, name := range []string{"mariadb.service", "vinyl.service", "stackfort-agent.service", "stackfort-api.service", "stackfort-phpmyadmin.service", "stackfort-panel-renew.service"} {
		if err := verifyNativeUnitFile(ctx, "/etc/systemd/system/"+name+".d/90-stackfort-native-storage.conf", NativeRuntimeConsumerDependency); err != nil {
			return err
		}
	}
	return nil
}

func verifyNativeUnitFile(ctx context.Context, path, expected string) error {
	actual, err := nativeTrustedHash(ctx, path)
	if err != nil || actual != admissionDigest([]byte(expected)) {
		return errors.Join(err, errors.New("native dependency or unit drift: "+path))
	}
	return nil
}

// RunNativeService is the actual installer service entry point. close and
// quarantine do not need the shared lock or intact records, so process-loss
// cleanup still works when an interrupted installer holds or corrupts state.
// Only admit can open ports, after every existing authenticated admission gate.
func RunNativeService(ctx context.Context, request NativeServiceRequest, output io.Writer) (err error) {
	if err := request.Validate(); err != nil {
		return err
	}
	if ctx == nil || ctx.Err() != nil || output == nil {
		return errors.New("native service requires an active context and output")
	}
	gate, err := NewLinuxAdmissionGate(request.OperationID)
	if err != nil {
		return err
	}
	quarantine := func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return errors.Join(gate.Close(cleanup), gate.VerifyClosed(cleanup), gate.StopConsumers(cleanup))
	}
	if request.Action == "quarantine" {
		return quarantine()
	}
	if request.Action == "recheck-cleanup" {
		cgroup, readErr := os.ReadFile("/proc/self/cgroup")
		if readErr == nil && nativeCompletedCleanupSucceeded(request.OperationID, string(cgroup), os.Getenv("SERVICE_RESULT"), os.Getenv("EXIT_CODE"), os.Getenv("EXIT_STATUS")) {
			return nil
		}
		return quarantine()
	}
	if request.Action == "close" {
		return gate.Close(ctx)
	}
	if request.Action == "admit" || request.Action == "recheck-completed" {
		defer func() {
			if err != nil {
				err = errors.Join(err, quarantine())
			}
		}()
	}
	// Never accept a previously open gate as permission for another invocation.
	if err := gate.Close(ctx); err != nil {
		return err
	}
	if err := gate.VerifyClosed(ctx); err != nil {
		return err
	}
	stage, exists, err := openExistingSourceStage()
	if err != nil || !exists {
		return errors.Join(err, errors.New("no sealed native runtime exists"))
	}
	defer func() { err = errors.Join(err, stage.Close()) }()
	manifest, backend, err := stage.loadNativeRuntime(ctx, request.OperationID)
	if err != nil {
		return err
	}
	if request.Action == "verify-storage" {
		decision, err := stage.AdvanceManifest(ctx, manifest, backend)
		if err != nil {
			return err
		}
		if !decision.Ready {
			return errors.New("native storage not ready")
		}
		_, err = fmt.Fprintln(output, "NATIVE_RUNTIME storage verified; no storage mutation or web admission")
		return err
	}
	backend.hostingMounted = true
	var result Result
	if request.Action == "recheck-completed" {
		// The public precheck released its lock before exec. Revalidate the
		// completed-only fence here, under the runtime lock; never consume an
		// approval that appeared during that handoff or continue pending work.
		completed, exists, checkErr := stage.completedNativeManifest(ctx)
		if checkErr != nil || !exists || completed != manifest {
			return errors.Join(checkErr, errors.New("completed native rerun state changed before live admission"))
		}
		result, err = stage.AdmitInstallation(ctx, manifest, backend, gate, nil, output)
	} else {
		result, err = stage.AdmitPendingInstallation(ctx, manifest, backend, gate, output)
	}
	if err != nil {
		return err
	}
	if request.Action == "recheck-completed" {
		if err := ValidateNativeCompletedResult(result, manifest); err != nil {
			return err
		}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	prefix := NativeRuntimeResultPrefix
	if request.Action == "recheck-completed" {
		prefix = NativeCompletedResultPrefix
	}
	_, err = fmt.Fprintln(output, prefix+string(data))
	return err
}
