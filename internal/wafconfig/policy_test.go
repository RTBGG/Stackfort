// SPDX-License-Identifier: AGPL-3.0-or-later

package wafconfig

import (
	"errors"
	"strings"
	"testing"
)

func TestModesMapOnlyToFixedProfiles(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		mode Mode
		path string
	}{
		{"", ""}, {ModeOff, ""},
		{ModeDetectionOnly, DetectionPL1Path}, {ModeBlockingPL1, BlockingPL1Path},
	} {
		path, err := ProfilePath(fixture.mode)
		if err != nil || path != fixture.path {
			t.Fatalf("ProfilePath(%q) = %q, %v", fixture.mode, path, err)
		}
	}
	for _, mode := range []Mode{"advanced", "On", "../../evil", "detection_only\nInclude /tmp/evil"} {
		if _, err := ProfilePath(mode); !errors.Is(err, ErrInvalidMode) {
			t.Errorf("ProfilePath(%q) error = %v", mode, err)
		}
	}
}

func TestProfilesPinPL1AndDisableSensitiveAuditStorage(t *testing.T) {
	t.Parallel()
	engine := Engine()
	for _, required := range []string{
		"SecRequestBodyAccess On", "SecResponseBodyAccess Off", "SecAuditEngine Off",
		"SecDebugLogLevel 0", "ctl:requestBodyProcessor=XML",
		"ctl:requestBodyProcessor=JSON", "SecRequestBodyInMemoryLimit 1048576",
		PersistentRoot,
	} {
		if !strings.Contains(engine, required) {
			t.Errorf("engine configuration omits %q", required)
		}
	}
	for _, unsupported := range []string{"SecPcreMatchLimit", "SecStatusEngine", "SecUnicodeMapFile", "SecRequestBodyNoFilesLimit"} {
		if strings.Contains(engine, unsupported) {
			t.Errorf("Coraza engine configuration contains unsupported %q", unsupported)
		}
	}
	base := BasePL1()
	for _, required := range []string{EnginePath, CRSSetupPath, CRSRulesPattern, "paranoia level 1"} {
		if !strings.Contains(base, required) {
			t.Errorf("base profile omits %q", required)
		}
	}
	if strings.Contains(SharedPL1(), "coraza_") || strings.Contains(SharedPL1(), BasePL1Path) {
		t.Fatal("shared NGINX policy must not make Coraza rules global")
	}
	for name, profile := range map[string]string{
		"detection": DetectionPL1(), "blocking": BlockingPL1(),
	} {
		for _, forbidden := range []string{
			"SecRuleRemove", "SecAction", "paranoia_level=2", EnginePath, CRSSetupPath, CRSRulesPattern,
		} {
			if strings.Contains(profile, forbidden) {
				t.Errorf("%s profile contains unsupported or duplicated %q", name, forbidden)
			}
		}
		if !strings.Contains(profile, BasePL1Path) {
			t.Errorf("%s profile does not include the fixed PL1 base", name)
		}
	}
	if !strings.Contains(DetectionPL1(), "SecRuleEngine DetectionOnly") ||
		!strings.Contains(BlockingPL1(), "SecRuleEngine On") {
		t.Fatal("profiles do not select their exact engine modes")
	}
}
