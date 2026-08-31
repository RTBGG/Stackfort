// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package agentexec

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var (
	helperMode   = flag.String("stackfort-agentexec-helper", "", "internal agentexec test helper mode")
	helperMarker = flag.String("stackfort-agentexec-marker", "", "internal agentexec test helper marker")
)

const helperProfile ProfileID = "test.helper"

func TestRunnerSanitizesEnvironmentAndRedactsOutput(t *testing.T) {
	t.Setenv("STACKFORT_PARENT_SECRET", "must-not-be-inherited")
	runner := newHelperRunner(t, 2*time.Second, defaultOutputLimit, true)

	result, err := runner.Run(t.Context(), Invocation{
		Profile: helperProfile, Values: []string{"environment", "must-not-be-inherited"},
	})
	if err != nil {
		t.Fatalf("environment helper: %v", err)
	}
	if strings.Contains(result.Stdout, "STACKFORT_PARENT_SECRET") ||
		strings.Contains(result.Stdout, "must-not-be-inherited") {
		t.Fatalf("parent environment leaked: %q", result.Stdout)
	}
	for _, item := range sanitizedEnvironment() {
		if !strings.Contains(result.Stdout, item+"\n") {
			t.Fatalf("sanitized environment is missing %q: %q", item, result.Stdout)
		}
	}

	result, err = runner.Run(t.Context(), Invocation{
		Profile: helperProfile, Values: []string{"echo", "must-not-be-inherited"},
	})
	if err != nil {
		t.Fatalf("echo helper: %v", err)
	}
	if strings.Contains(result.Stdout, "must-not-be-inherited") ||
		strings.Contains(result.Stderr, "must-not-be-inherited") ||
		!strings.Contains(result.Stdout, "[REDACTED]") ||
		!strings.Contains(result.Stderr, "[REDACTED]") {
		t.Fatalf("redacted result = stdout %q, stderr %q", result.Stdout, result.Stderr)
	}
}

func TestRunnerPropagatesCancellationWithoutStartingAHelper(t *testing.T) {
	runner := newHelperRunner(t, 2*time.Second, defaultOutputLimit, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := runner.Run(ctx, Invocation{
		Profile: helperProfile, Values: []string{"echo", "not-started"},
	})
	if !errors.Is(err, ErrCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestTimeoutKillsEntireProcessGroup(t *testing.T) {
	marker := t.TempDir() + "/timeout-child"
	runner := newHelperRunner(t, 750*time.Millisecond, defaultOutputLimit, false)

	_, err := runner.Run(t.Context(), Invocation{
		Profile: helperProfile, Values: []string{"tree-timeout", marker},
	})
	if !errors.Is(err, ErrTimedOut) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	assertHelperChildGone(t, marker)
}

func TestOutputExhaustionKillsEntireProcessGroup(t *testing.T) {
	marker := t.TempDir() + "/output-child"
	runner := newHelperRunner(t, 3*time.Second, 1<<10, false)

	_, err := runner.Run(t.Context(), Invocation{
		Profile: helperProfile, Values: []string{"tree-output", marker},
	})
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("output-limit error = %v", err)
	}
	assertHelperChildGone(t, marker)
}

func TestAgentExecHelperProcess(t *testing.T) {
	if *helperMode == "" {
		return
	}
	switch *helperMode {
	case "environment":
		_, _ = fmt.Fprintln(os.Stdout, strings.Join(os.Environ(), "\n"))
		os.Exit(0)
	case "echo":
		_, _ = fmt.Fprintln(os.Stdout, *helperMarker)
		_, _ = fmt.Fprintln(os.Stderr, *helperMarker)
		os.Exit(0)
	case "tree-timeout", "tree-output":
		startHelperChild()
		waitForHelperReady(*helperMarker + ".ready")
		if *helperMode == "tree-output" {
			_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), 128<<10))
		}
		time.Sleep(10 * time.Minute)
	case "child":
		content := []byte(strconv.Itoa(os.Getpid()))
		if err := os.WriteFile(*helperMarker+".ready", content, 0o600); err != nil {
			os.Exit(2)
		}
		time.Sleep(1500 * time.Millisecond)
		if err := os.WriteFile(*helperMarker, []byte("orphaned"), 0o600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	default:
		os.Exit(4)
	}
}

func newHelperRunner(t *testing.T, timeout time.Duration, outputLimit int, sensitive bool) *Runner {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	profile := executionProfile{
		executable: executable, timeout: timeout, stdoutLimit: outputLimit,
		stderrLimit: outputLimit, waitDelay: defaultWaitDelay,
		resolve: func(values []string) ([]string, error) {
			if len(values) != 2 || values[0] == "" {
				return nil, ErrInvalidInvocation
			}
			return []string{
				"-test.run=^TestAgentExecHelperProcess$",
				"-stackfort-agentexec-helper=" + values[0],
				"-stackfort-agentexec-marker=" + values[1],
			}, nil
		},
	}
	if sensitive {
		profile.sensitiveInputs = map[int]struct{}{1: {}}
	}
	return &Runner{profiles: map[ProfileID]executionProfile{helperProfile: profile}}
}

func startHelperChild() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(5)
	}
	// #nosec G204 -- this is the current Go test binary with fixed helper flags.
	command := exec.Command(executable,
		"-test.run=^TestAgentExecHelperProcess$",
		"-stackfort-agentexec-helper=child",
		"-stackfort-agentexec-marker="+*helperMarker,
	)
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		os.Exit(6)
	}
}

func waitForHelperReady(path string) {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	os.Exit(7)
}

func assertHelperChildGone(t *testing.T, marker string) {
	t.Helper()
	content, err := os.ReadFile(marker + ".ready")
	if err != nil {
		t.Fatalf("child readiness marker: %v", err)
	}
	pid, err := strconv.Atoi(string(content))
	if err != nil || pid <= 0 {
		t.Fatalf("child PID = %q, error = %v", content, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = unix.Kill(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			break
		}
		if err != nil {
			t.Fatalf("inspect helper child %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper child %d remains after runner returned", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan side-effect marker exists or is unreadable: %v", err)
	}
}
