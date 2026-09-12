// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func operatorApproval() RecoveryApproval {
	return RecoveryApproval{SchemaVersion: 1, ID: "30000000-0000-4000-8000-000000000099",
		Plan: pinPlan(testSourcePin()), Review: AdmissionRecovery{strings.Repeat("a", 64), strings.Repeat("b", 64)}, Status: "pending"}
}

func TestNativeApprovalDurableTransitionsAndCompareBeforeWrite(t *testing.T) {
	for _, finish := range []string{"consumed", "cancelled"} {
		t.Run(finish, func(t *testing.T) {
			root := stageTestDirectory(t)
			stage := testSourceStage(t, root)
			next := operatorApproval()
			if err := stage.saveRecoveryApproval(next, ""); err != nil {
				t.Fatal(err)
			}
			first, hash, err := stage.readRecoveryApproval()
			if err != nil || first == nil || *first != next || len(hash) != 64 {
				t.Fatal(first, hash, err)
			}
			path := filepath.Join(root, recoveryApprovalName)
			before, _ := os.ReadFile(path)
			next.Status = finish
			if stage.saveRecoveryApproval(next, strings.Repeat("0", 64)) == nil {
				t.Fatal("stale CAS accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("stale write modified approval")
			}
			if err := stage.saveRecoveryApproval(next, hash); err != nil {
				t.Fatal(err)
			}
			_ = stage.Close()
			stage = testSourceStage(t, root)
			record, hash, err := stage.readRecoveryApproval()
			if err != nil || record.Status != finish {
				t.Fatal(record, err)
			}
			next.Status = "pending"
			if stage.saveRecoveryApproval(next, hash) == nil {
				t.Fatal("same identity replayed")
			}
			next.ID = "30000000-0000-4000-8000-000000000098"
			if err := stage.saveRecoveryApproval(next, hash); err != nil {
				t.Fatal(err)
			}
			_, hash, _ = stage.readRecoveryApproval()
			next.Plan.OperationID = "30000000-0000-4000-8000-000000000097"
			next.Status = "cancelled"
			if stage.saveRecoveryApproval(next, hash) == nil {
				t.Fatal("plan rebound")
			}
		})
	}
}

func TestNativeApprovalUnsafeRecordsRetained(t *testing.T) {
	for _, kind := range []string{"truncated", "duplicate", "unknown", "mode", "symlink", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := stageTestDirectory(t)
			stage := testSourceStage(t, root)
			record := operatorApproval()
			if err := stage.saveRecoveryApproval(record, ""); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, recoveryApprovalName)
			data, _ := os.ReadFile(path)
			switch kind {
			case "truncated":
				if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				if err := os.WriteFile(path, append([]byte("{\"schemaVersion\":1,"), data[1:]...), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				if err := os.WriteFile(path, append([]byte("{\"extra\":true,"), data[1:]...), 0600); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "symlink", "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink("missing", path); err != nil {
						t.Fatal(err)
					}
				} else if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := stage.readRecoveryApproval(); err == nil {
				t.Fatal("unsafe record accepted")
			}
			if stage.saveRecoveryApproval(record, "") == nil {
				t.Fatal("unsafe record overwritten")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("evidence removed")
			}
		})
	}
}
