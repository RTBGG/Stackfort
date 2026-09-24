// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"maps"
	"strings"
	"testing"
)

func TestNativePartitionMetadata(t *testing.T) {
	id := "a7d6a899-01"
	mbr := map[string]string{"TYPE": "ext4", "PART_ENTRY_SCHEME": "dos", "PART_ENTRY_UUID": id,
		"PART_ENTRY_TYPE": "0x83", "PART_ENTRY_FLAGS": "0x80", "PART_ENTRY_NUMBER": "1"}
	if err := nativeValidatePartition(id, mbr, "bios"); err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"2", "3", "4"} {
		next := maps.Clone(mbr)
		next["PART_ENTRY_UUID"], next["PART_ENTRY_NUMBER"] = "a7d6a899-0"+number, number
		if err := nativeValidatePartition(next["PART_ENTRY_UUID"], next, "bios"); err != nil {
			t.Fatal("primary MBR partition", number, err)
		}
	}
	for key, bad := range map[string][]string{
		"TYPE": {"xfs", ""}, "PART_ENTRY_SCHEME": {"gpt", "", "dos\ngpt"},
		"PART_ENTRY_UUID": {"b7d6a899-01", "a7d6a899-02", ""}, "PART_ENTRY_TYPE": {"0x5", "0xf", "0x8e", "0xee", ""},
		"PART_ENTRY_FLAGS": {"", "0x0", "0x81"}, "PART_ENTRY_NUMBER": {"01", "2", "5", ""},
	} {
		for _, value := range bad {
			next := maps.Clone(mbr)
			next[key] = value
			if nativeValidatePartition(id, next, "bios") == nil {
				t.Fatal("accepted metadata drift", key, value)
			}
		}
	}
	for _, firmware := range []string{"enabled", "disabled", "", "unknown"} {
		if nativeValidatePartition(id, mbr, firmware) == nil {
			t.Fatal("accepted MBR outside BIOS", firmware)
		}
	}
	for _, id := range []string{"00000000-01", "a7d6a899-05", "a7d6a899-01/PARTNROFF=1"} {
		if nativeValidatePartition(id, mbr, "bios") == nil {
			t.Fatal("accepted unsafe partition", id)
		}
	}
	gptID := testPrerequisiteRecord().Before.Host.PartitionUUID
	gpt := map[string]string{"TYPE": "ext4", "PART_ENTRY_SCHEME": "gpt", "PART_ENTRY_UUID": gptID}
	for _, firmware := range []string{"bios", "enabled", "disabled"} {
		if err := nativeValidatePartition(gptID, gpt, firmware); err != nil {
			t.Fatal("GPT compatibility", err)
		}
	}
}

func TestMBRPrerequisitesRecoveryAndFstabBindings(t *testing.T) {
	record := testPrerequisiteRecord()
	record.Before.Host.PartitionUUID, record.Before.SecureBoot = "a7d6a899-01", "bios"
	if err := record.validate(); err != nil {
		t.Fatal(err)
	}
	for _, firmware := range []string{"enabled", "disabled", ""} {
		bad := record
		bad.Before.SecureBoot = firmware
		if bad.validate() == nil {
			t.Fatal("unqualified firmware in prerequisite receipt")
		}
	}
	choice := testRecoveryChoice()
	oldDigest := choice.Decision.ReviewedSHA256
	choice.Review.Snapshot = record.Before
	if choice.validate() == nil {
		t.Fatal("GPT consent reused for MBR")
	}
	digest, err := choice.Review.Digest()
	if err != nil || digest == oldDigest {
		t.Fatal("MBR review not bound", err)
	}
	choice.Decision.ReviewedSHA256 = digest
	if err := choice.validate(); err != nil {
		t.Fatal(err)
	}
	choice.Review.Snapshot.Host.PartitionUUID = "a7d6a899-02"
	if choice.validate() == nil {
		t.Fatal("consent reused for different MBR partition")
	}
	before := "PARTUUID=a7d6a899-01 / ext4 errors=remount-ro 0 1\n"
	after, err := nativeBootFstab(before, record.Before.Host.RootUUID, "a7d6a899-01")
	if err != nil || !strings.Contains(after, "PARTUUID=a7d6a899-01\t/\text4\terrors=remount-ro,prjquota") {
		t.Fatal(after, err)
	}
	if _, err := nativeBootFstab(before, record.Before.Host.RootUUID, "a7d6a899-02"); err == nil {
		t.Fatal("foreign root fstab accepted")
	}
}
