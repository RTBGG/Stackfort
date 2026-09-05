// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration && linux

package updateapply

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/buildinfo"
	"github.com/RTBGG/stackfort/internal/installapply"
)

// This test driver is built with the PRIOR release's production Go source and
// build version. It is never shipped in a release or reachable from the API.
func TestDisposableHostUpgradeMatrix(t *testing.T) {
	if os.Getenv("STACKFORT_DISPOSABLE_HOST_TEST") != "1" || os.Getenv("STACKFORT_UPGRADE_ROOT") == "" {
		t.Skip("requires explicit disposable host upgrade inputs")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root on a disposable host")
	}
	root := os.Getenv("STACKFORT_UPGRADE_ROOT")
	if !filepath.IsAbs(root) || !strings.HasPrefix(root, "/var/tmp/stackfort-upgrade.") {
		t.Fatal("invalid private qualification root")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatal("qualification root must be private")
	}
	from, to := os.Getenv("STACKFORT_UPGRADE_FROM"), os.Getenv("STACKFORT_UPGRADE_TO")
	comparison, err := CompareVersions(from, to)
	if err != nil || comparison >= 0 || buildinfo.Current().Version != from {
		t.Fatal("driver must be built from and versioned as the prior release")
	}
	current := inspectMatrixSource(t, root, "from", from)
	target := inspectMatrixSource(t, root, "to", to)
	commit, err := os.ReadFile(filepath.Join(current.Root, "COMMIT"))
	if err != nil || strings.TrimSpace(string(commit)) != buildinfo.Current().Commit {
		t.Fatal("qualification driver commit differs from the installed prior release")
	}
	runner, err := NewLinuxRunner(os.Stdout, target)
	if err != nil {
		t.Fatal(err)
	}
	journalStore := NewFileStore()
	if os.Getenv("STACKFORT_UPGRADE_CRASH_CHILD") == "1" {
		lock, err := journalStore.AcquireLock()
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := runner.Preflight(t.Context(), current, target); err != nil {
			t.Fatal(err)
		}
		for _, stage := range orderedStages[:indexOfOrderedStage(StageStartServices)] {
			if err := runner.Apply(t.Context(), stage, current, target); err != nil {
				t.Fatal(err)
			}
			if err := runner.Verify(t.Context(), stage, current, target); err != nil {
				t.Fatal(err)
			}
			if err := journalStore.Save(qualificationJournal(current, target, stage)); err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(86) // Model process loss after durable migration, without cleanup.
	}
	installer, err := installapply.NewLinuxRunner(os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	installStore := installapply.NewFileStore()
	installLock, err := installStore.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	installEngine, err := installapply.NewEngine(installStore, installer)
	if err != nil {
		t.Fatal(err)
	}
	result, installErr := installEngine.Install(t.Context(), current)
	_ = installLock.Close()
	if installErr != nil || result.Status != installapply.InstallComplete {
		t.Fatalf("baseline install: %#v %v", result, installErr)
	}
	if os.Getenv("STACKFORT_UPGRADE_KIND") == "release-candidate" {
		stager, err := NewStager()
		if err != nil {
			t.Fatal(err)
		}
		verified, err := stager.Prepare(t.Context(), from)
		if err != nil || verified.ArchiveSHA256 != os.Getenv("STACKFORT_UPGRADE_FROM_SHA256") || verified.Source.Digest != current.Digest {
			t.Fatalf("published predecessor provenance verification failed: %v", err)
		}
	}
	seedMatrixData(t, runner)
	secretPaths := []string{"/var/lib/stackfort/master.key", "/etc/stackfort/panel-tls/bootstrap.pem", "/var/lib/stackfort-phpmyadmin/blowfish.key"}
	secrets := map[string][32]byte{}
	for _, path := range secretPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		secrets[path] = sha256.Sum256(content)
	}
	for _, scenario := range []string{"health-rollback", "interrupted-recovery", "success"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "interrupted-recovery" {
				child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDisposableHostUpgradeMatrix$", "-test.v", "-test.timeout=15m")
				child.Env = append(os.Environ(), "STACKFORT_UPGRADE_CRASH_CHILD=1")
				child.Stdout, child.Stderr = os.Stdout, os.Stderr
				var exit *exec.ExitError
				if err := child.Run(); !errors.As(err, &exit) || exit.ExitCode() != 86 {
					t.Fatalf("interrupted subprocess did not reach migration: %v", err)
				}
			}
			lock, err := journalStore.AcquireLock()
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			actual, err := NewLinuxRunner(os.Stdout, target)
			if err != nil {
				t.Fatal(err)
			}
			var stageRunner Runner = actual
			if scenario == "health-rollback" {
				stageRunner = matrixHealthFailure{actual}
			}
			engine, err := NewEngine(journalStore, stageRunner)
			if err != nil {
				t.Fatal(err)
			}
			result, applyErr := engine.Apply(t.Context(), current, target)
			wanted, active := StatusRolledBack, current
			if scenario == "success" {
				wanted, active = StatusComplete, target
			}
			if result.Status != wanted || (scenario == "success") != (applyErr == nil) || result.Recovered != (scenario == "interrupted-recovery") {
				t.Fatalf("result=%#v err=%v", result, applyErr)
			}
			if err := installer.VerifyInstallation(t.Context(), active); err != nil {
				t.Fatal(err)
			}
			journal, exists, err := journalStore.Load()
			if err != nil || !exists || journal.Status != wanted || ValidateJournal(journal) != nil {
				t.Fatalf("journal=%#v err=%v", journal, err)
			}
			if scenario == "health-rollback" {
				for _, stage := range journal.Stages {
					want := StageComplete
					if stage.ID == StageHealth {
						want = StageFailed
					}
					if stage.Status != want {
						t.Fatalf("failure did not reach the health gate: %s=%s", stage.ID, stage.Status)
					}
				}
			}
			assertMatrixData(t)
			for path, wanted := range secrets {
				content, err := os.ReadFile(path)
				if err != nil || sha256.Sum256(content) != wanted {
					t.Fatalf("secret changed: %s: %v", path, err)
				}
			}
			fmt.Printf("STACKFORT_UPGRADE_CELL %s=passed\n", scenario)
		})
		if t.Failed() {
			return
		}
	}
	receipt := map[string]any{"from": from, "to": to, "sourceArchiveSHA256": os.Getenv("STACKFORT_UPGRADE_FROM_SHA256"), "targetArchiveSHA256": os.Getenv("STACKFORT_UPGRADE_TO_SHA256"), "scenarios": []string{"success", "health-rollback", "interrupted-recovery"}}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("STACKFORT_UPGRADE_RECEIPT %s\n", encoded)
}

func inspectMatrixSource(t *testing.T, root, role, version string) installapply.Source {
	t.Helper()
	archive := filepath.Join(root, role+".tar.gz")
	digest, err := digestFile(archive, maximumArchiveBytes)
	if err != nil || digest != os.Getenv("STACKFORT_UPGRADE_"+strings.ToUpper(role)+"_SHA256") {
		t.Fatalf("%s archive digest mismatch: %v", role, err)
	}
	destination := filepath.Join(root, role)
	if os.Getenv("STACKFORT_UPGRADE_CRASH_CHILD") != "1" {
		if err := os.Mkdir(destination, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := extractReleaseArchive(archive, destination, "stackfort-"+version+"-linux-amd64"); err != nil {
			t.Fatal(err)
		}
	}
	source, err := installapply.InspectSource(filepath.Join(destination, "stackfort-"+version+"-linux-amd64"))
	if err != nil {
		t.Fatal(err)
	}
	if err := installapply.ValidateSourceTrust(source); err != nil {
		t.Fatal(err)
	}
	return source
}

type matrixHealthFailure struct{ *LinuxRunner }

func (runner matrixHealthFailure) Apply(ctx context.Context, stage StageID, current, target installapply.Source) error {
	if stage == StageHealth {
		return errors.New("injected qualification health failure")
	}
	return runner.LinuxRunner.Apply(ctx, stage, current, target)
}

func seedMatrixData(t *testing.T, runner *LinuxRunner) {
	t.Helper()
	if err := runner.stopServices(t.Context()); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", defaultPanelStatePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO stackfort_metadata(key,value,updated_at) VALUES ('upgrade-matrix','Grüße / tenant state','2026-01-01T00:00:00Z')`)
	if closeErr := db.Close(); err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	if err := runner.run(t.Context(), "/usr/bin/systemctl", "start", "stackfort-agent.service", "stackfort-api.service", "stackfort-phpmyadmin.service"); err != nil {
		t.Fatal(err)
	}
}

func assertMatrixData(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+defaultPanelStatePath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow(`SELECT value FROM stackfort_metadata WHERE key='upgrade-matrix'`).Scan(&value); err != nil || value != "Grüße / tenant state" {
		t.Fatalf("tenant state=%q error=%v", value, err)
	}
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&value); err != nil || value != "ok" {
		t.Fatalf("integrity=%q error=%v", value, err)
	}
}
