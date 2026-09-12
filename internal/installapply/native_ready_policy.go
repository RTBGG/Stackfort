// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"errors"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

// A past conversion receipt only changes boot-artifact policy when the exact
// journal already reached Ready. In particular, Resume writes this receipt while
// the journal is still Verifying; the first readiness check must remain pinned.
// This pure predicate is not authority to skip the caller's locked source,
// manifest, live quota, device, configuration and retired-artifact checks.
func nativeReadyCompletionAccepted(state storageprep.State, plan storageprep.Plan, intent NativeRuntimeIntent, data []byte) (bool, error) {
	if state.Validate() != nil || plan.Validate() != nil || state.Plan != plan {
		return false, errors.New("native completion journal/plan mismatch")
	}
	if _, err := intent.Digest(); err != nil {
		return false, err
	}
	if intent.Profile != NativeBootProfile || state.Phase != storageprep.Ready {
		return false, nil
	}
	var receipt struct {
		Operation string `json:"operation"`
		Manifest  string `json:"manifest"`
		BootID    string `json:"bootID"`
		Status    string `json:"status"`
	}
	if err := nativeBootDecode(data, &receipt); err != nil {
		return false, err
	}
	if receipt.Operation != plan.OperationID || receipt.Manifest != plan.ManifestDigest || receipt.BootID != state.ResumeBootID || receipt.Status != "converted" {
		return false, errors.New("native completion receipt differs from verified ready journal")
	}
	return true, nil
}

// Empty expected hashes mean inspect the current OS-owned boot artifact, not
// compare it to a forever-frozen conversion-era digest. Only the fully verified
// post-conversion policy may select that mode; managed fstab remains pinned.
func nativeReadyArtifactHashes(plan storageprep.Plan, spec NativeReadySpec, kernel string, completed bool) (map[string]string, error) {
	current := plan
	current.Kernel = kernel
	if plan.Validate() != nil || current.Validate() != nil || (!completed && kernel != plan.Kernel) {
		return nil, errors.New("native current kernel is invalid or changed before readiness")
	}
	if !pinDigestPattern.MatchString(spec.FstabSHA256) || !pinDigestPattern.MatchString(spec.KernelSHA256) || !pinDigestPattern.MatchString(spec.InitrdSHA256) || !pinDigestPattern.MatchString(spec.GRUBSHA256) {
		return nil, errors.New("native readiness artifact pins are invalid")
	}
	result := map[string]string{"/etc/fstab": spec.FstabSHA256, "/boot/vmlinuz-" + kernel: spec.KernelSHA256, "/boot/initrd.img-" + kernel: spec.InitrdSHA256, "/boot/grub/grub.cfg": spec.GRUBSHA256}
	if completed {
		for path := range result {
			if path != "/etc/fstab" {
				result[path] = ""
			}
		}
	}
	return result, nil
}
