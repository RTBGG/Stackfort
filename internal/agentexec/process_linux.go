// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package agentexec

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"golang.org/x/sys/unix"
)

func configureProcess(command *exec.Cmd, identity *hostingidentity.Spec) error {
	command.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
	if identity != nil {
		command.SysProcAttr.Credential = &syscall.Credential{
			Uid: identity.UID, Gid: identity.GID, Groups: []uint32{identity.GID},
		}
	}
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
