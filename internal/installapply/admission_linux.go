// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

const admissionName = "installation-admission.json"

// AdmitInstallation is an internal qualification API. The caller must arrange
// an early-boot closed gate and independent process-loss quarantine. No public
// installer or recovery CLI enables it yet. SourceStage retains the shared lock.
func (stage *SourceStage) AdmitInstallation(ctx context.Context, manifest NativeReleaseManifest, backend storageprep.Backend, gate InstallationGate, recovery *AdmissionRecovery, output io.Writer) (Result, error) {
	if err := stage.check(); err != nil {
		return Result{}, err
	}
	if stage.journal == nil || backend == nil {
		return Result{}, errors.New("admission requires the shared lock and storage backend")
	}
	plan, err := manifest.Plan()
	if err != nil {
		return Result{}, err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return Result{}, err
	}
	check := func(ctx context.Context) error {
		state, exists, err := stage.journal.Load()
		if err != nil {
			return err
		}
		if err := requireContinuationReady(state, exists, plan); err != nil {
			return err
		}
		decision, err := stage.AdvanceManifest(ctx, manifest, backend)
		if err != nil {
			return err
		}
		if !decision.Ready {
			return errors.New("storage not ready for admission")
		}
		return nil
	}
	coordinator := admissionCoordinator{store: nativeAdmissionStore{stage}, gate: gate, plan: plan, boot: strings.TrimSpace(string(boot)), check: check,
		install: func(ctx context.Context) (Result, error) {
			result, err := stage.continueInstallation(ctx, manifest, backend, gate, output)
			if err != nil {
				return result, err
			}
			if err := stage.registerNativeSetup(ctx, manifest); err != nil {
				return Result{}, err
			}
			return result, nil
		}}
	return coordinator.run(ctx, recovery)
}

// InspectAdmission is read-only under the shared lock. Malformed records are
// never normalized into recovery authorizations or silently repaired.
func (stage *SourceStage) InspectAdmission() (AdmissionInspection, error) {
	return (nativeAdmissionStore{stage}).Load()
}

type nativeAdmissionStore struct{ stage *SourceStage }

func (store nativeAdmissionStore) Load() (AdmissionInspection, error) {
	if err := store.stage.check(); err != nil {
		return AdmissionInspection{}, err
	}
	data, exists, err := admissionReadAt(store.stage.dir, admissionName)
	if err != nil {
		return AdmissionInspection{}, err
	}
	packages, packageExists, err := admissionReadAt(store.stage.dir, "install-state.json")
	if err != nil {
		return AdmissionInspection{}, err
	}
	if !exists && packageExists {
		return AdmissionInspection{}, errors.New("cannot adopt existing package state without admission history")
	}
	complete := false
	if packageExists {
		var journal Journal
		if json.Unmarshal(packages, &journal) != nil || validateJournal(journal) != nil {
			return AdmissionInspection{}, errors.New("invalid package recovery snapshot")
		}
		canonical, _ := json.MarshalIndent(journal, "", "  ")
		if !bytes.Equal(packages, append(canonical, '\n')) {
			return AdmissionInspection{}, errors.New("noncanonical package recovery snapshot")
		}
		complete = journal.Status == InstallComplete
		for _, stage := range journal.Stages {
			complete = complete && stage.Status == StageComplete && stage.Attempts > 0
		}
	} else {
		packages = []byte("absent package journal\n")
	}
	inspection := AdmissionInspection{Exists: exists, PackageComplete: complete, Review: AdmissionRecovery{PackageSHA256: admissionDigest(packages)}}
	if !exists {
		return inspection, nil
	}
	if json.Unmarshal(data, &inspection.State) != nil {
		return AdmissionInspection{}, errors.New("invalid admission JSON")
	}
	canonical, err := inspection.State.encode()
	if err != nil || !bytes.Equal(data, canonical) {
		return AdmissionInspection{}, errors.New("invalid or noncanonical admission record")
	}
	inspection.Review.StateSHA256 = admissionDigest(data)
	return inspection, nil
}

func (store nativeAdmissionStore) Save(next AdmissionState) error {
	data, err := next.encode()
	if err != nil {
		return err
	}
	previous, err := store.Load()
	if err != nil {
		return err
	}
	if !previous.Exists {
		if next.Phase != "checking" || next.Attempt != 1 {
			return errors.New("invalid initial admission state")
		}
	} else {
		old := previous.State
		if old.Plan != next.Plan {
			return errors.New("cannot rebind admission")
		}
		newAttempt := next.Phase == "checking" && next.Attempt == old.Attempt+1
		finish := next.Attempt == old.Attempt && next.BootID == old.BootID &&
			((old.Phase == "checking" && (next.Phase == "admitted" || next.Phase == "recovery-required")) || (old.Phase == "admitted" && next.Phase == "recovery-required"))
		if !newAttempt && !finish {
			return errors.New("invalid admission transition")
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := ".admission-" + hex.EncodeToString(random[:])
	if err := writeOriginRecord(store.stage.dir, temporary, data); err != nil {
		return err
	}
	defer unix.Unlinkat(store.stage.dir, temporary, 0)
	if err := unix.Renameat(store.stage.dir, temporary, store.stage.dir, admissionName); err != nil {
		return err
	}
	return unix.Fsync(store.stage.dir)
}

func admissionReadAt(dir int, name string) ([]byte, bool, error) {
	file, err := openResumeFile(dir, name, 64<<10)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Mode().Perm() != 0600 {
		return nil, false, errors.New("unsafe admission record mode")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil, false, errors.New("admission record exceeds bound")
	}
	return data, true, nil
}
