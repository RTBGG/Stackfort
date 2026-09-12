// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func originArchiveFixture(t *testing.T) (Source, []byte) {
	t.Helper()
	root := resumeSourceFixture(t)
	source, err := InspectSource(root)
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	writer := tar.NewWriter(&raw)
	if err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if err := writer.WriteHeader(&tar.Header{Name: "stackfort-" + source.Version + "-linux-amd64/" + filepath.ToSlash(relative), Mode: 0o644, Size: int64(len(content))}); err != nil {
			return err
		}
		_, err = writer.Write(content)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return source, raw.Bytes()
}

func originCompressFixture(t *testing.T, raw []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func TestOriginArchiveGlobalBudgetAndCompleteGzipStream(t *testing.T) {
	source, raw := originArchiveFixture(t)
	valid := originCompressFixture(t, raw)
	corruptCRC := bytes.Clone(valid)
	corruptCRC[len(corruptCRC)-8] ^= 0xff
	for _, test := range []struct {
		name  string
		data  []byte
		limit int64
		valid bool
	}{
		{"exact-budget", valid, int64(len(raw)), true},
		{"ordinary-zero-record-padding", originCompressFixture(t, append(bytes.Clone(raw), make([]byte, 10240)...)), int64(len(raw) + 10240), true},
		{"budget-exhausted", valid, int64(len(raw) - 1), false},
		{"padding-bomb", originCompressFixture(t, append(bytes.Clone(raw), make([]byte, 32768)...)), int64(len(raw)), false},
		{"nonzero-tar-suffix", originCompressFixture(t, append(bytes.Clone(raw), 'x')), int64(len(raw) + 1), false},
		{"invalid-gzip-crc", corruptCRC, maximumOriginTarBytes, false},
		{"truncated-gzip-footer", valid[:len(valid)-4], maximumOriginTarBytes, false},
		{"concatenated-gzip-member", append(bytes.Clone(valid), valid...), maximumOriginTarBytes, false},
		{"trailing-compressed-byte", append(bytes.Clone(valid), 0), maximumOriginTarBytes, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := matchOriginArchive(t.Context(), bytes.NewReader(test.data), source, test.limit)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t, error=%v", test.valid, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := matchOriginArchive(ctx, bytes.NewReader(valid), source, maximumOriginTarBytes); !errors.Is(err, context.Canceled) {
		t.Fatalf("archive cancellation ignored: %v", err)
	}
	if err := matchOriginArchive(nil, bytes.NewReader(valid), source, maximumOriginTarBytes); err == nil {
		t.Fatal("nil context accepted")
	}
	for _, limit := range []int64{0, -1, maximumOriginTarBytes + 1} {
		if err := matchOriginArchive(t.Context(), bytes.NewReader(valid), source, limit); err == nil {
			t.Fatal("invalid decompression limit accepted")
		}
	}
}

func TestOriginArchiveHiddenPAXMetadataUsesGlobalBudget(t *testing.T) {
	source, _ := originArchiveFixture(t)
	var raw bytes.Buffer
	writer := tar.NewWriter(&raw)
	// The tar writer emits a hidden PAX record for this long name. Next must
	// consume that metadata before returning a normal Header to the verifier.
	if err := writer.WriteHeader(&tar.Header{Name: strings.Repeat("a", 8192), Mode: 0o644, Format: tar.FormatPAX}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	err := matchOriginArchive(t.Context(), bytes.NewReader(originCompressFixture(t, raw.Bytes())), source, 2048)
	if err == nil || strings.Contains(err.Error(), "unsafe or duplicate archive member") {
		t.Fatalf("hidden metadata was not rejected before returning the oversized Header: %v", err)
	}
}

func TestOriginArchiveOpeningRejectsUnsafeMetadata(t *testing.T) {
	source, raw := originArchiveFixture(t)
	valid := originCompressFixture(t, raw)
	for _, mode := range []string{"symlink", "hardlink", "writable", "nonroot", "fifo", "parent-symlink", "relative", "noncanonical"} {
		t.Run(mode, func(t *testing.T) {
			dir := stageTestDirectory(t)
			archive := filepath.Join(dir, "archive.tar.gz")
			if err := os.WriteFile(archive, valid, 0o600); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "symlink":
				alias := filepath.Join(dir, "alias")
				if err := os.Symlink(archive, alias); err != nil {
					t.Fatal(err)
				}
				archive = alias
			case "hardlink":
				if err := os.Link(archive, filepath.Join(dir, "alias")); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(archive, 0o666); err != nil {
					t.Fatal(err)
				}
			case "nonroot":
				if err := os.Chown(archive, 12345, 12345); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				archive = filepath.Join(dir, "fifo")
				if err := unix.Mkfifo(archive, 0o600); err != nil {
					t.Fatal(err)
				}
			case "parent-symlink":
				alias := filepath.Join(dir, "alias")
				if err := os.Symlink(dir, alias); err != nil {
					t.Fatal(err)
				}
				archive = filepath.Join(alias, "archive.tar.gz")
			case "relative":
				archive = "archive.tar.gz"
			case "noncanonical":
				archive = dir + "/./archive.tar.gz"
			}
			if err := archiveMatchesSource(t.Context(), archive, source); err == nil {
				t.Fatalf("unsafe %s archive accepted", mode)
			}
		})
	}
}
