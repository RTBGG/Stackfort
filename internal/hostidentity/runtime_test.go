// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostidentity

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/RTBGG/stackfort/internal/agentexec"
	"github.com/RTBGG/stackfort/internal/hostingoci"
)

func TestExistingRuntimeDirectoryStillStartsUserManager(t *testing.T) {
	t.Parallel()
	identity := testSpec(t)
	// The fake command runner does not apply identity mutations. Match the
	// temporary directory's owner to exercise the ready-directory branch.
	identity.UID, identity.GID = uint32(os.Getuid()), uint32(os.Getgid())
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	host := newFakeHost(identity)
	manager := &linuxRuntimeManager{commands: host}
	spec := hostingoci.Spec{Identity: identity, RuntimeRoot: directory}
	if err := manager.ensureUserRuntime(t.Context(), spec); err == nil {
		t.Fatal("accepted a user manager without its D-Bus socket")
	}
	if !slices.Equal(host.profiles, []agentexec.ProfileID{agentexec.ProfileStartUserManager}) {
		t.Fatal("a directory was mistaken for a ready user session", host.profiles)
	}
	bus, err := net.Listen("unix", filepath.Join(directory, "bus"))
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	if err := manager.ensureUserRuntime(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	host.commandErr = errors.New("user manager failed")
	if err := manager.ensureUserRuntime(t.Context(), spec); err == nil {
		t.Fatal("accepted a runtime with a failed user manager")
	}
}

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
	spec := hostingoci.Spec{Identity: testSpec(t)}
	if err := os.Symlink("target", filepath.Join(directory, spec.Identity.Username)); err != nil {
		t.Fatal(err)
	}
	manager := &linuxRuntimeManager{linger: directory}
	_, err := manager.lingerEnabled(spec)
	if !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("linger symlink error = %v", err)
	}
}
