// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/installpreflight"
	"github.com/RTBGG/stackfort/internal/storageprep"
)

func TestNativeHostPackageInventoryAndConflicts(t *testing.T) {
	valid := "apt\tii \t3.0.3\nlibedit2:amd64\tii \t3.1-1+b1\nnginx\trc \t1.2\n"
	packages, err := nativePackageInventory(valid)
	if err != nil || nativeInstalled(packages, "libedit2") != "3.1-1+b1" || !nativeConflictingPackage("nginx") {
		t.Fatal(packages, err)
	}
	for _, bad := range []string{"", valid + valid, strings.Replace(valid, "ii ", "hi ", 1), strings.Replace(valid, "ii ", "iU ", 1), strings.Replace(valid, "amd64", "i386", 1), "apt\tii \tinvalid\n", "apt ii 1.0\n"} {
		if _, err := nativePackageInventory(bad); err == nil {
			t.Fatal("invalid inventory accepted", bad)
		}
	}
	for _, name := range []string{"apache2-bin", "mariadb-server", "php8.4-fpm", "docker.io", "containerd", "stackfort-api"} {
		if !nativeConflictingPackage(name) {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"podman", "buildah", "libnginx-foo", "quota"} {
		if nativeConflictingPackage(name) {
			t.Fatal(name)
		}
	}
}

func TestNativeHostReservedSockets(t *testing.T) {
	header := "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid\n"
	for _, tcp := range []bool{true, false} {
		for _, address := range []string{"0100007F", "00000000000000000000000001000000"} {
			for _, port := range []string{"0050", "01BB", "20FB"} {
				row := header + "0: " + address + ":" + port + " 00000000:0000 0A 0 0 0 0 0 0\n"
				if nativeSocketConflicts(row, tcp) == nil {
					t.Fatal("reserved listener accepted", row)
				}
				if nativeSocketConflicts(strings.Replace(row, " 0A ", " 01 ", 1), tcp) == nil && !tcp {
					t.Fatal("bound UDP accepted")
				}
			}
		}
	}
	if nativeSocketConflicts(header, true) != nil || nativeSocketConflicts("unknown", true) == nil {
		t.Fatal("socket inventory policy")
	}
}

func TestNativePrerequisiteAPTPlanAndActualGuard(t *testing.T) {
	simulation := "Inst quota (4.09-1+b1 Debian:13 [amd64])\nConf quota (4.09-1+b1 Debian:13 [amd64])\n"
	plan, err := nativeAPTPlan(simulation, []string{"quota"})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{simulation + "Remv apt [3.0]\n", strings.Replace(simulation, "quota (", "quota [4.08] (", 1), strings.ReplaceAll(simulation, "quota", "linux-image-amd64"), simulation + simulation, ""} {
		if _, err := nativeAPTPlan(bad, []string{"quota"}); err == nil {
			t.Fatal("unsafe simulation accepted", bad)
		}
	}
	transaction := "VERSION 2\nAPT::Architecture=amd64\n\nquota - < 4.09-1+b1 /var/cache/apt/archives/quota_4.09-1+b1_amd64.deb\nquota - < 4.09-1+b1 **CONFIGURE**\n"
	if err := checkNativeAPTTransaction(transaction, plan); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(transaction, "VERSION 2", "VERSION 1", 1), strings.ReplaceAll(transaction, " - < ", " 4.08 < "), strings.ReplaceAll(transaction, " - < ", " 4.09-1+b1 = "), strings.ReplaceAll(transaction, "4.09-1+b1", "4.10"), strings.ReplaceAll(transaction, "quota", "nftables"), strings.Replace(transaction, "/var/cache/apt/archives/quota_4.09-1+b1_amd64.deb", "**REMOVE**", 1), "VERSION 2\n\nquota - < 4.09-1+b1 **CONFIGURE**\n", transaction + "quota - < 4.09-1+b1 /var/cache/apt/archives/quota.deb\n"} {
		if checkNativeAPTTransaction(bad, plan) == nil {
			t.Fatal("unsafe transaction accepted", bad)
		}
	}
}

func testPrerequisiteRecord() nativePrerequisiteRecord {
	op := testSourcePin().OperationID
	snapshot := NativeHostSnapshot{SecureBoot: "enabled", Host: storageprep.Observation{MachineID: op, RootUUID: op, PartitionUUID: op, BootID: op, Kernel: "6.12.1-amd64"}, Features: "has_journal extent", Blocks: "10000000", BlockSize: "4096", InodeSize: "256", PackagesSHA256: strings.Repeat("a", 64), BootArtifacts: map[string]string{}}
	for _, path := range []string{"/etc/fstab", "/boot/grub/grub.cfg", "/boot/vmlinuz-" + snapshot.Host.Kernel, "/boot/initrd.img-" + snapshot.Host.Kernel} {
		snapshot.BootArtifacts[path] = strings.Repeat("b", 64)
	}
	return nativePrerequisiteRecord{SchemaVersion: 1, OperationID: op, ReleaseSHA256: strings.Repeat("c", 64), InstallerSHA256: strings.Repeat("d", 64), Phase: "checking", Before: snapshot, Planned: map[string]string{}}
}

func TestNativePrerequisiteRecordAndReport(t *testing.T) {
	if err := testPrerequisiteRecord().validate(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*nativePrerequisiteRecord){
		func(r *nativePrerequisiteRecord) { r.Phase = "retry" },
		func(r *nativePrerequisiteRecord) { r.Phase = "complete" },
		func(r *nativePrerequisiteRecord) { r.Phase = "applying" },
		func(r *nativePrerequisiteRecord) { r.Before.SecureBoot = "unknown" },
		func(r *nativePrerequisiteRecord) { r.Before.Host.RootUUID = "other" },
		func(r *nativePrerequisiteRecord) { r.Planned = map[string]string{"apt": "3.0"} },
		func(r *nativePrerequisiteRecord) {
			delete(r.Before.BootArtifacts, "/etc/fstab")
			r.Before.BootArtifacts["/other"] = strings.Repeat("b", 64)
		},
		func(r *nativePrerequisiteRecord) {
			after := r.Before
			after.Host.BootID = "different"
			r.After = &after
			r.Phase = "complete"
		},
	} {
		record := testPrerequisiteRecord()
		edit(&record)
		if record.validate() == nil {
			t.Fatal("invalid receipt accepted", record)
		}
	}
	report := NativeHostReport{MissingPackages: []string{"quota"}, Checks: []installpreflight.Check{{ID: "pass", Status: installpreflight.CheckPass}}}
	report.finish()
	if !report.Eligible || report.PrerequisitesReady || report.PublicActivation {
		t.Fatal(report)
	}
	report.Checks = append(report.Checks, installpreflight.Check{ID: "fail", Status: installpreflight.CheckFail})
	report.finish()
	if report.Eligible || report.failure() == nil {
		t.Fatal(report)
	}
}
