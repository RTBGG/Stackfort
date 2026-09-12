// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostociimage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// This separately opted-in producer test uses the installed Podman executable,
// not a fixture archive or mocked runner. It qualifies OCI layout, actual .Id
// output, config identity and layer diffIDs. It does NOT qualify rootless
// Buildah, the production snapshot permissions, Trivy, or deployment admission.
//
// No RUN, network, container startup, default store, host hooks or host services
// are involved. Every invocation uses the same fresh private vfs store and
// private mount/network namespaces; no podman system reset/prune is used.
func TestDisposableRealPodmanOCIArchiveProducer(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" ||
		os.Getenv("STACKFORT_REAL_PODMAN_EXPORT_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("requires both explicit disposable-host and real-Podman opt-ins as root")
	}
	info, err := os.Lstat("/usr/bin/podman")
	if err != nil || !producerRootOwned(info) || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		t.Fatal("producer prerequisite: trusted installed /usr/bin/podman unavailable")
	}
	parentInfo, err := os.Lstat("/var/tmp")
	if err != nil || !producerRootOwned(parentInfo) || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 ||
		parentInfo.Mode().Perm()&0o022 != 0 && parentInfo.Mode()&os.ModeSticky == 0 {
		t.Fatal("producer prerequisite: unsafe temporary parent")
	}
	root, err := os.MkdirTemp("/var/tmp", "stackfort-podman-producer-")
	if err != nil {
		t.Fatal("producer prerequisite: private temporary directory unavailable")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !producerRootOwned(rootInfo) || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		t.Fatal("producer prerequisite: private temporary directory metadata invalid")
	}
	t.Cleanup(func() {
		// Validate the exact original inode and fixed direct-parent scope before
		// recursive cleanup. RemoveAll does not follow symlinks in the store.
		current, err := os.Lstat(root)
		if err != nil || !os.SameFile(rootInfo, current) || !producerRootOwned(current) ||
			!current.IsDir() || current.Mode().Perm() != 0o700 || filepath.Dir(root) != "/var/tmp" ||
			!strings.HasPrefix(filepath.Base(root), "stackfort-podman-producer-") {
			t.Error("producer cleanup: temporary directory identity mismatch; preserved")
			return
		}
		if os.RemoveAll(root) != nil {
			t.Error("producer cleanup: isolated temporary directory removal failed")
		}
	})
	for _, name := range []string{"store", "runroot", "tmp", "home", "config", "runtime", "hooks", "networks", "context"} {
		if os.Mkdir(filepath.Join(root, name), 0o700) != nil {
			t.Fatal("producer prerequisite: isolated directory creation failed")
		}
	}
	// CONTAINERS_CONF replaces the system/user configuration. File locks avoid
	// the default shared-memory lock namespace. A separate storage.conf also
	// prevents an inherited imagestore or storage option from escaping the test.
	configuration := "[engine]\nlock_type=\"file\"\nevents_logger=\"none\"\ncgroup_manager=\"cgroupfs\"\n"
	storageConfiguration := fmt.Sprintf("[storage]\ndriver=\"vfs\"\nrunroot=%q\ngraphroot=%q\n", filepath.Join(root, "runroot"), filepath.Join(root, "store"))
	for name, content := range map[string]string{
		"containers.conf":     configuration,
		"storage.conf":        storageConfiguration,
		"Containerfile":       "FROM scratch\nCOPY fixture.txt /fixture.txt\n",
		"context/fixture.txt": "stackfort real OCI producer fixture\n",
	} {
		if os.WriteFile(filepath.Join(root, name), []byte(content), 0o600) != nil {
			t.Fatal("producer prerequisite: isolated fixture creation failed")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	const tag = "localhost/stackfort-producer-fixture:disposable"
	run := func(stage string, arguments ...string) string {
		t.Helper()
		output, err := runIsolatedPodmanProducer(ctx, root, arguments...)
		if err != nil {
			// Neither raw subprocess output nor exception strings reach test logs.
			t.Fatalf("producer stage %s failed (output redacted)", stage)
		}
		return output
	}
	run("build", "build", "--pull=never", "--no-cache", "--layers=false", "--force-rm=true",
		"--network=none", "--format=oci", "--tag", tag, "--file", filepath.Join(root, "Containerfile"), filepath.Join(root, "context"))
	inspected := run("inspect", "image", "inspect", "--format", "{{.Id}}", tag)
	imageDigest, err := parseInspectedImageID(inspected)
	if err != nil || strings.HasPrefix(inspected, "sha256:") {
		t.Fatal("producer inspect did not return the expected canonical bare image ID")
	}
	archive := filepath.Join(root, "image.tar")
	file, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("producer archive creation failed")
	}
	if file.Close() != nil {
		t.Fatal("producer archive close failed")
	}
	run("save", "image", "save", "--format", "oci-archive", "--output", archive, tag)
	before, err := os.Lstat(archive)
	if err != nil || !ownedArchive(before, 0, 0) {
		t.Fatal("producer archive metadata invalid")
	}
	if sealImageArchive(ctx, archive, 0, 0, 0, 0, imageDigest) != nil {
		t.Fatal("real Podman archive rejected by immutable archive validator")
	}
	after, err := os.Lstat(archive)
	if err != nil || !ownedArchive(after, 0, 0) || os.SameFile(before, after) {
		t.Fatal("producer archive was not published as a fresh privileged inode")
	}
	sealed, err := os.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal("producer sealed archive open failed")
	}
	verifyErr := verifyImageArchive(ctx, sealed, imageDigest)
	closeErr := sealed.Close()
	if verifyErr != nil || closeErr != nil {
		t.Fatal("producer published archive failed second verification")
	}
	run("remove-isolated-image", "image", "rm", "--ignore", "--no-prune", tag)
	t.Log("real isolated rootful Podman build/inspect/save/seal/reverify passed; rootless builder and Trivy are not covered")
}

func producerRootOwned(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	return ok && status.Uid == 0 && status.Gid == 0
}

// runIsolatedPodmanProducer never inherits remote-connection settings, host
// storage configuration, credentials, proxy variables, or default runtime paths.
func runIsolatedPodmanProducer(ctx context.Context, root string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	options := []string{
		"--remote=false", "--root=" + filepath.Join(root, "store"), "--runroot=" + filepath.Join(root, "runroot"),
		"--storage-driver=vfs", "--tmpdir=" + filepath.Join(root, "tmp"), "--events-backend=none", "--cgroup-manager=cgroupfs",
		"--hooks-dir=" + filepath.Join(root, "hooks"), "--network-config-dir=" + filepath.Join(root, "networks"),
	}
	command := exec.CommandContext(ctx, "/usr/bin/podman", append(options, arguments...)...)
	command.Dir = root
	command.Env = []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C",
		"HOME=" + filepath.Join(root, "home"), "TMPDIR=" + filepath.Join(root, "tmp"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_RUNTIME_DIR=" + filepath.Join(root, "runtime"),
		"CONTAINERS_CONF=" + filepath.Join(root, "containers.conf"), "CONTAINERS_STORAGE_CONF=" + filepath.Join(root, "storage.conf"),
	}
	// Go's Linux Unshareflags implementation makes the inherited mount tree
	// recursively private. Even a failed build cannot propagate mounts to the
	// host, and the independent network namespace has no external interfaces.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL,
		Unshareflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWNET}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	command.WaitDelay = 3 * time.Second
	stdout := &producerBoundedOutput{cancel: cancel}
	stderr := &producerBoundedOutput{cancel: cancel}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil || ctx.Err() != nil || stdout.overflow || stderr.overflow {
		return "", errors.New("isolated producer command failed")
	}
	return stdout.output.String(), nil
}

type producerBoundedOutput struct {
	mutex    sync.Mutex
	output   bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (output *producerBoundedOutput) Write(content []byte) (int, error) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	const limit = 64 << 10
	if len(content) > limit-output.output.Len() {
		output.overflow = true
		output.cancel()
		return len(content), nil
	}
	return output.output.Write(content)
}
