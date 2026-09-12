// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package agentexec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRootProjectQuotaTargetRequiresExactKernelEvidence(t *testing.T) {
	device := unix.Mkdev(8, 1)
	valid := "31 1 8:1 / / rw,relatime - ext4 /dev/sda1 rw,prjquota\n"
	if target, err := rootProjectQuotaTarget(device, valid); err != nil || target != "/" {
		t.Fatal(target, err)
	}
	for _, bad := range []string{"", valid + valid, "malformed\n", strings.Replace(valid, "8:1", "8:2", 1), strings.Replace(valid, "ext4", "xfs", 1), strings.ReplaceAll(valid, ",prjquota", ""), strings.Replace(valid, "/ / rw", "/subtree / rw", 1), strings.Replace(valid, "rw,relatime", "ro,relatime", 1)} {
		if target, err := rootProjectQuotaTarget(device, bad); err == nil {
			t.Fatalf("unsafe root quota target %q for %q", target, bad)
		}
	}
}

func TestProjectQuotaTargetRejectsUntrustedAncestors(t *testing.T) {
	root := t.TempDir()
	// testing.TempDir's child permissions follow the user's umask. The valid
	// fixture must explicitly satisfy the production no-group-write contract.
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	srv := filepath.Join(root, "srv")
	hosting := filepath.Join(srv, "hosting")
	if err := os.MkdirAll(hosting, 0755); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(root, &stat); err != nil {
		t.Fatal(err)
	}
	info := filepath.Join(root, "mountinfo")
	if err := os.WriteFile(info, []byte(fmt.Sprintf("31 1 %d:%d / / rw - ext4 /dev/test rw,prjquota\n", unix.Major(uint64(stat.Dev)), unix.Minor(uint64(stat.Dev)))), 0600); err != nil {
		t.Fatal(err)
	}
	owner := uint32(os.Getuid())
	if target, err := projectQuotaTargetAt(root, info, owner); err != nil || target != "/" {
		for _, path := range []string{root, srv, hosting, info} {
			var actual unix.Stat_t
			if statErr := unix.Stat(path, &actual); statErr == nil {
				t.Logf("fixture %s uid=%d gid=%d mode=%o", filepath.Base(path), actual.Uid, actual.Gid, actual.Mode)
			}
		}
		t.Fatalf("target=%q error=%v expected-owner=%d", target, err, owner)
	}
	if _, err := projectQuotaTargetAt(root, info, owner+1); err == nil {
		t.Fatal("foreign owner accepted")
	}
	if err := os.Chmod(srv, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := projectQuotaTargetAt(root, info, owner); err == nil {
		t.Fatal("writable ancestor accepted")
	}
	if err := os.Chmod(srv, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hosting); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, hosting); err != nil {
		t.Fatal(err)
	}
	if _, err := projectQuotaTargetAt(root, info, owner); err == nil {
		t.Fatal("symlink accepted")
	}
}
