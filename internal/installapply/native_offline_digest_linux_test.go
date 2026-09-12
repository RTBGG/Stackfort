// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestNativeOfflineDigestOutputIsBounded(t *testing.T) {
	for _, mode := range []string{"write", "copy"} {
		t.Run(mode, func(t *testing.T) {
			var target bytes.Buffer
			output := &nativeDigestOutput{target: &target, limit: 4}
			if n, err := output.Write([]byte("1234")); err != nil || n != 4 {
				t.Fatal(n, err)
			}
			var err error
			if mode == "copy" {
				_, err = io.Copy(output, bytes.NewReader([]byte("5")))
			} else {
				_, err = output.Write([]byte("5"))
			}
			if err == nil || !output.overflow || output.written != 4 || target.String() != "1234" {
				t.Fatal("overflow bypassed bounded digest", output, target.String(), err)
			}
		})
	}
	var target bytes.Buffer
	output := &nativeDigestOutput{target: &target, limit: 4}
	if n, err := output.Write([]byte("12345")); n != 0 || err == nil || output.written != 0 || target.Len() != 0 {
		t.Fatal("oversized first write was not rejected atomically")
	}
}

func TestNativeOfflineDigestRefusesForeignInputs(t *testing.T) {
	_, manifest, _, _ := testRecoveryBoot(t)
	plan, err := manifest.Plan()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/etc/shadow", "/boot/vmlinuz-foreign", "/boot/grub/../grub/grub.cfg", "/boot/grub/grub.cfg\nwrite", "relative"} {
		if _, err := nativeOfflineArtifactDigest(t.Context(), "/dev/sda1", path, plan); err == nil {
			t.Fatal("foreign path accepted", path)
		}
	}
	for _, device := range []string{"/etc/fstab", "/dev/null", "/dev/disk/by-uuid/foreign", "relative"} {
		if _, err := nativeOfflineArtifactDigest(t.Context(), device, "/boot/grub/grub.cfg", plan); err == nil {
			t.Fatal("foreign target accepted", device)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, invalid := range []context.Context{nil, ctx} {
		if _, err := nativeOfflineArtifactDigest(invalid, "/dev/sda1", "/boot/grub/grub.cfg", plan); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	plan.Kernel = "injected;command"
	if _, err := nativeOfflineArtifactDigest(t.Context(), "/dev/sda1", "/boot/grub/grub.cfg", plan); err == nil {
		t.Fatal("invalid plan accepted")
	}
}
