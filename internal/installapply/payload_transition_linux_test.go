// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPayloadTransitionPreservesConflictBoundary(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned payload metadata requires the dedicated disposable CI/host invocation")
	}
	root := t.TempDir()
	old, next, active := filepath.Join(root, "old"), filepath.Join(root, "next"), filepath.Join(root, "active")
	for _, path := range []string{old, next, active} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(base, name, content string) {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(old, "index.html", "old index")
	write(old, "assets/old.js", "old asset")
	write(next, "index.html", "new index")
	write(next, "assets/new.js", "new asset")
	if _, err := deployWebTree(old, active); err != nil {
		t.Fatal(err)
	}
	if _, err := deployWebTree(next, active); err == nil {
		t.Fatal("fresh installer overwrote old payload")
	}
	write(active, "foreign.txt", "unmanaged")
	if _, err := transitionPayloadTree(next, old, active); err == nil {
		t.Fatal("transition removed unmanaged file")
	}
	if err := verifyFile(filepath.Join(active, "index.html"), []byte("old index"), 0, 0, 0o644); err != nil {
		t.Fatal("failed precheck mutated installed payload")
	}
	if err := os.Remove(filepath.Join(active, "foreign.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionPayloadTree(next, old, active); err != nil {
		t.Fatal(err)
	}
	if err := verifyWebTree(next, active); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionPayloadTree(old, next, active); err != nil {
		t.Fatal(err)
	}
	if err := verifyWebTree(old, active); err != nil {
		t.Fatal(err)
	}
	// Recovery accepts an interrupted mix of the two approved trees.
	write(active, "index.html", "new index")
	if _, err := transitionPayloadTree(old, next, active); err != nil {
		t.Fatal(err)
	}
	write(active, "index.html", "tampered")
	if _, err := transitionPayloadTree(next, old, active); err == nil {
		t.Fatal("tampered file accepted")
	}
	write(active, "index.html", "old index")
	if err := os.Remove(filepath.Join(active, "assets/old.js")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(old, "assets/old.js"), filepath.Join(active, "assets/old.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionPayloadTree(next, old, active); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestPayloadTransitionReplacesOnlyApprovedExecutable(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned payload fixture")
	}
	root := t.TempDir()
	old, next, active := filepath.Join(root, "old"), filepath.Join(root, "next"), filepath.Join(root, "active")
	for path, content := range map[string]string{old: "old binary", next: "new binary", active: "old binary"} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := copySourceFile(next, active, 0o755); err == nil {
		t.Fatal("fresh installation replaced existing binary")
	}
	if _, err := transitionPayloadFile(next, old, active, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionPayloadFile(old, next, active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, []byte("unrecognized"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionPayloadFile(next, old, active, 0o755); err == nil {
		t.Fatal("unrecognized binary replaced")
	}
}
