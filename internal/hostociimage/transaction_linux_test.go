// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build linux

package hostociimage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestImageTransactionTraversalSurvivesBrokerUmask(t *testing.T) {
	if os.Getenv("STACKFORT_IMAGE_TRANSACTION_UMASK_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		// #nosec G204 -- exact current test executable and fixed test name; only the child changes its own umask and temporary files.
		command := exec.CommandContext(ctx, self, "-test.run=^TestImageTransactionTraversalSurvivesBrokerUmask$", "-test.v")
		command.Env = append(os.Environ(), "STACKFORT_IMAGE_TRANSACTION_UMASK_CHILD=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated broker umask regression: %v\n%s", err, output)
		}
		return
	}
	for _, mask := range []int{0027, 0077} {
		t.Run(fmt.Sprintf("umask-%04o", mask), func(t *testing.T) {
			unix.Umask(mask) // Isolated child, not the parallel test process.
			root := t.TempDir()
			transaction := filepath.Join(root, "transaction")
			if err := os.Mkdir(transaction, 0700); err != nil {
				t.Fatal(err)
			}
			if err := exposeTransactionDirectory(transaction, uint32(os.Getuid()), uint32(os.Getgid())); err != nil {
				t.Fatal(err)
			}
			var info unix.Stat_t
			if err := unix.Lstat(transaction, &info); err != nil || info.Mode != unix.S_IFDIR|0711 || info.Uid != uint32(os.Getuid()) || info.Gid != uint32(os.Getgid()) {
				t.Fatalf("broker umask removed account traversal: %#v / %v", info, err)
			}
			if err := exposeTransactionDirectory(transaction, uint32(os.Getuid()), uint32(os.Getgid())); err == nil {
				t.Fatal("already exposed directory must not be silently adopted")
			}
			link := filepath.Join(root, "link")
			if err := os.Symlink(transaction, link); err != nil {
				t.Fatal(err)
			}
			if err := exposeTransactionDirectory(link, uint32(os.Getuid()), uint32(os.Getgid())); err == nil {
				t.Fatal("transaction symlink accepted")
			}
		})
	}
}
