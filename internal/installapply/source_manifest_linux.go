// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

const NativeReleaseManifestPath = DefaultJournalDirectory + "/native-release-manifest.json"
const nativeReleaseManifestName = "native-release-manifest.json"

// SealManifest publishes a complete manifest before its Planned journal while
// retaining the same lock as source staging. Neither step arms a boot entry.
// A complete manifest without a journal can be reverified and sealed; partial
// manifest bytes cannot be overwritten. No backward journal transition exists.
func (stage *SourceStage) SealManifest(ctx context.Context, manifest NativeReleaseManifest) (storageprep.Plan, error) {
	if err := stage.check(); err != nil {
		return storageprep.Plan{}, err
	}
	if stage.journal == nil {
		return storageprep.Plan{}, errors.New("manifest sealing requires the shared journal lock")
	}
	plan, err := manifest.Plan()
	if err != nil {
		return storageprep.Plan{}, err
	}
	state, exists, err := stage.journal.Load()
	if err != nil {
		return storageprep.Plan{}, err
	}
	if exists && (state.Plan != plan || state.Phase != storageprep.Planned) {
		return storageprep.Plan{}, errors.New("manifest cannot rebind or reset storage preparation")
	}
	if _, err := stage.VerifyBinding(ctx, manifest.Release); err != nil {
		return storageprep.Plan{}, err
	}
	content, _ := json.MarshalIndent(manifest, "", "  ")
	content = append(content, '\n')
	var stat unix.Stat_t
	err = unix.Fstatat(stage.dir, nativeReleaseManifestName, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) && !exists {
		err = writeOriginRecord(stage.dir, nativeReleaseManifestName, content)
	} else if err == nil {
		err = equalOriginRecord(stage.dir, nativeReleaseManifestName, content)
	}
	if err != nil {
		return storageprep.Plan{}, err
	}
	if !exists {
		if err := ctx.Err(); err != nil {
			return storageprep.Plan{}, err
		}
		if err := stage.journal.Save(storageprep.State{SchemaVersion: storageprep.SchemaVersion, Plan: plan, Phase: storageprep.Planned}); err != nil {
			return storageprep.Plan{}, err
		}
	}
	return plan, nil
}

func (stage *SourceStage) VerifyManifest(ctx context.Context, manifest NativeReleaseManifest) (storageprep.Plan, error) {
	if err := stage.check(); err != nil {
		return storageprep.Plan{}, err
	}
	plan, err := manifest.Plan()
	if err != nil {
		return storageprep.Plan{}, err
	}
	content, _ := json.MarshalIndent(manifest, "", "  ")
	if err := equalOriginRecord(stage.dir, nativeReleaseManifestName, append(content, '\n')); err != nil {
		return storageprep.Plan{}, err
	}
	if _, err := stage.VerifyBinding(ctx, manifest.Release); err != nil {
		return storageprep.Plan{}, err
	}
	return plan, nil
}

// AdvanceManifest performs source/manifest checks under the same lock as boot
// state transitions. Its backend must check the actual intent's BootSHA256 and
// all existing host/offline readiness gates. This API is not a production runner.
func (stage *SourceStage) AdvanceManifest(ctx context.Context, manifest NativeReleaseManifest, backend storageprep.Backend) (storageprep.Decision, error) {
	if err := stage.check(); err != nil {
		return storageprep.Decision{}, err
	}
	if stage.journal == nil {
		return storageprep.Decision{}, errors.New("manifest advancement requires the shared journal lock")
	}
	plan, err := manifest.Plan()
	if err != nil {
		return storageprep.Decision{}, err
	}
	state, exists, err := stage.journal.Load()
	if err != nil || !exists || state.Plan != plan {
		return storageprep.Decision{}, errors.Join(err, errors.New("release manifest does not match the sealed journal"))
	}
	if state.Phase == storageprep.RecoveryRequired {
		return storageprep.Decision{State: state}, storageprep.ErrRecoveryRequired
	}
	if ctx == nil || ctx.Err() != nil {
		return storageprep.Decision{}, errors.New("manifest advancement requires an active context")
	}
	if _, err := stage.VerifyManifest(ctx, manifest); err != nil {
		state.Phase, state.FailureCode = storageprep.RecoveryRequired, "release-invalid"
		return storageprep.Decision{State: state}, errors.Join(storageprep.ErrRecoveryRequired, err, stage.journal.Save(state))
	}
	return storageprep.Advance(ctx, stage.journal, backend, plan)
}
