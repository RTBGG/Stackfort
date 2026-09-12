// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"
)

func testBootIntent() NativeRuntimeIntent {
	intent := testRuntimeIntent()
	intent.Profile = NativeBootProfile
	intent.Offline = &NativeBootIntent{SchemaVersion: 1, FstabBefore: "UUID=" + testSourcePin().OperationID + " / ext4 errors=remount-ro 0 1\n", Tools: map[string]string{}}
	intent.Offline.FstabAfter, _ = nativeBootFstab(intent.Offline.FstabBefore, testSourcePin().OperationID, testSourcePin().OperationID)
	for _, path := range nativeBootTools {
		intent.Offline.Tools[path] = strings.Repeat("a", 64)
	}
	intent.BootIntentSHA256, _ = intent.Offline.Digest()
	intent.Ready.FstabSHA256 = admissionDigest([]byte(intent.Offline.FstabAfter))
	return intent
}

func TestNativeBootIntentRejectsMissingForeignAndUnboundFields(t *testing.T) {
	intent := testBootIntent()
	if _, err := intent.Digest(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*NativeRuntimeIntent){
		func(i *NativeRuntimeIntent) { i.Offline = nil },
		func(i *NativeRuntimeIntent) { i.Profile = "debian-native-post-ready-qualification-v1" },
		func(i *NativeRuntimeIntent) { i.Offline.FstabBefore += "# drift\n" },
		func(i *NativeRuntimeIntent) { i.Offline.FstabAfter += "# drift\n" },
		func(i *NativeRuntimeIntent) { delete(i.Offline.Tools, "/usr/sbin/e2fsck") },
		func(i *NativeRuntimeIntent) { i.Offline.Tools["/tmp/tool"] = strings.Repeat("a", 64) },
		func(i *NativeRuntimeIntent) { i.Offline.Tools["/usr/sbin/e2fsck"] = strings.Repeat("b", 64) },
	} {
		next := testBootIntent()
		edit(&next)
		if _, err := next.Digest(); err == nil {
			t.Fatal("unbound intent accepted")
		}
	}
}

func TestNativeBootFstabIsNarrowAndPreservesUnrelatedLines(t *testing.T) {
	uuid := testSourcePin().OperationID
	root := "UUID=" + uuid + " / ext4 errors=remount-ro 0 1"
	before := "# comment\n" + root + "\nUUID=other /boot/efi vfat umask=0077 0 1\n"
	cloud := strings.Replace(root, "errors=remount-ro", "rw,discard,errors=remount-ro,x-systemd.growfs", 1)
	if after, err := nativeBootFstab(cloud, uuid, uuid); err != nil || !strings.Contains(after, "x-systemd.growfs,prjquota") {
		t.Fatal("qualified cloud option not preserved", after, err)
	}
	after, err := nativeBootFstab(before, uuid, uuid)
	if err != nil || after != "# comment\nUUID="+uuid+"\t/\text4\terrors=remount-ro,prjquota\t0\t1\nUUID=other /boot/efi vfat umask=0077 0 1\n" {
		t.Fatal(after, err)
	}
	for _, bad := range []string{"", root + "\n" + root, strings.Replace(root, "ext4", "xfs", 1), strings.Replace(root, "UUID=", "LABEL=", 1), root + " extra", strings.Replace(root, " 0 1", " 0 0", 1), root + "\nmalformed", root + "\n/dev/foo /srv/hosting ext4 defaults 0 2", strings.Replace(root, "errors=remount-ro", "prjquota", 1), strings.Replace(root, "errors=remount-ro", "ro", 1), strings.Replace(root, "errors=remount-ro", "noquota", 1), strings.Replace(root, "errors=remount-ro", "noauto", 1), strings.Replace(root, "errors=remount-ro", "x-systemd.automount", 1), after} {
		if _, err := nativeBootFstab(bad, uuid, uuid); err == nil {
			t.Fatal("unsafe fstab accepted", bad)
		}
	}
}

func TestNativeBootArtifactsUseRealInstallerAndOrderedFinalization(t *testing.T) {
	op := testSourcePin().OperationID
	units, err := NativeBootUnits(op)
	if err != nil || len(units) != 5 {
		t.Fatal(units, err)
	}
	if !strings.Contains(units[NativeRuntimeVerifyUnit], "Requires="+NativeBootFinalizeUnit) || !strings.Contains(units[NativeBootFinalizeUnit], " native-boot finalize --operation-id="+op) {
		t.Fatal("missing finalization dependency")
	}
	hook, premount, err := nativeBootScripts(op)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range append([]string{hook, premount}, units[NativeBootFinalizeUnit]) {
		for _, bad := range []string{".test", "-test.", "STACKFORT_DISPOSABLE", "STACKFORT_NATIVE_QUOTA_EARLY", "update-initramfs", " -y ", "Restart="} {
			if strings.Contains(content, bad) {
				t.Fatal("test or retry in boot path", bad)
			}
		}
	}
	if !strings.Contains(premount, "[ -e "+nativeBootMarker+" ]") || !strings.Contains(premount, "while :; do panic") {
		t.Fatal("missing post-write emergency latch")
	}
	if _, _, err := nativeBootScripts(op + "\ncommand"); err == nil {
		t.Fatal("script injection")
	}
	for _, action := range []string{"prepare", "force", "reset", "open", "resume", ""} {
		if (NativeBootRequest{Action: action, OperationID: op}).Validate() == nil {
			t.Fatal(action)
		}
	}
}

func TestNativeBootJSONRejectsAmbiguousRecords(t *testing.T) {
	intent := testBootIntent()
	var target NativeRuntimeIntent
	good := nativeBootJSON(intent)
	if err := nativeBootDecode(good, &target); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{nil, []byte("{}"), append(good, '\n'), []byte(strings.Replace(string(good), "{\n", "{\n  \"unknown\": true,\n", 1)), []byte(strings.Repeat("x", (64<<10)+1))} {
		if nativeBootDecode(data, &target) == nil {
			t.Fatal("ambiguous record accepted")
		}
	}
}
