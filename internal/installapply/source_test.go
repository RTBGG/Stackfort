// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/releaseartifacts"
)

func TestInspectSourceRejectsIncompleteWAFManifestBeforePayloadInspection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := releaseartifacts.Manifest{
		SchemaVersion: releaseartifacts.ManifestSchema,
		Version:       "1.2.3",
		Architecture:  runtime.GOARCH,
		Artifacts:     []releaseartifacts.Artifact{},
	}
	if err := releaseartifacts.WriteManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	_, err := InspectSource(root)
	if err == nil || !strings.Contains(err.Error(), "complete native WAF package matrix") {
		t.Fatalf("incomplete release error = %v", err)
	}
}
