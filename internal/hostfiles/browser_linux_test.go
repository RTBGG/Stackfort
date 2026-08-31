// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostfiles

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPageNamesIsBoundedResumableAndOmitsUnsafeNames(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, name := range []string{"z-last", "a-first", "m-middle", "line\nbreak"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cursor := ""
	found := map[string]bool{}
	var omitted uint64
	for page := 0; page < 10; page++ {
		descriptor, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		names, pageOmitted, next, err := pageNames(t.Context(), descriptor, cursor, 1)
		_ = unix.Close(descriptor)
		if err != nil || len(names) > 1 {
			t.Fatalf("page %d names=%#v next=%q err=%v", page, names, next, err)
		}
		for _, name := range names {
			if found[name] {
				t.Fatalf("duplicate resumed name %q", name)
			}
			found[name] = true
		}
		omitted += pageOmitted
		if next == "" {
			break
		}
		cursor = next
	}
	if len(found) != 3 || !found["a-first"] || !found["m-middle"] || !found["z-last"] || omitted != 1 {
		t.Fatalf("found=%#v omitted=%d", found, omitted)
	}
}
