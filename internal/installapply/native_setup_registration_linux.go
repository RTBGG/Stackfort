// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

const nativeSetupRegistrationName = "native-setup-registered.json"

type nativeSetupRegistration struct {
	SchemaVersion int                                `json:"schemaVersion"`
	OperationID   string                             `json:"operationId"`
	SetupSHA256   string                             `json:"setupSHA256"`
	Capability    core.RegisteredBootstrapCapability `json:"capability"`
}

func (record nativeSetupRegistration) validate(operation, setup string) error {
	if record.SchemaVersion != 1 || !validSourceOperation(operation) || record.OperationID != operation ||
		!pinDigestPattern.MatchString(setup) || record.SetupSHA256 != setup {
		return errors.New("setup registration differs from sealed operation")
	}
	if _, err := core.ParseID(string(record.Capability.ID)); err != nil {
		return errors.New("invalid setup registration identity")
	}
	if record.Capability.CreatedAt.IsZero() || record.Capability.ExpiresAt.Sub(record.Capability.CreatedAt) != time.Hour {
		return errors.New("invalid setup registration lifetime")
	}
	return nil
}

// Called while admission is closed and the source/installation lock is held,
// after all package, installed-payload, service-identity and health checks.
// Reboots never renew an existing setup code. Only its digest crosses stdin.
func (stage *SourceStage) registerNativeSetup(ctx context.Context, manifest NativeReleaseManifest) error {
	if ctx == nil || ctx.Err() != nil || stage.check() != nil || stage.journal == nil {
		return errors.New("setup registration requires an active locked installation")
	}
	state, exists, err := stage.journal.Load()
	plan, planErr := manifest.Plan()
	if err != nil || planErr != nil || requireNativeRuntimeReady(state, exists, plan) != nil {
		return errors.New("setup registration requires verified converted storage")
	}
	data, exists, err := admissionReadAt(stage.dir, nativeRuntimeName)
	if err == nil && !exists && manifest.Release.Policy.Class == "lab-candidate" {
		// Only the historical independently authenticated pre-runtime lab path
		// can omit runtime/hand-off records. A partial setup is never absence.
		if err := nativeOrphansAt(stage.dir, []string{nativeSetupName, nativeSetupRegistrationName, nativeOnboardingSourceName}); err != nil {
			return err
		}
		_, err := stage.VerifyBinding(ctx, manifest.Release)
		return err
	}
	if err != nil || !exists {
		return errors.Join(err, errors.New("missing setup runtime binding"))
	}
	var intent NativeRuntimeIntent
	if err := nativeBootDecode(data, &intent); err != nil {
		return err
	}
	digest, err := intent.Digest()
	if err != nil || digest != manifest.BootSHA256 {
		return errors.New("invalid setup runtime binding")
	}
	if err := stage.verifyNativeSetupBinding(manifest, intent); err != nil {
		return err
	}
	data, registered, err := admissionReadAt(stage.dir, nativeSetupRegistrationName)
	if err != nil {
		return err
	}
	if intent.SetupSHA256 == "" {
		if registered || manifest.Release.Policy.Class == "tag-release" {
			return errors.New("orphan setup registration")
		}
		return nil // Historical qualification path has no pre-reboot setup code.
	}
	if registered {
		var record nativeSetupRegistration
		if err := nativeBootDecode(data, &record); err != nil {
			return err
		}
		if !bytes.Equal(data, nativeBootJSON(record)) {
			return errors.New("noncanonical setup registration")
		}
		return record.validate(plan.OperationID, intent.SetupSHA256)
	}
	data, exists, err = admissionReadAt(stage.dir, nativeSetupName)
	if err != nil || !exists {
		return errors.New("missing setup commitment")
	}
	commitment, err := decodeNativeSetup(data, manifest.Release)
	if err != nil {
		return err
	}
	source, err := stage.VerifyBinding(ctx, manifest.Release)
	if err != nil {
		return err
	}
	expected, err := nativeTrustedHash(ctx, filepath.Join(source.Root, "bin", "stackfort-api"))
	if err != nil {
		return err
	}
	actual, err := nativeTrustedHash(ctx, "/usr/local/bin/stackfort-api")
	if err != nil || actual != expected {
		return errors.New("installed setup API differs from authenticated release")
	}
	if _, _, err := serviceIdentity(); err != nil {
		return err
	}
	capability, err := nativeImportSetupDigest(ctx, commitment.TokenSHA256)
	if err != nil {
		return err
	}
	record := nativeSetupRegistration{SchemaVersion: 1, OperationID: plan.OperationID, SetupSHA256: intent.SetupSHA256, Capability: capability}
	if err := record.validate(plan.OperationID, intent.SetupSHA256); err != nil {
		return err
	}
	// A lost reply or failed receipt write may have committed the digest in
	// SQLite. Exact active retry is idempotent there and preserves the expiry.
	return writeOriginRecord(stage.dir, nativeSetupRegistrationName, nativeBootJSON(record))
}

func nativeImportSetupDigest(ctx context.Context, digest string) (core.RegisteredBootstrapCapability, error) {
	var result core.RegisteredBootstrapCapability
	if ctx == nil || ctx.Err() != nil || !pinDigestPattern.MatchString(digest) {
		return result, errors.New("invalid local setup registration")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// No root SQLite writes: keep DB/WAL/SHM owned by the verified service user.
	command := exec.CommandContext(ctx, "/usr/sbin/runuser", "--user", "stackfort", "--", "/usr/local/bin/stackfort-api", "bootstrap", "import-digest", "--ttl=1h")
	containNativeCommand(command)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "HOME=/var/lib/stackfort", "STACKFORT_STATE_PATH=/var/lib/stackfort/stackfort.db"}
	command.Stdin = strings.NewReader(digest + "\n")
	var stdout, stderr nativeBootOutput
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil || stdout.overflow || stderr.overflow || ctx.Err() != nil {
		// Do not expose child output or input in journal/error messages.
		return result, errors.New("local administrator setup registration failed; retain installation evidence")
	}
	return decodeNativeSetupImportResponse([]byte(stdout.String()))
}
