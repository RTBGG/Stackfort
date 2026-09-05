// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationGateRequiresInventoryEvenForFirstRelease(t *testing.T) {
	root := t.TempDir()
	catalog := filepath.Join(root, "catalog.json")
	published := filepath.Join(root, "published.json")
	if err := os.WriteFile(catalog, []byte(`{"schemaVersion":1,"releases":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(published, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--catalog", catalog, "--target", "0.1.0-beta.1", "--target-sha256", strings.Repeat("a", 64), "--verify"}
	if err := run(args, new(bytes.Buffer)); err == nil {
		t.Fatal("missing published inventory accepted")
	}
	if err := run(append(args, "--published", published), new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(published, []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(append(args, "--published", published), new(bytes.Buffer)); err == nil {
		t.Fatal("null inventory accepted as first release")
	}
}
