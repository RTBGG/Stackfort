// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPassiveCarrierWrapperForwardsReadOnlyNativeHost(t *testing.T) {
	// Cross-compiled test binaries retain their builder's runtime.Caller path.
	// Qualification may supply the exact copied repository wrapper explicitly;
	// this test-only setting is never read by the production installer.
	fixture := os.Getenv("STACKFORT_TEST_CARRIER_WRAPPER")
	if fixture == "" {
		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			t.Fatal("test source location unavailable; set STACKFORT_TEST_CARRIER_WRAPPER to the copied repository wrapper")
		}
		fixture = filepath.Join(filepath.Dir(filename), "..", "..", "packaging", "core", "stackfort-install.in")
	}
	if !filepath.IsAbs(fixture) || filepath.Clean(fixture) != fixture {
		t.Fatal("wrapper fixture requires a canonical absolute path")
	}
	info, err := os.Lstat(fixture)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 {
		t.Fatal("wrapper fixture must be an existing bounded regular file; provide STACKFORT_TEST_CARRIER_WRAPPER when cross-compiling", err)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("CARRIER_WRAPPER fixture_sha256=%x", sha256.Sum256(data))
	if !strings.Contains(string(data), "preflight | version | panel | native | native-host)") {
		t.Fatal("carrier does not forward read-only native-host")
	}
	if runtime.GOOS != "linux" {
		return // The source contract is still checked on non-Linux builders.
	}
	workspace := t.TempDir()
	root := filepath.Join(workspace, "release")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >\"$STACKFORT_WRAPPER_TEST_RESULT\"\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "stackfort-installer"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	const assignment = "release_root='/usr/lib/stackfort/releases/@STACKFORT_VERSION@'"
	if strings.Count(string(data), assignment) != 1 || strings.Contains(root, "'") {
		t.Fatal("unexpected wrapper or test path")
	}
	wrapper := filepath.Join(workspace, "stackfort-install")
	contents := strings.Replace(string(data), assignment, "release_root='"+root+"'", 1)
	if err := os.WriteFile(wrapper, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		arguments []string
		want      []string
	}{
		{[]string{"native-host", "--format=json"}, []string{"native-host", "--format=json"}},
		{[]string{"native", "status"}, []string{"native", "status"}},
		{[]string{"preflight"}, []string{"preflight"}},
		{[]string{"version"}, []string{"version"}},
		{[]string{"panel", "status"}, []string{"panel", "status"}},
		{[]string{"install", "--yes"}, []string{"install", "--source-dir=" + root, "--yes"}},
		{[]string{"--yes"}, []string{"install", "--source-dir=" + root, "--yes"}},
	} {
		result := filepath.Join(workspace, "result")
		command := exec.Command("/bin/sh", append([]string{wrapper}, scenario.arguments...)...)
		command.Env = append(os.Environ(), "STACKFORT_WRAPPER_TEST_RESULT="+result)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
		actual, err := os.ReadFile(result)
		if err != nil || string(actual) != strings.Join(scenario.want, "\n")+"\n" {
			t.Fatal(scenario.arguments, string(actual), err)
		}
	}
}
