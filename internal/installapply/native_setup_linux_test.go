// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
	"github.com/RTBGG/stackfort/internal/storageprep"
	"golang.org/x/sys/unix"
)

func TestNativeSetupCommitmentContainsOnlyExactReleaseBoundDigest(t *testing.T) {
	_, review := testNativeOnboarding(t)
	previous := ""
	for range 2 {
		code, commitment, err := IssueNativeSetup(review)
		if err != nil || !strings.HasPrefix(code, "sfb_") || len(code) != 47 || code == previous {
			t.Fatal("invalid or repeated issued setup code")
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "sfb_"))
		if err != nil || len(raw) != 32 || commitment.TokenSHA256 != admissionDigest([]byte(code)) || commitment.Validate(review) != nil {
			t.Fatal("issued commitment differs from the complete displayed token")
		}
		data := nativeBootJSON(commitment)
		decoded, err := decodeNativeSetup(data, review.Recovery.Release)
		if err != nil || decoded != commitment || bytes.Contains(data, []byte(code)) || bytes.Contains(data, raw) || bytes.Contains(data, []byte(`"token":`)) {
			t.Fatal("invalid commitment or raw setup material persisted")
		}
		previous = code
	}
	bad := review
	bad.SchemaVersion++
	if code, commitment, err := IssueNativeSetup(bad); err == nil || code != "" || commitment != (NativeSetupCommitment{}) {
		t.Fatal("invalid review issued a setup capability")
	}
}

func TestNativeSetupRejectsMalformedNoncanonicalAndForeignCommitments(t *testing.T) {
	_, review := testNativeOnboarding(t)
	_, commitment, err := IssueNativeSetup(review)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*NativeSetupCommitment){
		"schema":         func(c *NativeSetupCommitment) { c.SchemaVersion++ },
		"operation":      func(c *NativeSetupCommitment) { c.OperationID = "11111111-1111-4111-8111-111111111111" },
		"release":        func(c *NativeSetupCommitment) { c.ReleaseSHA256 = strings.Repeat("e", 64) },
		"missing-digest": func(c *NativeSetupCommitment) { c.TokenSHA256 = "" },
		"uppercase":      func(c *NativeSetupCommitment) { c.TokenSHA256 = strings.Repeat("A", 64) },
		"zero-digest":    func(c *NativeSetupCommitment) { c.TokenSHA256 = strings.Repeat("0", 64) },
		"newline":        func(c *NativeSetupCommitment) { c.TokenSHA256 += "\n" },
		"raw-token":      func(c *NativeSetupCommitment) { c.TokenSHA256 = "sfb_synthetic_secret_never_valid" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := commitment
			mutate(&changed)
			if _, err := decodeNativeSetup(nativeBootJSON(changed), review.Recovery.Release); err == nil || strings.Contains(err.Error(), "sfb_synthetic_secret") {
				t.Fatal("invalid commitment accepted or raw input leaked")
			}
		})
	}
	canonical := nativeBootJSON(commitment)
	for _, data := range [][]byte{nil, []byte("{"), bytes.Repeat([]byte("x"), (64<<10)+1), bytes.TrimSpace(canonical),
		append(append([]byte{}, canonical...), []byte("{}\n")...),
		append([]byte("{\"schemaVersion\":1,"), canonical[1:]...),
		append([]byte("{\"token\":\"sfb_synthetic_secret_never_valid\","), canonical[1:]...)} {
		if _, err := decodeNativeSetup(data, review.Recovery.Release); err == nil || strings.Contains(err.Error(), "sfb_synthetic_secret") {
			t.Fatal("noncanonical record accepted or raw input leaked")
		}
	}
	foreign := review.Recovery.Release
	foreign.Policy.Commit = strings.Repeat("e", 40)
	if _, err := decodeNativeSetup(canonical, foreign); err == nil {
		t.Fatal("commitment accepted for a different authenticated commit")
	}
}

func TestNativeSetupRuntimeDigestBindsTheCommitment(t *testing.T) {
	intent := testBootIntent()
	baseline, err := intent.Digest()
	if err != nil {
		t.Fatal(err)
	}
	intent.SetupSHA256 = strings.Repeat("b", 64)
	bound, err := intent.Digest()
	if err != nil || bound == baseline {
		t.Fatal("setup commitment omitted from runtime digest")
	}
	intent.SetupSHA256 = strings.Repeat("c", 64)
	changed, err := intent.Digest()
	if err != nil || changed == bound {
		t.Fatal("changed setup commitment omitted from runtime digest")
	}
	for _, value := range []string{"invalid", strings.Repeat("B", 64), strings.Repeat("b", 63)} {
		intent.SetupSHA256 = value
		if _, err := intent.Digest(); err == nil {
			t.Fatal("invalid setup runtime digest accepted")
		}
	}
	legacy := testRuntimeIntent()
	legacy.SetupSHA256 = strings.Repeat("b", 64)
	if _, err := legacy.Digest(); err == nil {
		t.Fatal("post-ready-only runtime acquired pre-reboot setup authority")
	}
}

func setupRegistrationFixture(operation, setup string) nativeSetupRegistration {
	created := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	return nativeSetupRegistration{SchemaVersion: 1, OperationID: operation, SetupSHA256: setup,
		Capability: core.RegisteredBootstrapCapability{ID: "019ffd13-9819-7c51-9f53-0d0ed3b36c42", CreatedAt: created, ExpiresAt: created.Add(time.Hour)}}
}

func TestNativeSetupRegistrationReceiptPreservesOriginalLifetime(t *testing.T) {
	operation, setup := testSourcePin().OperationID, strings.Repeat("b", 64)
	record := setupRegistrationFixture(operation, setup)
	for _, repeated := range []bool{false, true} {
		record.Capability.AlreadyRegistered = repeated
		if err := record.validate(operation, setup); err != nil {
			t.Fatal("recorded historical expiry must not block normal subsequent boots", err)
		}
	}
	for name, mutate := range map[string]func(*nativeSetupRegistration){
		"schema":    func(r *nativeSetupRegistration) { r.SchemaVersion++ },
		"operation": func(r *nativeSetupRegistration) { r.OperationID = "11111111-1111-4111-8111-111111111111" },
		"setup":     func(r *nativeSetupRegistration) { r.SetupSHA256 = strings.Repeat("c", 64) },
		"id":        func(r *nativeSetupRegistration) { r.Capability.ID = "invalid" },
		"v4-id":     func(r *nativeSetupRegistration) { r.Capability.ID = "11111111-1111-4111-8111-111111111111" },
		"zero-time": func(r *nativeSetupRegistration) { r.Capability.CreatedAt = time.Time{} },
		"extended":  func(r *nativeSetupRegistration) { r.Capability.ExpiresAt = r.Capability.ExpiresAt.Add(time.Nanosecond) },
		"shortened": func(r *nativeSetupRegistration) {
			r.Capability.ExpiresAt = r.Capability.ExpiresAt.Add(-time.Nanosecond)
		},
		"reversed": func(r *nativeSetupRegistration) { r.Capability.ExpiresAt = r.Capability.CreatedAt.Add(-time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := record
			mutate(&changed)
			if err := changed.validate(operation, setup); err == nil {
				t.Fatal("invalid registration receipt accepted")
			}
		})
	}
	if record.validate("invalid", setup) == nil || record.validate(operation, "invalid") == nil {
		t.Fatal("invalid expected binding accepted")
	}
}

func TestNativeSetupInvalidContextOrDigestNeverInvokesLocalRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, context := range []context.Context{nil, ctx} {
		if result, err := nativeImportSetupDigest(context, strings.Repeat("b", 64)); err == nil || result != (core.RegisteredBootstrapCapability{}) {
			t.Fatal("inactive setup registration reached subprocess")
		}
	}
	for _, digest := range []string{"", "sfb_synthetic_secret_never_valid", strings.Repeat("B", 64), strings.Repeat("b", 63)} {
		result, err := nativeImportSetupDigest(t.Context(), digest)
		if err == nil || result != (core.RegisteredBootstrapCapability{}) || strings.Contains(err.Error(), "sfb_synthetic_secret") {
			t.Fatal("invalid registration input accepted or leaked")
		}
	}
	_, review := testNativeOnboarding(t)
	_, commitment, err := IssueNativeSetup(review)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []*SourceStage{nil, {}, {closed: true}} {
		if err := stage.saveNativeSetup(t.Context(), review.Recovery.Release, commitment); err == nil {
			t.Fatal("unlocked setup write accepted")
		}
		if _, err := stage.nativeSetupDigest(review.Recovery.Release); err == nil {
			t.Fatal("unlocked setup read accepted")
		}
		if err := stage.registerNativeSetup(t.Context(), NativeReleaseManifest{}); err == nil {
			t.Fatal("unlocked setup registration accepted")
		}
	}
}

func TestNativeSetupPreparationFailureCannotReturnReadyOrOpenAdmission(t *testing.T) {
	for _, failure := range []string{"", "save-setup", "prepare-boot", "close"} {
		t.Run(failure, func(t *testing.T) {
			coordinator, stage := onboardingFlowFixture(t)
			code, commitment, err := IssueNativeSetup(stage.review)
			if err != nil {
				t.Fatal(err)
			}
			stage.fail = failure
			result, err := coordinator.prepareWithSetup(t.Context(), stage.request, stage.review, &commitment)
			if (err == nil) != (failure == "") || !stage.closed || (failure != "" && result != (NativeOnboardingPrepared{})) {
				t.Fatal("failed setup preparation returned usable result or left lock open")
			}
			if failure == "save-setup" && stage.prepared {
				t.Fatal("boot preparation preceded setup commitment persistence")
			}
			if bytes.Contains(nativeBootJSON(result), []byte(code)) || strings.Contains(strings.Join(stage.events, ","), code) {
				t.Fatal("raw setup code entered result or events")
			}
		})
	}
	coordinator, store, gate, _ := admissionFixture(t)
	coordinator.install = func(context.Context) (Result, error) {
		return Result{}, errors.New("synthetic setup registration failure")
	}
	if _, err := coordinator.run(t.Context(), nil); err == nil || gate.openCalls != 0 || gate.open || !gate.stopped || store.inspection.State.Phase != "recovery-required" {
		t.Fatal("setup registration failure opened listeners instead of quarantine")
	}
}

// All fixed-path state is mounted over a temporary directory in a separate
// process/mount namespace. Tests only read existing synthetic registration
// receipts or reject before registration; they never invoke runuser/API/SQLite.
func TestDisposableNativeSetupRecordBoundaries(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" {
		t.Skip("requires disposable root host and private mount namespace")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root")
	}
	if os.Getenv("STACKFORT_NATIVE_SETUP_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(t.Context(), "/usr/bin/unshare", "--mount", "--propagation", "private", self, "-test.v", "-test.run=^TestDisposableNativeSetupRecordBoundaries$")
		command.Env = append(os.Environ(), "STACKFORT_NATIVE_SETUP_CHILD=1")
		output, err := command.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	nativePackageTestNamespace(t)
	for _, scenario := range []string{"valid", "missing", "changed", "truncated", "duplicate", "unknown", "symlink", "hardlink", "fifo", "directory", "mode", "foreign-owner", "oversized", "closed-journal", "closed-stage", "missing-origin"} {
		t.Run("commitment-"+scenario, func(t *testing.T) {
			stage := setupNamespaceStage(t)
			_, review := testNativeOnboarding(t)
			code, commitment, err := IssueNativeSetup(review)
			if err != nil {
				t.Fatal(err)
			}
			data := nativeBootJSON(commitment)
			intent := testBootIntent()
			intent.SetupSHA256 = admissionDigest(data)
			manifest := NativeReleaseManifest{Release: review.Recovery.Release}
			if scenario == "missing-origin" {
				if err := stage.saveNativeSetup(t.Context(), review.Recovery.Release, commitment); err == nil {
					t.Fatal("commitment saved without authentic retained origin")
				}
				if _, err := os.Lstat(filepath.Join(DefaultJournalDirectory, nativeSetupName)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed unauthenticated setup attempt wrote commitment")
				}
				return
			}
			setupWriteUnsafeRecord(t, nativeSetupName, data, scenario)
			if scenario == "closed-journal" {
				if err := stage.journal.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "closed-stage" {
				if err := stage.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := stage.verifyNativeSetupBinding(manifest, intent); (err == nil) != (scenario == "valid") {
				t.Fatal("unsafe or changed setup commitment accepted", scenario, err)
			}
			if scenario == "valid" {
				before, err := os.ReadFile(filepath.Join(DefaultJournalDirectory, nativeSetupName))
				if err != nil || bytes.Contains(before, []byte(code)) {
					t.Fatal("raw code persisted")
				}
				if writeOriginRecord(stage.dir, nativeSetupName, data) == nil {
					t.Fatal("setup commitment silently reissued")
				}
				after, _ := os.ReadFile(filepath.Join(DefaultJournalDirectory, nativeSetupName))
				if !bytes.Equal(before, after) {
					t.Fatal("failed replay modified commitment")
				}
			}
		})
	}
	for _, scenario := range []string{"valid", "expired", "truncated", "duplicate", "unknown", "changed", "symlink", "hardlink", "fifo", "directory", "mode", "foreign-owner", "oversized", "wrong-operation", "wrong-setup", "lifetime", "wrong-state", "wrong-manifest", "missing-runtime", "changed-runtime", "missing-commitment", "closed-journal", "cancelled"} {
		t.Run("registration-"+scenario, func(t *testing.T) {
			stage := setupNamespaceStage(t)
			manifest, intent, commitment := setupRegistrationState(t, stage, "tag-release", true)
			record := setupRegistrationFixture(manifest.Release.Source.OperationID, intent.SetupSHA256)
			if scenario == "expired" {
				record.Capability.AlreadyRegistered = true
			}
			switch scenario {
			case "wrong-operation":
				record.OperationID = "11111111-1111-4111-8111-111111111111"
			case "wrong-setup":
				record.SetupSHA256 = strings.Repeat("f", 64)
			case "lifetime":
				record.Capability.ExpiresAt = record.Capability.ExpiresAt.Add(time.Second)
			case "wrong-manifest":
				manifest.Host.Kernel += "-changed"
			case "wrong-state":
				state, _, err := stage.journal.Load()
				if err != nil {
					t.Fatal(err)
				}
				state.Phase, state.FailureCode = storageprep.RecoveryRequired, "readiness-lost"
				if err := stage.journal.Save(state); err != nil {
					t.Fatal(err)
				}
			case "missing-runtime":
				if err := os.Remove(filepath.Join(DefaultJournalDirectory, nativeRuntimeName)); err != nil {
					t.Fatal(err)
				}
			case "changed-runtime":
				intent.SetupSHA256 = strings.Repeat("f", 64)
				writeStageFixture(t, DefaultJournalDirectory, nativeRuntimeName, nativeBootJSON(intent), 0600)
			case "missing-commitment":
				if err := os.Remove(filepath.Join(DefaultJournalDirectory, nativeSetupName)); err != nil {
					t.Fatal(err)
				}
			case "closed-journal":
				if err := stage.journal.Close(); err != nil {
					t.Fatal(err)
				}
			}
			setupWriteUnsafeRecord(t, nativeSetupRegistrationName, nativeBootJSON(record), scenario)
			ctx := t.Context()
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := stage.registerNativeSetup(ctx, manifest)
			if (err == nil) != (scenario == "valid" || scenario == "expired") {
				t.Fatal("registration boundary", scenario, err)
			}
			if err == nil {
				before, _ := os.ReadFile(filepath.Join(DefaultJournalDirectory, nativeSetupRegistrationName))
				if err := stage.registerNativeSetup(ctx, manifest); err != nil {
					t.Fatal(err)
				}
				after, _ := os.ReadFile(filepath.Join(DefaultJournalDirectory, nativeSetupRegistrationName))
				if !bytes.Equal(before, after) || bytes.Contains(after, []byte(commitment.TokenSHA256)) {
					t.Fatal("registration replay renewed or leaked capability")
				}
			}
		})
	}
	for _, scenario := range []string{"lab-no-runtime", "lab-empty-runtime", "tag-no-runtime", "tag-empty-runtime", "lab-orphan-commitment", "lab-orphan-registration", "lab-orphan-onboarding"} {
		t.Run(scenario, func(t *testing.T) {
			stage := setupNamespaceStage(t)
			class := "lab-candidate"
			if strings.HasPrefix(scenario, "tag-") {
				class = "tag-release"
			}
			manifest, _, _ := setupRegistrationState(t, stage, class, false)
			if !strings.HasSuffix(scenario, "empty-runtime") {
				if err := os.Remove(filepath.Join(DefaultJournalDirectory, nativeRuntimeName)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "lab-orphan-commitment" {
				writeStageFixture(t, DefaultJournalDirectory, nativeSetupName, []byte("{}\n"), 0600)
			}
			if scenario == "lab-orphan-registration" {
				writeStageFixture(t, DefaultJournalDirectory, nativeSetupRegistrationName, []byte("{}\n"), 0600)
			}
			if scenario == "lab-orphan-onboarding" {
				writeStageFixture(t, DefaultJournalDirectory, nativeOnboardingSourceName, []byte("{}\n"), 0600)
			}
			err := stage.registerNativeSetup(t.Context(), manifest)
			if scenario == "lab-no-runtime" {
				// Compatibility still requires actual retained source verification.
				// This fixture has none, so the namespace read must fail ENOENT;
				// a blanket missing-runtime rejection would fail this assertion.
				if !errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "runtime") {
					t.Fatal("legacy branch skipped source verification or required runtime", err)
				}
				return
			}
			if (err == nil) != (scenario == "lab-empty-runtime") {
				t.Fatal("historical compatibility widened or broken", scenario, err)
			}
		})
	}
	for _, name := range []string{nativeSetupName, nativeSetupRegistrationName, nativeOnboardingSourceName} {
		t.Run("operator-orphan-"+name, func(t *testing.T) {
			stage := setupNamespaceStage(t)
			writeStageFixture(t, DefaultJournalDirectory, name, []byte("{}\n"), 0600)
			if _, _, err := stage.recordedNative(); err == nil {
				t.Fatal("orphan setup state hidden from read-only operator")
			}
		})
	}
}

func setupNamespaceStage(t *testing.T) *SourceStage {
	t.Helper()
	nativePackageTestNamespace(t)
	fixture := t.TempDir()
	if err := unix.Mount(fixture, "/var/lib", "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount("/var/lib", 0); err != nil {
			t.Error(err)
		}
	})
	stage, err := OpenSourceStage()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := stage.Close(); err != nil {
			t.Error(err)
		}
	})
	return stage
}

func setupRegistrationState(t *testing.T, stage *SourceStage, class string, withSetup bool) (NativeReleaseManifest, NativeRuntimeIntent, NativeSetupCommitment) {
	t.Helper()
	_, review := testNativeOnboarding(t)
	binding := review.Recovery.Release
	if class == "lab-candidate" {
		binding.Policy = OriginPolicy{Class: class, Version: "0.1.0-beta.3", Commit: "5282946bec1f865de7222128a6a5d0d8a656f34c"}
	}
	intent := testBootIntent()
	commitment := NativeSetupCommitment{SchemaVersion: 1, OperationID: binding.Source.OperationID, ReleaseSHA256: admissionDigest(nativeBootJSON(binding)), TokenSHA256: admissionDigest([]byte("synthetic setup digest input; never a usable issued token"))}
	if withSetup {
		data := nativeBootJSON(commitment)
		intent.SetupSHA256 = admissionDigest(data)
		writeStageFixture(t, DefaultJournalDirectory, nativeSetupName, data, 0600)
	}
	digest, err := intent.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifest := NativeReleaseManifest{SchemaVersion: 1, Release: binding, Host: review.Recovery.Snapshot.Host, BootSHA256: digest}
	plan, err := manifest.Plan()
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic journal transitions exercise real store validation, not conversion.
	state := storageprep.State{SchemaVersion: 1, Plan: plan, Phase: storageprep.Planned}
	for _, phase := range []storageprep.Phase{storageprep.Planned, storageprep.Arming, storageprep.AwaitingReboot, storageprep.Verifying, storageprep.Ready} {
		state.Phase = phase
		if phase != storageprep.Planned {
			state.ArmAttempts = 1
		}
		if phase == storageprep.Verifying || phase == storageprep.Ready {
			state.ResumeBootID = "22222222-2222-4222-8222-222222222222"
		}
		if err := stage.journal.Save(state); err != nil {
			t.Fatal(err)
		}
	}
	writeStageFixture(t, DefaultJournalDirectory, nativeRuntimeName, nativeBootJSON(intent), 0600)
	return manifest, intent, commitment
}

func setupWriteUnsafeRecord(t *testing.T, name string, data []byte, scenario string) {
	t.Helper()
	path := filepath.Join(DefaultJournalDirectory, name)
	var err error
	switch scenario {
	case "missing":
		return
	case "symlink":
		err = os.Symlink("missing", path)
	case "fifo":
		err = unix.Mkfifo(path, 0600)
	case "directory":
		err = os.Mkdir(path, 0700)
	default:
		switch scenario {
		case "truncated":
			data = []byte("{")
		case "duplicate":
			data = append([]byte("{\"schemaVersion\":1,"), data[1:]...)
		case "unknown":
			data = append([]byte("{\"token\":\"sfb_synthetic_secret_never_valid\","), data[1:]...)
		case "changed":
			data = bytes.ReplaceAll(data, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 2`))
		}
		err = os.WriteFile(path, data, 0600)
		if err != nil {
			t.Fatal(err)
		}
		switch scenario {
		case "hardlink":
			err = os.Link(path, path+".link")
		case "mode":
			err = os.Chmod(path, 0644)
		case "foreign-owner":
			err = os.Chown(path, 65534, 65534)
		case "oversized":
			err = os.Truncate(path, (64<<10)+1)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
}
