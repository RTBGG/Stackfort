// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginArchiveMustMatchEveryStagedFile(t *testing.T) {
	for _, mode := range []string{"match", "changed", "omitted", "traversal", "symlink", "duplicate", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			root := resumeSourceFixture(t)
			source, err := InspectSource(root)
			if err != nil {
				t.Fatal(err)
			}
			archivePath := filepath.Join(stageTestDirectory(t), "archive.tar.gz")
			file, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(file)
			tarWriter := tar.NewWriter(gz)
			top := "stackfort-" + source.Version + "-linux-amd64/"
			err = filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				relative, err := filepath.Rel(root, name)
				if err != nil {
					return err
				}
				if mode == "omitted" && relative == "README.md" {
					return nil
				}
				content, err := os.ReadFile(name)
				if err != nil {
					return err
				}
				if mode == "changed" && relative == "README.md" {
					content = []byte("altered archive")
				}
				if err := tarWriter.WriteHeader(&tar.Header{Name: top + filepath.ToSlash(relative), Mode: 0644, Size: int64(len(content))}); err != nil {
					return err
				}
				_, err = tarWriter.Write(content)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			extra := &tar.Header{Name: top + "extra", Mode: 0644}
			switch mode {
			case "traversal":
				extra.Name = top + "../escape"
			case "symlink":
				extra.Typeflag, extra.Linkname = tar.TypeSymlink, "/etc/passwd"
			case "duplicate":
				extra.Name = top + "README.md"
			case "oversized":
				extra.Size = maximumSingleFile + 1
			}
			if mode != "match" && mode != "changed" && mode != "omitted" {
				if err := tarWriter.WriteHeader(extra); err != nil {
					t.Fatal(err)
				}
			}
			_ = tarWriter.Close()
			_ = gz.Close()
			_ = file.Close()
			err = archiveMatchesSource(t.Context(), archivePath, source)
			if (mode == "match") != (err == nil) {
				t.Fatalf("%s: %v", mode, err)
			}
		})
	}
}

func TestOriginRejectsUntrustedVerifierLocationWithoutRunningIt(t *testing.T) {
	root := resumeSourceFixture(t)
	policy := OriginPolicy{"tag-release", "0.1.0-beta.3", strings.Repeat("a", 40)}
	if err := verifyOriginCommand(t.Context(), root, stageTestDirectory(t), policy); err == nil {
		t.Fatal(err)
	}
	var output boundedOriginOutput
	if _, err := io.WriteString(&output, strings.Repeat("x", (64<<10)+1)); err == nil {
		t.Fatal("unbounded verifier output")
	}
}
