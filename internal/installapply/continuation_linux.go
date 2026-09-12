// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

const nativeInstallBindingName = "native-install-binding.json"

// continueInstallation is private to the admission coordinator, not a public CLI or
// generic native-journal bypass. The backend must independently verify the
// immutable boot intent, live host identity, mounts and quota enforcement.
// It only accepts Ready; it can neither arm nor resume filesystem conversion.
// The shared lock stays held across all real package/service installation.
// The caller must provide an acyclic, fail-closed boot/service graph.
func (stage *SourceStage) continueInstallation(ctx context.Context, manifest NativeReleaseManifest, backend storageprep.Backend, gate InstallationGate, output io.Writer) (Result, error) {
	if err := stage.check(); err != nil {
		return Result{}, err
	}
	if stage.journal == nil || ctx == nil || backend == nil || gate == nil {
		return Result{}, errors.New("continuation requires an active context, backend and shared journal lock")
	}
	plan, err := manifest.Plan()
	if err != nil {
		return Result{}, err
	}
	guard := func(ctx context.Context) error {
		if err := gate.VerifyClosed(ctx); err != nil {
			return err
		}
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
			return errors.New("storage readiness not established")
		}
		return nil
	}
	if err := guard(ctx); err != nil {
		return Result{}, err
	}
	source, err := stage.VerifyBinding(ctx, manifest.Release)
	if err != nil {
		return Result{}, err
	}
	runner, err := newLinuxRunner(output)
	if err != nil {
		return Result{}, err
	}
	if runner.Distribution() != plan.Distribution {
		return Result{}, errors.New("continuation platform differs from sealed plan")
	}
	if err := sealInstallBinding(stage.dir, plan); err != nil {
		return Result{}, err
	}
	store := nativeInstallStore{stage: stage, files: NewFileStore(), plan: plan}
	journal, exists, err := store.Load()
	if err != nil {
		return Result{}, err
	}
	if exists {
		for _, previous := range journal.Stages {
			if previous.ID == StageNGINX && previous.Status == StageComplete {
				// A failed check quarantines services. Reverify installed intent
				// before starting them for loopback health checks behind the gate.
				if err := runner.startForAdmission(ctx, source); err != nil {
					return Result{}, err
				}
				break
			}
		}
	}
	engine, err := NewEngine(store, continuationRunner{Runner: runner, guard: guard})
	if err != nil {
		return Result{}, err
	}
	return engine.Install(ctx, source)
}

// Publish binding before the first package journal. An existing package journal
// without this binding cannot be adopted, even with an identical release digest.
func sealInstallBinding(dir int, plan storageprep.Plan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	content, _ := json.MarshalIndent(plan, "", "  ")
	content = append(content, '\n')
	var stat unix.Stat_t
	err := unix.Fstatat(dir, nativeInstallBindingName, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		if err := unix.Fstatat(dir, "install-state.json", &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			return errors.Join(err, errors.New("cannot adopt package state without its native binding"))
		}
		return writeOriginRecord(dir, nativeInstallBindingName, content)
	}
	if err != nil {
		return err
	}
	return equalOriginRecord(dir, nativeInstallBindingName, content)
}

type nativeInstallStore struct {
	stage *SourceStage
	files *FileStore
	plan  storageprep.Plan
}

func (store nativeInstallStore) check() error {
	if err := store.stage.check(); err != nil {
		return err
	}
	content, _ := json.MarshalIndent(store.plan, "", "  ")
	return equalOriginRecord(store.stage.dir, nativeInstallBindingName, append(content, '\n'))
}

func (store nativeInstallStore) Load() (Journal, bool, error) {
	if err := store.check(); err != nil {
		return Journal{}, false, err
	}
	journal, exists, err := store.files.loadJournal()
	if err != nil || !exists {
		return journal, exists, err
	}
	if err := store.validate(journal); err != nil {
		return Journal{}, false, err
	}
	// The native adapter additionally rejects duplicates, aliases and any
	// noncanonical encoding. It never rewrites an unsafe existing journal.
	content, _ := json.MarshalIndent(journal, "", "  ")
	if err := equalOriginRecord(store.stage.dir, "install-state.json", append(content, '\n')); err != nil {
		return Journal{}, false, err
	}
	return journal, true, nil
}

func (store nativeInstallStore) Save(journal Journal) error {
	if err := store.check(); err != nil {
		return err
	}
	if err := store.validate(journal); err != nil {
		return err
	}
	return store.files.Save(journal)
}

func (store nativeInstallStore) validate(journal Journal) error {
	if err := validateJournal(journal); err != nil {
		return err
	}
	if journal.Version != store.plan.Version || journal.SourceDigest != store.plan.SourceDigest || journal.Distribution != store.plan.Distribution {
		return errors.New("package journal differs from native continuation binding")
	}
	return nil
}
