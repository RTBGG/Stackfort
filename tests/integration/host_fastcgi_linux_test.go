// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux && integration

package integration_test

import (
	"os/exec"
	"testing"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/hostnginx"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

// Existing qualification guests predate the new installer cache path. Apply
// the same fixed directory and narrow SELinux label before exercising it.
func prepareNativeFastCGIRuntime(t *testing.T) {
	t.Helper()
	platform := hostcapabilities.NewInspector().InspectPlatform()
	spec, err := nginxbaseline.ForDistribution(platform.DistributionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostnginx.PrepareCacheRuntime(spec); err != nil {
		t.Fatal(err)
	}
	if err := hostnginx.VerifyCacheRuntime(spec); err != nil {
		t.Fatal(err)
	}
	if platform.DistributionID == "rocky" {
		expression := cacheconfig.FastCGIDirectory + "(/.*)?"
		if err := exec.Command("/usr/sbin/semanage", "fcontext", "-a", "-t", "httpd_cache_t", expression).Run(); err != nil {
			if output, err := exec.Command("/usr/sbin/semanage", "fcontext", "-m", "-t", "httpd_cache_t", expression).CombinedOutput(); err != nil {
				t.Fatalf("label native cache: %v: %s", err, output)
			}
		}
		if output, err := exec.Command("/usr/sbin/restorecon", "-R", cacheconfig.FastCGIDirectory).CombinedOutput(); err != nil {
			t.Fatalf("restore cache context: %v: %s", err, output)
		}
	}
}
