// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package agentexec

import (
	"errors"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func configureProcess(command *exec.Cmd) error {
	command.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return terminateProcessTree(command) }
	return nil
}

func terminateProcessTree(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return os.ErrProcessDone
	}
	err := unix.Kill(-command.Process.Pid, unix.SIGKILL)
	if errors.Is(err, unix.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
