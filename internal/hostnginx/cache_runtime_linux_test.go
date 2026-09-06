// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package hostnginx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RTBGG/stackfort/internal/cacheconfig"
	"github.com/RTBGG/stackfort/internal/hostcapabilities"
	"github.com/RTBGG/stackfort/internal/nginxbaseline"
)

func TestCacheRuntimeOwnershipIdempotencyAndSymlinkRejection(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root for worker ownership fixture")
	}
	spec, err := nginxbaseline.ForDistribution(hostcapabilities.NewInspector().InspectPlatform().DistributionID)
	if err != nil {
		t.Skip("requires a supported distribution worker identity")
	}
	manager := &linuxConfigurationManager{root: t.TempDir()}
	if err := os.MkdirAll(manager.rooted("/var/cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := manager.cacheRuntime(spec, true)
	if err != nil || !changed {
		t.Fatalf("create = %v, %v", changed, err)
	}
	changed, err = manager.cacheRuntime(spec, true)
	if err != nil || changed {
		t.Fatalf("replay = %v, %v", changed, err)
	}
	path := manager.rooted(cacheconfig.FastCGIDirectory)
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.cacheRuntime(spec, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted unsafe permissions: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(manager.root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.cacheRuntime(spec, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("followed cache symlink: %v", err)
	}
	if info, err := os.Stat(outside); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("changed symlink target")
	}
}
