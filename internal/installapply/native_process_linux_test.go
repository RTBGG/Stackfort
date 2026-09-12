// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package installapply

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeCommandCancellationContainsOrdinaryDescendants(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The shell and sleep deliberately retain the same process group. This tests
	// cancellation, not parent SIGKILL or descendants that call setsid/setpgid.
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 300 & echo $!; wait")
	containNativeCommand(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	type announced struct {
		pid int
		err error
	}
	ready := make(chan announced, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		pid, parseErr := strconv.Atoi(strings.TrimSpace(line))
		ready <- announced{pid, errors.Join(err, parseErr)}
	}()
	var child announced
	select {
	case child = <-ready:
	case <-time.After(5 * time.Second):
		cancel()
		_ = command.Wait()
		t.Fatal("child readiness timed out")
	}
	cancel()
	if err := command.Wait(); err == nil {
		t.Fatal("cancelled process succeeded")
	}
	if child.err != nil || child.pid <= 1 {
		t.Fatal("invalid child readiness", child)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(child.pid) + "/stat")
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		// A zombie has exited; its unrelated parent/reaper owns collection.
		end := strings.LastIndex(string(data), ") ")
		if end < 0 {
			t.Fatal("invalid child process status")
		}
		fields := strings.Fields(string(data)[end+2:])
		if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("ordinary descendant survived cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNativeCommandCancellationContract(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/true")
	containNativeCommand(command)
	if !errors.Is(command.Cancel(), os.ErrProcessDone) || !command.SysProcAttr.Setpgid || command.WaitDelay != 2*time.Second {
		t.Fatal("missing process containment contract")
	}
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
}
