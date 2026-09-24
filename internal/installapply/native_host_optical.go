// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

type nativeOptionalOptical struct{ source, target string }

var nativeOpticalSource = regexp.MustCompile(`^/dev/sr(0|[1-9][0-9]{0,3})$`)
var nativeOpticalTarget = regexp.MustCompile(`^/media/cdrom(0|[1-9][0-9]{0,3})?$`)

// These are inert fstab declarations, not permission to mount, unmount, edit
// fstab or ignore another filesystem. nofail alone is NOT equivalent to noauto;
// x-systemd.automount overrides noauto, so unknown options fail closed.
func nativeHostFstabPolicy(text string) ([]nativeOptionalOptical, error) {
	if text == "" || len(text) > 16<<10 || strings.ContainsAny(text, "\x00\r") {
		return nil, errors.New("invalid fstab input")
	}
	var optical []nativeOptionalOptical
	var targets []string
	for number, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		fail := func(reason string) ([]nativeOptionalOptical, error) {
			// Do not echo a possibly credential-bearing source/options field.
			return nil, fmt.Errorf("fstab line %d: %s", number+1, reason)
		}
		if len(fields) < 4 {
			return fail("malformed filesystem entry")
		}
		targets = append(targets, fields[1])
		if strings.Contains(fields[3], "quota") {
			return fail("existing quota mount policy")
		}
		if fields[1] == "/" || fields[1] == "/boot/efi" || fields[2] == "tmpfs" || fields[2] == "swap" {
			continue // Existing policy; root binding is separately validated.
		}
		// Permit an ordinary trailing comment, without interpreting escapes.
		for i := 4; i < len(fields); i++ {
			if strings.HasPrefix(fields[i], "#") {
				fields = fields[:i]
				break
			}
		}
		if len(fields) != 6 || !nativeOpticalSource.MatchString(fields[0]) ||
			!nativeOpticalTarget.MatchString(fields[1]) || fields[4] != "0" || fields[5] != "0" ||
			!slices.Contains([]string{"udf", "iso9660", "udf,iso9660", "iso9660,udf"}, fields[2]) {
			return fail("additional persistent filesystems require separate qualification; only inactive optional optical entries are exempt")
		}
		options := strings.Split(fields[3], ",")
		seen := map[string]bool{}
		for _, option := range options {
			if seen[option] || !slices.Contains([]string{"noauto", "user", "users", "ro", "nosuid", "nodev", "noexec", "nofail"}, option) {
				return fail("optional optical entry has conflicting, duplicate or unqualified options")
			}
			seen[option] = true
		}
		if !seen["noauto"] || (seen["user"] && seen["users"]) {
			return fail("optional optical entry requires unambiguous noauto policy")
		}
		optical = append(optical, nativeOptionalOptical{fields[0], fields[1]})
	}
	for _, entry := range optical {
		matches := 0
		for _, target := range targets {
			if target == entry.target {
				matches++
			} else if target != "/" && nativeOpticalPathsOverlap(entry.target, target) {
				return nil, errors.New("optional optical target overlaps another fstab entry")
			}
		}
		if matches != 1 {
			return nil, errors.New("duplicate optional optical target")
		}
	}
	return optical, nil
}

func nativeOpticalPathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// Inspect every row, including stacked/hidden mounts and autofs, not just
// findmnt --real. Never use a substring match for /media/cdrom1 vs cdrom10.
func nativeOpticalUnmounted(entries []nativeOptionalOptical, mountinfo string) error {
	if len(entries) == 0 {
		return nil
	}
	if mountinfo == "" || len(mountinfo) > 256<<10 {
		return errors.New("cannot inspect optional optical mount state")
	}
	rootSeen := false
	for _, line := range strings.Split(strings.TrimSuffix(mountinfo, "\n"), "\n") {
		fields := strings.Fields(line)
		separator := slices.Index(fields, "-")
		if separator < 6 || separator+4 != len(fields) {
			return errors.New("malformed optical mount-state record")
		}
		target, err := nativeOpticalMountPath(fields[4])
		if err != nil {
			return err
		}
		if target == "/" {
			rootSeen = true
		}
		if fields[separator+1] == "iso9660" || fields[separator+1] == "udf" {
			return errors.New("an optical filesystem is mounted; optional optical exception requires inactive media")
		}
		for _, entry := range entries {
			if fields[separator+2] == entry.source || (target != "/" && nativeOpticalPathsOverlap(entry.target, target)) {
				return errors.New("optional optical source or target has an active mount or automount")
			}
		}
	}
	if !rootSeen {
		return errors.New("incomplete optical mount-state inventory")
	}
	return nil
}

func nativeOpticalMountPath(value string) (string, error) {
	var decoded strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			decoded.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", errors.New("invalid mount path escape")
		}
		code, ok := map[string]byte{"040": ' ', "011": '\t', "012": '\n', "134": '\\'}[value[i+1:i+4]]
		if !ok {
			return "", errors.New("invalid mount path escape")
		}
		decoded.WriteByte(code)
		i += 3
	}
	result := decoded.String()
	if !strings.HasPrefix(result, "/") || path.Clean(result) != result || strings.ContainsRune(result, 0) {
		return "", errors.New("noncanonical mount path")
	}
	return result, nil
}
