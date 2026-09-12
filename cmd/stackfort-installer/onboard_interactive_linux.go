// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/RTBGG/stackfort/internal/installapply"
	"golang.org/x/sys/unix"
)

func newOnboardInteractiveController() onboardInteractiveController {
	return onboardInteractiveController{tty: openOnboardTerminal, review: installapply.ReviewNativeOnboarding,
		setup: installapply.IssueNativeSetup, prepare: installapply.PrepareNativeOnboardingWithSetup,
		verify: verifyOnboardSealedRuntime, arm: armOnboardRuntime, reboot: rebootOnboardHost}
}

func openOnboardTerminal() (io.ReadWriteCloser, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("onboarding requires root")
	}
	deviceDirectory, err := unix.Open("/dev", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(deviceDirectory)
	var directoryStat unix.Stat_t
	if unix.Fstat(deviceDirectory, &directoryStat) != nil || directoryStat.Uid != 0 || directoryStat.Gid != 0 || directoryStat.Mode&0o7022 != 0 {
		return nil, errors.New("unsafe terminal device directory")
	}
	fd, err := unix.Openat(deviceDirectory, "tty", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	terminal := os.NewFile(uintptr(fd), "/dev/tty")
	if err := validateOnboardTerminal(terminal); err != nil {
		_ = terminal.Close()
		return nil, err
	}
	return terminal, nil
}

func validateOnboardTerminal(terminal *os.File) error {
	if terminal == nil || os.Geteuid() != 0 {
		return errors.New("missing root controlling terminal")
	}
	fd := int(terminal.Fd())
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR || stat.Uid != 0 ||
		unix.Major(uint64(stat.Rdev)) != 5 || unix.Minor(uint64(stat.Rdev)) != 0 {
		return errors.New("onboarding input is not the controlling-terminal device")
	}
	if _, err := unix.IoctlGetTermios(fd, unix.TCGETS); err != nil {
		return errors.New("onboarding input is not a terminal")
	}
	terminalSession, err := unix.IoctlGetInt(fd, unix.TIOCGSID)
	if err != nil {
		return errors.New("terminal session could not be verified")
	}
	processSession, err := unix.Getsid(0)
	if err != nil || terminalSession != processSession {
		return errors.New("terminal belongs to another session")
	}
	return nil
}

// Reopen the canonical sealed manifest under the shared source lock rather
// than treating the returned struct as proof. Close the lock before arm exec.
func verifyOnboardSealedRuntime(ctx context.Context, prepared installapply.NativeOnboardingPrepared) (err error) {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != 0 || prepared.RuntimePath != installapply.NativeRuntimePath ||
		prepared.OperationID != prepared.Manifest.Release.Source.OperationID || prepared.InstallerSHA256 != prepared.Manifest.Release.Source.InstallerSHA256 {
		return errors.New("invalid sealed runtime handoff")
	}
	if _, err := prepared.Manifest.Plan(); err != nil {
		return err
	}
	stage, err := installapply.OpenSourceStage()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, stage.Close()) }()
	if _, err := stage.VerifyManifest(ctx, prepared.Manifest); err != nil {
		return err
	}
	// The stage opener already verifies every ancestor of this private directory;
	// O_NOFOLLOW additionally prevents a replaced runtime symlink from executing.
	fd, err := unix.Open(installapply.NativeRuntimePath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	runtimeFile := os.NewFile(uintptr(fd), installapply.NativeRuntimePath)
	defer func() { err = errors.Join(err, runtimeFile.Close()) }()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o500 ||
		stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || stat.Size <= 0 || stat.Size > 64<<20 {
		return errors.New("unsafe sealed runtime metadata")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(runtimeFile, (64<<20)+1))
	if err != nil || n != stat.Size || hex.EncodeToString(hash.Sum(nil)) != prepared.InstallerSHA256 {
		return errors.New("sealed runtime digest differs from authenticated installer")
	}
	return ctx.Err()
}

func armOnboardRuntime(ctx context.Context, prepared installapply.NativeOnboardingPrepared) error {
	if err := verifyOnboardSealedRuntime(ctx, prepared); err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	// #nosec G204 -- Fixed sealed runtime path and action; VerifyManifest validated the canonical operation UUID and the root-owned executable digest immediately above. No shell is used.
	command := exec.CommandContext(bounded, installapply.NativeRuntimePath, "native-boot", "arm", "--operation-id="+prepared.OperationID)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.WaitDelay = 5 * time.Second
	// The runtime rechecks its running inode, sealed intent, journal and live
	// host before any boot mutation. Neither setup code nor tty input is passed.
	if err := command.Run(); err != nil {
		return errors.New("native one-shot boot arming did not complete; reboot was not requested; preserve installer state for inspection")
	}
	return bounded.Err()
}

func rebootOnboardHost(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || os.Geteuid() != 0 {
		return errors.New("reboot requires an active root onboarding session")
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, "/usr/bin/systemctl", "reboot")
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.WaitDelay = 5 * time.Second
	if err := command.Run(); err != nil {
		return errors.New("one-shot boot was armed but reboot request failed; preserve installer state and inspect before taking further action")
	}
	return bounded.Err()
}
