// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Still qualification-only: no public activation or arbitrary device selection.
const NativeBootProfile = "debian-native-offline-qualification-v1"
const nativeBootStateDirectory = "/var/lib/stackfort-installer"
const NativeBootFinalizeUnit = "stackfort-native-storage-finalize.service"
const nativeBootHook = "/etc/initramfs-tools/hooks/stackfort-native-quota"
const nativeBootPremount = "/etc/initramfs-tools/scripts/local-premount/stackfort-native-quota"
const nativeBootProofPath = "/run/initramfs/stackfort-native-boot-proof.json"
const nativeBootMarker = "/run/initramfs/stackfort-native-quota-mutating"
const nativeBootEmbeddedManifest = "/etc/stackfort-native/release.json"
const nativeBootEmbeddedRuntime = "/etc/stackfort-native/runtime.json"
const nativeBootEmbeddedInstaller = "/stackfort-native-installer"
const nativeBootArtifactsName = "native-boot-artifacts.json"

var nativeBootTools = []string{"/usr/sbin/e2fsck", "/usr/sbin/tune2fs", "/usr/sbin/blkid", "/usr/sbin/debugfs"}

type NativeBootIntent struct {
	RecoveryChoiceSHA256 string            `json:"recoveryChoiceSHA256,omitempty"`
	PowerLossGuard       bool              `json:"powerLossGuard,omitempty"`
	PrerequisitesSHA256  string            `json:"prerequisitesSHA256,omitempty"`
	SchemaVersion        int               `json:"schemaVersion"`
	FstabBefore          string            `json:"fstabBefore"`
	FstabAfter           string            `json:"fstabAfter"`
	Tools                map[string]string `json:"tools"`
}

func (intent NativeBootIntent) Digest() (string, error) {
	if intent.RecoveryChoiceSHA256 != "" && (!pinDigestPattern.MatchString(intent.RecoveryChoiceSHA256) || !intent.PowerLossGuard || intent.PrerequisitesSHA256 == "") {
		return "", errors.New("invalid recovery-choice binding")
	}
	if intent.PrerequisitesSHA256 != "" && !pinDigestPattern.MatchString(intent.PrerequisitesSHA256) {
		return "", errors.New("invalid prerequisite binding")
	}
	if intent.SchemaVersion != 1 || len(intent.FstabBefore) == 0 || len(intent.FstabBefore) > 16<<10 || len(intent.FstabAfter) > 16<<10 || intent.FstabBefore == intent.FstabAfter || len(intent.Tools) != len(nativeBootTools) {
		return "", errors.New("invalid native boot intent")
	}
	for _, path := range nativeBootTools {
		if !pinDigestPattern.MatchString(intent.Tools[path]) {
			return "", errors.New("missing offline tool pin")
		}
	}
	return admissionDigest(nativeBootJSON(intent)), nil
}

func nativeBootJSON(value any) []byte {
	data, _ := json.MarshalIndent(value, "", "  ")
	return append(data, '\n')
}
func nativeBootDecode(data []byte, target any) error {
	if len(data) == 0 || len(data) > 64<<10 {
		return errors.New("invalid boot record size")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	if !bytes.Equal(data, nativeBootJSON(target)) {
		return errors.New("noncanonical boot record")
	}
	return nil
}

// Keep unrelated lines byte-for-byte; reject ambiguous/foreign root entries.
func nativeBootFstab(source, rootUUID, partUUID string) (string, error) {
	if !validSourceOperation(rootUUID) || !validSourceOperation(partUUID) || len(source) > 16<<10 || strings.ContainsAny(source, "\x00\r") {
		return "", errors.New("invalid fstab input")
	}
	lines, count := strings.Split(source, "\n"), 0
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) < 4 {
			return "", errors.New("malformed fstab")
		}
		if fields[1] == "/srv/hosting" || fields[1] == "/srv/stackfort-native-hosting" {
			return "", errors.New("existing hosting mount")
		}
		if fields[1] != "/" {
			continue
		}
		if len(fields) != 6 || fields[2] != "ext4" || (fields[0] != "UUID="+rootUUID && fields[0] != "PARTUUID="+partUUID) || fields[4] != "0" || fields[5] != "1" {
			return "", errors.New("unsupported root fstab entry")
		}
		count++
		options := strings.Split(fields[3], ",")
		for _, option := range options {
			if option == "" || option == "ro" || strings.Contains(option, "quota") || (strings.HasPrefix(option, "x-") && option != "x-systemd.growfs") || slices.Contains([]string{"noauto", "bind", "rbind", "remount"}, option) {
				return "", errors.New("conflicting root mount option")
			}
		}
		fields[3] += ",prjquota"
		lines[i] = strings.Join(fields, "\t")
	}
	if count != 1 {
		return "", errors.New("expected exactly one root fstab entry")
	}
	return strings.Join(lines, "\n"), nil
}

type NativeBootRequest struct{ Action, OperationID string }

func (request NativeBootRequest) Validate() error {
	if !validSourceOperation(request.OperationID) || !slices.Contains([]string{"arm", "early", "finalize"}, request.Action) {
		return errors.New("invalid internal native boot invocation")
	}
	return nil
}

func NativeBootUnits(operation string) (map[string]string, error) {
	units, err := NativeRuntimeUnits(operation)
	if err != nil {
		return nil, err
	}
	units[NativeBootFinalizeUnit] = "[Unit]\nDescription=Stackfort native storage finalization\nRequires=" + NativeRuntimeGateUnit + "\nAfter=local-fs.target " + NativeRuntimeGateUnit + "\nBefore=" + NativeRuntimeVerifyUnit + "\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + NativeRuntimePath + " native-boot finalize --operation-id=" + operation + "\nTimeoutStartSec=180s\n"
	units[NativeRuntimeVerifyUnit] = strings.Replace(units[NativeRuntimeVerifyUnit], "[Unit]\n", "[Unit]\nRequires="+NativeBootFinalizeUnit+"\nAfter="+NativeBootFinalizeUnit+"\n", 1)
	return units, nil
}

func nativeBootScripts(operation string) (string, string, error) {
	if !validSourceOperation(operation) {
		return "", "", errors.New("invalid boot operation")
	}
	hook := "#!/bin/sh\nset -eu\ncase ${1:-} in prereqs) exit 0;; esac\n. /usr/share/initramfs-tools/hook-functions\n"
	for _, path := range nativeBootTools {
		hook += "copy_exec " + path + "\n"
	}
	hook += "copy_file executable " + NativeRuntimePath + " " + nativeBootEmbeddedInstaller + "\ncopy_file config " + nativeBootStateDirectory + "/native-release-manifest.json " + nativeBootEmbeddedManifest + "\ncopy_file config " + nativeBootStateDirectory + "/" + nativeRuntimeName + " " + nativeBootEmbeddedRuntime + "\n"
	premount := "#!/bin/sh\ncase ${1:-} in prereqs) exit 0;; esac\n. /scripts/functions\nmkdir -p /run/initramfs\n" + nativeBootEmbeddedInstaller + " native-boot early --operation-id=" + operation + " > /run/initramfs/stackfort-native-quota.log 2>&1\nresult=$?\ncat /run/initramfs/stackfort-native-quota.log > /dev/kmsg\nif [ $result -ne 0 ] && [ -e " + nativeBootMarker + " ]; then\n  while :; do panic 'Stackfort metadata operation uncertain; filesystem recovery required'; done\nfi\n# Pre-write rejection permits OS boot, but finalization and hosting remain blocked.\nexit 0\n"
	return hook, premount, nil
}

// The guarded profile never falls through to the normal root-mount path on an
// uncertain boot. Historical lab renderers remain unchanged.
func nativeBootGuardedScripts(operation string) (string, string, error) {
	hook, premount, err := nativeBootScripts(operation)
	if err != nil {
		return "", "", err
	}
	premount = strings.Replace(premount, "if [ $result -ne 0 ] && [ -e "+nativeBootMarker+" ]; then", "if [ $result -ne 0 ]; then", 1)
	premount = strings.Replace(premount, "# Pre-write rejection permits OS boot, but finalization and hosting remain blocked.", "# Any rejection remains in initramfs; no automatic fsck, root mount or retry.", 1)
	return hook, premount, nil
}
