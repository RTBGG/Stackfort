// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

const NativeRuntimePath = "/var/lib/stackfort-installer/native-runtime-installer"
const NativeRuntimeGateUnit = "stackfort-native-admission-gate.service"
const NativeRuntimeVerifyUnit = "stackfort-native-storage-verify.service"
const NativeRuntimeInstallUnit = "stackfort-native-install.service"
const NativeRuntimeResultPrefix = "NATIVE_RUNTIME_RESULT="
const NativeRuntimeConsumerDependency = "# Stackfort disposable native-installation lab only.\n[Unit]\nBindsTo=srv-hosting.mount\nAfter=srv-hosting.mount\n"
const nativeRuntimeName = "native-runtime-intent.json"

// NativeRuntimeIntent is sealed BEFORE the storage journal is created. This
// first profile qualifies only post-conversion services on the Debian fixture;
// it neither registers an offline backend nor enables public native installs.
// BootIntentSHA256 binds the independently qualified offline handoff capsule.
type NativeRuntimeIntent struct {
	SchemaVersion    int               `json:"schemaVersion"`
	Profile          string            `json:"profile"`
	InstallerSHA256  string            `json:"installerSHA256"`
	BootIntentSHA256 string            `json:"bootIntentSHA256"`
	Ready            NativeReadySpec   `json:"ready"`
	Offline          *NativeBootIntent `json:"offline,omitempty"`
	SetupSHA256      string            `json:"setupSHA256,omitempty"`
}

type NativeReadySpec struct {
	Features     string `json:"features"`
	Blocks       string `json:"blocks"`
	BlockSize    string `json:"blockSize"`
	InodeSize    string `json:"inodeSize"`
	FstabSHA256  string `json:"fstabSHA256"`
	KernelSHA256 string `json:"kernelSHA256"`
	InitrdSHA256 string `json:"initrdSHA256"`
	GRUBSHA256   string `json:"grubSHA256"`
}

func (intent NativeRuntimeIntent) Digest() (string, error) {
	if intent.SetupSHA256 != "" && (intent.Profile != NativeBootProfile || !pinDigestPattern.MatchString(intent.SetupSHA256)) {
		return "", errors.New("invalid native setup binding")
	}
	if intent.SchemaVersion != 1 || (intent.Profile != "debian-native-post-ready-qualification-v1" && intent.Profile != NativeBootProfile) {
		return "", errors.New("native runtime profile is not qualified")
	}
	if intent.Profile == NativeBootProfile {
		if intent.Offline == nil {
			return "", errors.New("missing offline intent")
		}
		digest, err := intent.Offline.Digest()
		if err != nil || digest != intent.BootIntentSHA256 || admissionDigest([]byte(intent.Offline.FstabAfter)) != intent.Ready.FstabSHA256 {
			return "", errors.New("offline intent binding mismatch")
		}
	} else if intent.Offline != nil {
		return "", errors.New("offline intent forbidden in post-ready profile")
	}
	for _, value := range []string{intent.InstallerSHA256, intent.BootIntentSHA256, intent.Ready.FstabSHA256, intent.Ready.KernelSHA256, intent.Ready.InitrdSHA256, intent.Ready.GRUBSHA256} {
		if !pinDigestPattern.MatchString(value) {
			return "", errors.New("invalid runtime artifact digest")
		}
	}
	for _, value := range []string{intent.Ready.Blocks, intent.Ready.BlockSize, intent.Ready.InodeSize} {
		if !regexp.MustCompile(`^[1-9][0-9]{0,18}$`).MatchString(value) {
			return "", errors.New("invalid native filesystem geometry")
		}
	}
	features := strings.Fields(intent.Ready.Features)
	if len(features) == 0 || strings.Join(features, " ") != intent.Ready.Features {
		return "", errors.New("invalid baseline features")
	}
	for index, feature := range features {
		if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(feature) || slices.Contains(features[:index], feature) {
			return "", errors.New("invalid or duplicate baseline feature")
		}
	}
	data, _ := json.MarshalIndent(intent, "", "  ")
	return admissionDigest(append(data, '\n')), nil
}

type NativeServiceRequest struct{ Action, OperationID string }

func (request NativeServiceRequest) Validate() error {
	if !validSourceOperation(request.OperationID) || !slices.Contains([]string{"close", "verify-storage", "admit", "quarantine", "recheck-completed", "recheck-cleanup"}, request.Action) {
		return errors.New("invalid internal native service invocation")
	}
	return nil
}

// NativeRuntimeUnits contains no test program, caller-selected path, shell,
// repair command or automatic retry. Activation remains an internal lab step.
func NativeRuntimeUnits(operation string) (map[string]string, error) {
	if !validSourceOperation(operation) {
		return nil, errors.New("invalid runtime operation")
	}
	command := func(action string) string {
		return NativeRuntimePath + " native-service " + action + " --operation-id=" + operation
	}
	return map[string]string{
		NativeRuntimeGateUnit:    "[Unit]\nDescription=Stackfort native installation web gate\nDefaultDependencies=no\nAfter=local-fs.target nftables.service\nBefore=network-pre.target\nWants=network-pre.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + command("close") + "\nTimeoutStartSec=60s\n\n[Install]\nWantedBy=multi-user.target\n",
		NativeRuntimeVerifyUnit:  "[Unit]\nDescription=Stackfort native storage live verification\nRequires=" + NativeRuntimeGateUnit + "\nAfter=local-fs.target " + NativeRuntimeGateUnit + "\nBefore=srv-hosting.mount\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + command("verify-storage") + "\nTimeoutStartSec=180s\n",
		NativeRuntimeInstallUnit: "[Unit]\nDescription=Stackfort native installation admission\nWants=network-online.target\nRequires=srv-hosting.mount\nAfter=network-online.target srv-hosting.mount\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + command("admit") + "\nExecStopPost=" + command("quarantine") + "\nTimeoutStartSec=20min\nTimeoutStopSec=90s\n\n[Install]\nWantedBy=multi-user.target\n",
		"srv-hosting.mount":      "[Unit]\nDescription=Stackfort qualified native hosting bind\nDefaultDependencies=no\nRequires=" + NativeRuntimeVerifyUnit + "\nAfter=local-fs.target " + NativeRuntimeVerifyUnit + "\nBefore=umount.target nginx.service\nConflicts=umount.target\n\n[Mount]\nWhat=/srv/stackfort-native-hosting\nWhere=/srv/hosting\nType=none\nOptions=bind\n\n[Install]\nRequiredBy=nginx.service\n",
	}, nil
}

func nativeFeatureDelta(before, after string) error {
	old, current := strings.Fields(before), strings.Fields(after)
	transient := func(value string) bool { return value == "needs_recovery" || value == "orphan_present" }
	for _, value := range current {
		if !slices.Contains(old, value) && value != "project" && value != "quota" && !transient(value) {
			return fmt.Errorf("unexpected filesystem feature: %s", value)
		}
	}
	for _, value := range old {
		if !slices.Contains(current, value) && !transient(value) {
			return fmt.Errorf("filesystem feature disappeared: %s", value)
		}
	}
	return nil
}

func requireNativeRuntimeReady(state storageprep.State, exists bool, plan storageprep.Plan) error {
	if !exists || state.Plan != plan || state.Phase != storageprep.Ready {
		return errors.New("native service requires the exact already-ready storage journal; no conversion or reset is available")
	}
	return nil
}
