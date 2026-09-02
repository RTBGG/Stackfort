// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package updateapply

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/installapply"
	"github.com/RTBGG/stackfort/internal/store"
	"golang.org/x/sys/unix"
)

const (
	defaultPanelStatePath      = "/var/lib/stackfort/stackfort.db"
	maximumUpdateCommandOutput = 64 << 10
	minimumUpdateFreeBytes     = 512 << 20
)

var stoppedUpdateUnits = []string{
	"stackfort-api.service", "stackfort-phpmyadmin.service", "stackfort-agent.service",
}

type LinuxRunner struct {
	installer   *installapply.LinuxRunner
	output      io.Writer
	statePath   string
	backupPath  string
	runOverride func(context.Context, string, ...string) error
}

func NewLinuxRunner(output io.Writer, target installapply.Source) (*LinuxRunner, error) {
	if output == nil {
		output = io.Discard
	}
	installer, err := installapply.NewLinuxUpdateRunner(output)
	if err != nil {
		return nil, err
	}
	if len(target.Digest) != 64 {
		return nil, errors.New("target release digest is invalid")
	}
	return &LinuxRunner{
		installer: installer, output: output, statePath: defaultPanelStatePath,
		backupPath: filepath.Join(DefaultBackupDirectory, target.Digest+".sqlite"),
	}, nil
}

func (runner *LinuxRunner) Preflight(
	ctx context.Context,
	current installapply.Source,
	target installapply.Source,
) error {
	if os.Geteuid() != 0 {
		return errors.New("Stackfort updates must run as root")
	}
	if buildinfo.Current().Version != current.Version {
		return fmt.Errorf("trusted updater version %s does not match current release %s",
			buildinfo.Current().Version, current.Version)
	}
	if err := runner.installer.VerifyInstallation(ctx, current); err != nil {
		return fmt.Errorf("verify installed release before update: %w", err)
	}
	if err := secureUpdaterSubdirectory(DefaultReleaseDirectory); err != nil {
		return err
	}
	if err := secureUpdaterSubdirectory(DefaultBackupDirectory); err != nil {
		return err
	}
	if err := ensureFreeSpace(DefaultStateDirectory, minimumUpdateFreeBytes); err != nil {
		return err
	}
	if err := removePriorTerminalBackup(runner.backupPath); err != nil {
		return err
	}
	return nil
}

func (runner *LinuxRunner) Apply(
	ctx context.Context,
	stage StageID,
	current installapply.Source,
	target installapply.Source,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(runner.output, "Applying updater stage: %s\n", stage)
	switch stage {
	case StageStopServices:
		return runner.stopServices(ctx)
	case StageBackupState:
		return runner.backupState(ctx)
	case StageNativePackages:
		return runner.applyInstallerStages(ctx, target,
			installapply.StageWAFPackage, installapply.StageVinylPackage)
	case StagePayload:
		return runner.applyInstallerStages(ctx, target, installapply.StagePayload)
	case StageConfiguration:
		return runner.applyInstallerStages(ctx, target,
			installapply.StageConfiguration, installapply.StageSecurity, installapply.StageNGINX)
	case StageMigration:
		return runner.databaseCommand(ctx, "migrate")
	case StageStartServices:
		if err := runner.run(ctx, "/usr/bin/systemctl", "restart", "vinyl.service", "nginx.service"); err != nil {
			return err
		}
		_, err := runner.installer.Apply(ctx, installapply.StageServices, target)
		return err
	case StageHealth:
		return runner.installer.VerifyInstallation(ctx, target)
	default:
		return errors.New("updater stage is not allowlisted")
	}
}

func (runner *LinuxRunner) Verify(
	ctx context.Context,
	stage StageID,
	_ installapply.Source,
	target installapply.Source,
) error {
	switch stage {
	case StageStopServices:
		for _, unit := range stoppedUpdateUnits {
			if runner.commandSucceeds(ctx, "/usr/bin/systemctl", "is-active", "--quiet", unit) {
				return fmt.Errorf("service remained active during update: %s", unit)
			}
		}
		return nil
	case StageBackupState:
		return verifyRootRegular(runner.backupPath, 0o600, 1<<40)
	case StageNativePackages:
		return runner.verifyInstallerStages(ctx, target,
			installapply.StageWAFPackage, installapply.StageVinylPackage)
	case StagePayload:
		return runner.verifyInstallerStages(ctx, target, installapply.StagePayload)
	case StageConfiguration:
		return runner.verifyInstallerStages(ctx, target,
			installapply.StageConfiguration, installapply.StageSecurity, installapply.StageNGINX)
	case StageMigration:
		return runner.databaseCommand(ctx, "check")
	case StageStartServices:
		return runner.installer.Verify(ctx, installapply.StageServices, target)
	case StageHealth:
		return runner.installer.VerifyInstallation(ctx, target)
	default:
		return errors.New("updater stage is not allowlisted")
	}
}

func (runner *LinuxRunner) Rollback(
	ctx context.Context,
	current installapply.Source,
	_ installapply.Source,
	journal Journal,
) error {
	var result error
	if err := runner.stopServices(ctx); err != nil {
		result = errors.Join(result, err)
	}
	if rollbackRequiresBackup(journal) {
		if err := runner.restoreState(); err != nil {
			result = errors.Join(result, err)
		}
	}
	for _, stages := range [][]installapply.StageID{
		{installapply.StageWAFPackage, installapply.StageVinylPackage},
		{installapply.StagePayload},
		{installapply.StageConfiguration, installapply.StageSecurity, installapply.StageNGINX},
	} {
		if err := runner.applyInstallerStages(ctx, current, stages...); err != nil {
			result = errors.Join(result, err)
			break
		}
	}
	if err := runner.run(ctx, "/usr/bin/systemctl", "restart", "vinyl.service", "nginx.service"); err != nil {
		result = errors.Join(result, err)
	}
	if _, err := runner.installer.Apply(ctx, installapply.StageServices, current); err != nil {
		result = errors.Join(result, err)
	} else if err := runner.installer.VerifyInstallation(ctx, current); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

func rollbackRequiresBackup(journal Journal) bool {
	for index, stage := range journal.Stages {
		if stage.ID == StageBackupState && (stage.Status == StageApplying || stage.Status == StageComplete) {
			return true
		}
		if index > indexOfOrderedStage(StageBackupState) && stage.Status != StagePending {
			return true
		}
	}
	return false
}

func indexOfOrderedStage(wanted StageID) int {
	for index, stage := range orderedStages {
		if stage == wanted {
			return index
		}
	}
	return len(orderedStages)
}

func (runner *LinuxRunner) applyInstallerStages(
	ctx context.Context,
	source installapply.Source,
	stages ...installapply.StageID,
) error {
	for _, stage := range stages {
		if _, err := runner.installer.Apply(ctx, stage, source); err != nil {
			return fmt.Errorf("apply %s: %w", stage, err)
		}
		if err := runner.installer.Verify(ctx, stage, source); err != nil {
			return fmt.Errorf("verify %s: %w", stage, err)
		}
	}
	return nil
}

func (runner *LinuxRunner) verifyInstallerStages(
	ctx context.Context,
	source installapply.Source,
	stages ...installapply.StageID,
) error {
	for _, stage := range stages {
		if err := runner.installer.Verify(ctx, stage, source); err != nil {
			return fmt.Errorf("verify %s: %w", stage, err)
		}
	}
	return nil
}

func (runner *LinuxRunner) stopServices(ctx context.Context) error {
	arguments := append([]string{"stop"}, stoppedUpdateUnits...)
	return runner.run(ctx, "/usr/bin/systemctl", arguments...)
}

func (runner *LinuxRunner) backupState(ctx context.Context) (returnErr error) {
	state, err := store.Open(ctx, runner.statePath)
	if err != nil {
		return fmt.Errorf("open panel state for update backup: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, state.Close()) }()
	if err := state.Backup(ctx, runner.backupPath); err != nil {
		return fmt.Errorf("create pre-migration state backup: %w", err)
	}
	return nil
}

func (runner *LinuxRunner) restoreState() error {
	if err := verifyRootRegular(runner.backupPath, 0o600, 1<<40); err != nil {
		return fmt.Errorf("verify rollback database: %w", err)
	}
	uid, gid, err := stackfortIdentity()
	if err != nil {
		return err
	}
	directory := filepath.Dir(runner.statePath)
	temporary, err := os.CreateTemp(directory, ".stackfort-update-rollback-*")
	if err != nil {
		return fmt.Errorf("create rollback database: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	source, err := os.Open(runner.backupPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(temporary, io.LimitReader(source, 1<<40))
	closeErr := source.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if err := temporary.Chown(uid, gid); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := runner.statePath + suffix
		info, statErr := os.Lstat(path)
		if statErr == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe SQLite sidecar blocks rollback: %s", path)
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	if err := os.Rename(temporaryPath, runner.statePath); err != nil {
		return err
	}
	cleanup = false
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}

func (runner *LinuxRunner) databaseCommand(ctx context.Context, command string) error {
	return runner.run(ctx, "/usr/sbin/runuser", "--user", "stackfort", "--", "/usr/bin/env",
		"STACKFORT_STATE_PATH="+runner.statePath,
		"STACKFORT_MASTER_KEY_PATH=/etc/stackfort/master.key",
		"/usr/local/bin/stackfort-api", "database", command)
}

func (runner *LinuxRunner) run(ctx context.Context, executable string, arguments ...string) error {
	if runner.runOverride != nil {
		return runner.runOverride(ctx, executable, arguments...)
	}
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	output, err := command.CombinedOutput()
	if err != nil {
		if len(output) > maximumUpdateCommandOutput {
			output = output[:maximumUpdateCommandOutput]
		}
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(executable), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (runner *LinuxRunner) commandSucceeds(ctx context.Context, executable string, arguments ...string) bool {
	return runner.run(ctx, executable, arguments...) == nil
}

func secureUpdaterSubdirectory(path string) error {
	if err := secureRootDirectory(DefaultStateDirectory); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("updater subdirectory has unsafe metadata: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return fmt.Errorf("updater subdirectory has unsafe ownership: %s", path)
	}
	return nil
}

func ensureFreeSpace(path string, minimum uint64) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return err
	}
	available := stat.Bavail * uint64(stat.Bsize)
	if available < minimum {
		return errors.New("updater state filesystem has less than 512 MiB free")
	}
	return nil
}

func removePriorTerminalBackup(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("prior updater backup has an unsafe file type")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 || info.Mode().Perm() != 0o600 {
		return errors.New("prior updater backup has unsafe metadata")
	}
	return os.Remove(path)
}

func verifyRootRegular(path string, mode os.FileMode, maximum int64) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != mode || info.Size() <= 0 || info.Size() > maximum {
		return fmt.Errorf("file has unsafe metadata: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 0 {
		return fmt.Errorf("file has unsafe ownership: %s", path)
	}
	return nil
}

func stackfortIdentity() (int, int, error) {
	account, err := user.Lookup("stackfort")
	if err != nil {
		return 0, 0, errors.New("resolve Stackfort service identity")
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil || uid <= 0 {
		return 0, 0, errors.New("Stackfort service UID is invalid")
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil || gid <= 0 {
		return 0, 0, errors.New("Stackfort service GID is invalid")
	}
	return uid, gid, nil
}
