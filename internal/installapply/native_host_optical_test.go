// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"
)

const opticalFixtureRoot = "UUID=5b5aaec0-39f6-4ff9-a197-a4ab78e33141 / ext4 errors=remount-ro 0 1\n"
const opticalProviderEntries = "/dev/sr1        /media/cdrom0   udf,iso9660 user,noauto     0       0\n/dev/sr0        /media/cdrom1   udf,iso9660 user,noauto     0       0\n"
const opticalMountRoot = "22 1 8:1 / / rw,relatime - ext4 /dev/sda1 rw,errors=remount-ro\n"

func TestNativeHostOptionalOpticalProviderFstab(t *testing.T) {
	before := opticalFixtureRoot + opticalProviderEntries
	entries, err := nativeHostFstabPolicy(before)
	if err != nil || len(entries) != 2 || entries[0].source != "/dev/sr1" || entries[1].target != "/media/cdrom1" {
		t.Fatal(entries, err)
	}
	if err := nativeOpticalUnmounted(entries, opticalMountRoot); err != nil {
		t.Fatal(err)
	}
	after, err := nativeBootFstab(before, "5b5aaec0-39f6-4ff9-a197-a4ab78e33141", "a7d6a899-01")
	if err != nil || !strings.HasSuffix(after, opticalProviderEntries) ||
		!strings.Contains(after, "errors=remount-ro,prjquota") {
		t.Fatal("quota transformation did not preserve the original optical lines", err)
	}
	for _, row := range []string{
		"/dev/sr0 /media/cdrom iso9660 ro,noauto 0 0",
		"/dev/sr12 /media/cdrom3 udf noauto,users,nofail,nodev,nosuid,noexec 0 0",
		"/dev/sr1 /media/cdrom1 iso9660,udf noauto,user 0 0 # optional installation media",
	} {
		if entries, err := nativeHostFstabPolicy(opticalFixtureRoot + row + "\n"); err != nil || len(entries) != 1 {
			t.Fatal(row, entries, err)
		}
	}
}

func TestNativeHostOptionalOpticalRejectsPolicyExpansion(t *testing.T) {
	base := "/dev/sr0 /media/cdrom0 udf,iso9660 user,noauto 0 0\n"
	cases := map[string]string{
		"missing-noauto":      strings.ReplaceAll(base, "user,noauto", "user"),
		"nofail-not-noauto":   strings.ReplaceAll(base, "user,noauto", "user,nofail"),
		"auto-after":          strings.ReplaceAll(base, "noauto", "noauto,auto"),
		"auto-before":         strings.ReplaceAll(base, "noauto", "auto,noauto"),
		"defaults":            strings.ReplaceAll(base, "noauto", "noauto,defaults"),
		"automount":           strings.ReplaceAll(base, "noauto", "noauto,x-systemd.automount"),
		"required-by":         strings.ReplaceAll(base, "noauto", "noauto,x-systemd.required-by=local-fs.target"),
		"initrd":              strings.ReplaceAll(base, "noauto", "noauto,x-initrd.mount"),
		"bind":                strings.ReplaceAll(base, "noauto", "noauto,bind"),
		"rbind":               strings.ReplaceAll(base, "noauto", "noauto,rbind"),
		"loop":                strings.ReplaceAll(base, "noauto", "noauto,loop"),
		"quota":               strings.ReplaceAll(base, "noauto", "noauto,prjquota"),
		"empty-option":        strings.ReplaceAll(base, "noauto", "noauto,"),
		"duplicate-option":    strings.ReplaceAll(base, "noauto", "noauto,noauto"),
		"ambiguous-users":     strings.ReplaceAll(base, "user", "user,users"),
		"source-alias":        strings.ReplaceAll(base, "/dev/sr0", "/dev/cdrom"),
		"regular-device":      strings.ReplaceAll(base, "/dev/sr0", "/dev/sda2"),
		"network-source":      strings.ReplaceAll(base, "/dev/sr0", "server:/media"),
		"source-traversal":    strings.ReplaceAll(base, "/dev/sr0", "/dev/../dev/sr0"),
		"source-leading-zero": strings.ReplaceAll(base, "/dev/sr0", "/dev/sr00"),
		"target-boot":         strings.ReplaceAll(base, "/media/cdrom0", "/boot"),
		"target-var":          strings.ReplaceAll(base, "/media/cdrom0", "/var"),
		"target-data":         strings.ReplaceAll(base, "/media/cdrom0", "/srv/data"),
		"target-traversal":    strings.ReplaceAll(base, "/media/cdrom0", "/media/../boot"),
		"target-suffix":       strings.ReplaceAll(base, "/media/cdrom0", "/media/cdrom0/data"),
		"target-escaped":      strings.ReplaceAll(base, "/media/cdrom0", "/media/cdrom\\060"),
		"ext4-noauto":         strings.ReplaceAll(base, "udf,iso9660", "ext4"),
		"auto-type":           strings.ReplaceAll(base, "udf,iso9660", "auto"),
		"mixed-type":          strings.ReplaceAll(base, "udf,iso9660", "udf,ext4"),
		"dump":                strings.ReplaceAll(base, "0 0", "1 0"),
		"fsck":                strings.ReplaceAll(base, "0 0", "0 2"),
		"missing-columns":     strings.ReplaceAll(base, " 0 0", ""),
		"extra-column":        strings.ReplaceAll(base, "0 0", "0 0 extra"),
		"duplicate-target":    base + base,
		"tmpfs-parent":        base + "tmpfs /media tmpfs defaults 0 0\n",
		"tmpfs-child":         base + "tmpfs /media/cdrom0/sub tmpfs defaults 0 0\n",
		"real-extra-storage":  base + "UUID=foreign /data ext4 noauto 0 2\n",
		"malformed":           "broken\n",
		"nul":                 base + "\x00",
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := nativeHostFstabPolicy(opticalFixtureRoot + row); err == nil {
				t.Fatal("unsupported optical/storage policy accepted")
			}
		})
	}
	if _, err := nativeHostFstabPolicy(opticalFixtureRoot + "secret:password /private nfs noauto 0 0\n"); err == nil || strings.Contains(err.Error(), "password") || !strings.Contains(err.Error(), "line 2") {
		t.Fatal("diagnostic must identify the line without exposing source credentials", err)
	}
}

func TestNativeHostOptionalOpticalMountState(t *testing.T) {
	entries, err := nativeHostFstabPolicy(opticalFixtureRoot + opticalProviderEntries)
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{
		"33 22 11:0 / /mnt/cd ro - iso9660 /dev/sr0 ro\n",
		"33 22 11:1 / /mnt/cd rw - udf /dev/cdrom rw\n",
		"33 22 0:20 / /media/cdrom0 rw - autofs systemd-1 rw\n",
		"33 22 0:20 / /media rw - tmpfs tmpfs rw\n",
		"33 22 0:20 / /media/cdrom1/sub rw - tmpfs tmpfs rw\n",
		"33 22 8:1 /boot /media/cdrom0 rw - ext4 /dev/sda1 rw\n",
		"33 22 8:2 / /elsewhere ro - ext4 /dev/sr1 ro\n",
		"33 22 0:20 / /media/cdrom0 rw - tmpfs tmpfs rw\n34 33 0:21 / /media/cdrom0 rw - tmpfs tmpfs rw\n",
		"malformed\n", "33 22 0:20 / /media/\\057bad rw - tmpfs tmpfs rw\n",
		"33 22 0:20 / /media/../boot rw - tmpfs tmpfs rw\n",
	} {
		if err := nativeOpticalUnmounted(entries, opticalMountRoot+extra); err == nil {
			t.Fatal("mounted or malformed optical inventory accepted", extra)
		}
	}
	for _, input := range []string{"", "33 22 0:20 / /run rw - tmpfs tmpfs rw\n", strings.Repeat("x", 256<<10+1)} {
		if nativeOpticalUnmounted(entries, input) == nil {
			t.Fatal("incomplete/oversize inventory accepted")
		}
	}
	extra := "33 22 0:20 / /media/cdrom10 rw - tmpfs tmpfs rw\n34 22 0:21 / /unrelated\\040name rw - tmpfs tmpfs rw\n"
	if err := nativeOpticalUnmounted(entries, opticalMountRoot+extra); err != nil {
		t.Fatal("unrelated mount or prefix neighbor rejected", err)
	}
}

func TestNativeHostFstabExistingPolicyPreserved(t *testing.T) {
	for _, row := range []string{"", "UUID=efi /boot/efi vfat umask=0077 0 1\n", "/swapfile none swap sw 0 0\n", "tmpfs /tmp tmpfs defaults 0 0\n"} {
		entries, err := nativeHostFstabPolicy(opticalFixtureRoot + row)
		if err != nil || len(entries) != 0 {
			t.Fatal("existing non-optical layout changed", row, err)
		}
	}
}
