// SPDX-License-Identifier: AGPL-3.0-or-later

package updateapply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installapply"
)

func TestLoadPreparedRejectsTrailingProvenanceJSON(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "1.2.3")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := provenancePath(directory, "1.2.3")
	content := `{"schema":1,"version":"1.2.3","tag":"v1.2.3","sourceDigest":"` + strings.Repeat("a", 64) +
		`","archiveSHA256":"` + strings.Repeat("b", 64) + `"}` + "\n{}"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stager := &Stager{releasesDirectory: directory, inspectSource: func(string) (installapply.Source, error) {
		return installapply.Source{Root: root, Version: "1.2.3", Digest: strings.Repeat("a", 64)}, nil
	}}
	if _, err := stager.loadPrepared(root, "1.2.3", "v1.2.3", strings.Repeat("b", 64)); err == nil {
		t.Fatal("trailing provenance JSON was accepted")
	}
}
