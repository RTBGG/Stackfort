// SPDX-License-Identifier: AGPL-3.0-or-later

// Package wafconfig defines the closed Coraza/OWASP CRS policy surface
// used by Stackfort. It intentionally exposes no raw SecLang or filesystem
// path input from account users.
package wafconfig

import (
	"errors"
	"fmt"
)

const (
	CorazaVersion      = "3.7.0"
	LibCorazaVersion   = "1.7.0"
	CorazaNGINXVersion = "0.20.0"
	GoToolchainVersion = "1.25.12"
	CRSVersion         = "4.25.1"

	ConfigurationRoot = "/etc/nginx/stackfort/coraza"
	EnginePath        = ConfigurationRoot + "/engine.conf"
	ProfilesDirectory = ConfigurationRoot + "/profiles"
	BasePL1Path       = ProfilesDirectory + "/base-pl1.conf"
	DetectionPL1Path  = ProfilesDirectory + "/detection-pl1.conf"
	BlockingPL1Path   = ProfilesDirectory + "/blocking-pl1.conf"
	SharedPL1Path     = "/etc/nginx/stackfort/global/20-waf-pl1.conf"
	CRSRoot           = "/usr/share/stackfort/owasp-crs-" + CRSVersion
	CRSSetupPath      = CRSRoot + "/crs-setup.conf"
	CRSRulesPattern   = CRSRoot + "/rules/*.conf"
	EngineDataRoot    = "/usr/share/stackfort/coraza-" + CorazaVersion
	RuntimeRoot       = "/var/cache/stackfort/coraza"
	PersistentRoot    = RuntimeRoot + "/data"
)

var ErrInvalidMode = errors.New("invalid WAF mode")

// Mode is the complete account-managed WAF policy set for the first WAF
// slice. Advanced administrator exceptions are modeled separately later.
type Mode string

const (
	ModeOff           Mode = "off"
	ModeDetectionOnly Mode = "detection_only"
	ModeBlockingPL1   Mode = "blocking_pl1"
)

// Normalize treats the empty value as off for backwards-compatible decoding
// of desired-state revisions created before WAF policy existed.
func Normalize(mode Mode) (Mode, error) {
	if mode == "" {
		return ModeOff, nil
	}
	switch mode {
	case ModeOff, ModeDetectionOnly, ModeBlockingPL1:
		return mode, nil
	default:
		return "", ErrInvalidMode
	}
}

func ProfilePath(mode Mode) (string, error) {
	normalized, err := Normalize(mode)
	if err != nil {
		return "", err
	}
	switch normalized {
	case ModeOff:
		return "", nil
	case ModeDetectionOnly:
		return DetectionPL1Path, nil
	case ModeBlockingPL1:
		return BlockingPL1Path, nil
	default:
		panic("unreachable WAF mode")
	}
}

// Engine renders privacy-conservative common Coraza settings. Raw audit logs
// and response-body inspection are deliberately disabled. The separate
// structured event stream exposes only explicitly allowlisted fields.
func Engine() string {
	return fmt.Sprintf(`# Managed by Stackfort. Do not edit.
SecRequestBodyAccess On
SecRule REQUEST_HEADERS:Content-Type "^(?:application(?:/soap[+]|/)|text/)xml" \
    "id:'200000',phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=XML"
SecRule REQUEST_HEADERS:Content-Type "^application/json" \
    "id:'200001',phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=JSON"
SecRule REQUEST_HEADERS:Content-Type "^application/[a-z0-9.-]+[+]json" \
    "id:'200006',phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=JSON"
SecRequestBodyLimit 134217728
SecRequestBodyInMemoryLimit 1048576
SecRequestBodyLimitAction Reject
SecRequestBodyJsonDepthLimit 512
SecRule &ARGS "@ge 1000" \
    "id:'200007',phase:2,t:none,log,deny,status:400,msg:'Request argument limit reached',severity:2"
SecRule REQBODY_ERROR "!@eq 0" \
    "id:'200002',phase:2,t:none,log,deny,status:400,msg:'Request body parsing failed',severity:2"
SecRule MULTIPART_STRICT_ERROR "!@eq 0" \
    "id:'200003',phase:2,t:none,log,deny,status:400,msg:'Multipart strict validation failed',severity:2"
SecResponseBodyAccess Off
SecDataDir %s
SecAuditEngine Off
SecDebugLogLevel 0
`, PersistentRoot)
}

// BasePL1 is the immutable ruleset included only by active, closed profiles.
// coraza-nginx creates WAFs after the worker fork and compiles inherited rules
// per location, so loading this globally would also allocate a WAF for every
// off, panel, and default-server context.
func BasePL1() string {
	return fmt.Sprintf(`# Managed by Stackfort. Do not edit.
# OWASP CRS %s, paranoia level 1. Account users cannot add SecLang here.
Include %s
Include %s
Include %s
`, CRSVersion, EnginePath, CRSSetupPath, CRSRulesPattern)
}

func SharedPL1() string {
	return `# Managed by Stackfort. Do not edit.
# Coraza rules are deliberately not inherited at HTTP scope. Active domains
# load one of the fixed profiles below; WAF-off and panel hosts allocate none.
`
}

func DetectionPL1() string { return profile("DetectionOnly") }

func BlockingPL1() string { return profile("On") }

func profile(engineMode string) string {
	return fmt.Sprintf(`# Managed by Stackfort. Do not edit.
Include %s
SecRuleEngine %s
`, BasePL1Path, engineMode)
}
