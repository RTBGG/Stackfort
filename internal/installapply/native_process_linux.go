// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Cancel the owned process group, not only the immediate APT/mkinitramfs child.
// This contains ordinary descendants on timeout/cancellation. It does not replace
// a systemd control-group for parent SIGKILL or deliberately detached descendants.
func containNativeCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		// The direct child has not been reaped while cancellation runs, so its
		// process-group identity cannot have been recycled for a foreign process.
		err := unix.Kill(-command.Process.Pid, unix.SIGKILL)
		if errors.Is(err, unix.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
