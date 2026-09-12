// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

// NativeOnboardingPrepared is returned only after preparation's shared source
// lock has closed successfully. It does not mean armed, rebooted, converted, or
// publicly admitted; callers must use the sealed runtime for the next phase.
type NativeOnboardingPrepared struct {
	OperationID     string                `json:"operationId"`
	RuntimePath     string                `json:"runtimePath"`
	InstallerSHA256 string                `json:"installerSHA256"`
	Manifest        NativeReleaseManifest `json:"manifest"`
}
