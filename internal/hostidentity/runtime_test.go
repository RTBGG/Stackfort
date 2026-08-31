// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostidentity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingoci"
)

func TestSubordinateIDParserAndRangeIsolation(t *testing.T) {
	t.Parallel()
	entries, err := parseSubordinateIDs("first:1000000:65536\nsecond:1065536:65536\n")
	if err != nil {
		t.Fatal(err)
	}
	if present, err := inspectSubordinateRange(entries, "first", 1000000, 65536); err != nil || !present {
		t.Fatalf("expected range: present=%t err=%v", present, err)
	}
	if _, err := inspectSubordinateRange(entries, "third", 1000100, 65536); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("overlap error = %v", err)
	}
	if _, err := inspectSubordinateRange(entries, "first", 2000000, 65536); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("alternate same-user range error = %v", err)
	}
}

func TestSubordinateIDParserRejectsAmbiguousInput(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"user:1\n", " user:1:2\n", "user:1:0\n", "user:not-a-number:2\n",
		"user:4294967295:2\n",
	} {
		if _, err := parseSubordinateIDs(value); !errors.Is(err, ErrInvalidDatabase) {
			t.Fatalf("input %q error = %v", value, err)
		}
	}
}

func TestLingerInspectionRejectsSymlink(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Symlink("target", filepath.Join(directory, "managed")); err != nil {
		t.Fatal(err)
	}
	manager := &linuxRuntimeManager{linger: directory}
	_, err := manager.lingerEnabled(hostingoci.Spec{Identity: testSpec(t)})
	if !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("linger symlink error = %v", err)
	}
}
