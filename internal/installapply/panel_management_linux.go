// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build linux

package installapply

import (
	"context"
	"errors"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strings"
)

// AcquirePanelManagement retains the existing installation lock for panel-only
// changes. It does not authorize package installation, storage continuation,
// admission, recovery, or updates. In particular FileStore.Load stays closed to
// every native journal, including Ready.
func AcquirePanelManagement(ctx context.Context) (io.Closer, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("panel management requires an active context")
	}
	stage, exists, err := openExistingSourceStage()
	if err != nil || !exists {
		return nil, errors.Join(err, errors.New("installed platform state required"))
	}
	if err := stage.checkPanelManagement(ctx); err != nil {
		_ = stage.Close()
		return nil, err
	}
	return stage, nil
}

func (stage *SourceStage) checkPanelManagement(ctx context.Context) error {
	journal, exists, err := NewFileStore().loadJournal()
	if err != nil || !exists || validateJournal(journal) != nil || journal.Status != InstallComplete {
		return errors.Join(err, errors.New("complete installation required for panel management"))
	}
	for _, item := range journal.Stages {
		if item.Status != StageComplete || item.Attempts < 1 {
			return errors.New("incomplete installation stage")
		}
	}
	if err := storageprep.CheckInactive(); err == nil {
		return ctx.Err()
	} else if !errors.Is(err, storageprep.ErrNotQualified) {
		return err
	}
	report, manifest, err := stage.recordedNative()
	if err != nil {
		return err
	}
	if err := validateNativeCompletedStatus(report, manifest); err != nil {
		return err
	}
	// The native snapshot loader also validates the canonical package journal and
	// exact native-install binding. No partial or approved-for-recovery state fits.
	boot, err := nativeHostKernelFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != report.Admission.State.BootID {
		return errors.New("native installation has not been admitted in this boot")
	}
	// Do not require raw block devices or a frozen installer executable here:
	// the renewal unit deliberately has PrivateDevices=yes. Inspect the live
	// mounted filesystem and kernel quota state through directory descriptors.
	var root unix.Stat_t
	if err := unix.Stat("/", &root); err != nil {
		return err
	}
	var source, target unix.Stat_t
	for path, info := range map[string]*unix.Stat_t{"/srv/stackfort-native-hosting": &source, "/srv/hosting": &target} {
		fd, err := openSourceDirectory(path, false)
		if err != nil {
			return err
		}
		err = unix.Fstat(fd, info)
		_ = unix.Close(fd)
		if err != nil || !nativeHostingDirectoryMetadata(*info, root.Dev) {
			return errors.New("native hosting directory drift")
		}
	}
	if source.Ino != target.Ino || source.Dev != target.Dev {
		return errors.New("native hosting bind source drift")
	}
	for _, path := range []string{"/", "/srv/hosting"} {
		if err := nativeVerifyQuotaMount(ctx, path, root.Dev); err != nil {
			return err
		}
	}
	if os.Geteuid() != 0 {
		return errors.New("panel management requires root")
	}
	return ctx.Err()
}
