// SPDX-License-Identifier: AGPL-3.0-or-later

package ociimage

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
)

func TestValidateContainerfileRequiresPinnedBasesAndClosedFeatures(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	valid := "FROM registry.example/base@sha256:" + digest + " AS build\nRUN make app\nFROM scratch\nCOPY --from=build /app /app\nENTRYPOINT [\"/app\"]\n"
	if err := ValidateContainerfile([]byte(valid)); err != nil {
		t.Fatalf("valid Containerfile: %v", err)
	}
	for name, content := range map[string]string{
		"tagged base":       "FROM registry.example/base:latest\n",
		"implicit registry": "FROM library/base@sha256:" + digest + "\n",
		"remote add":        "FROM scratch\nADD https://example.test/app /app\n",
		"external copy":     "FROM scratch\nCOPY --from=registry.example/tool@sha256:" + digest + " /x /x\n",
		"volume":            "FROM scratch\nVOLUME /data\n",
		"secret mount":      "FROM registry.example/base@sha256:" + digest + "\nRUN --mount=type=secret,id=x true\n",
		"frontend":          "# syntax=docker/dockerfile:1\nFROM scratch\n",
	} {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateContainerfile([]byte(content)); err == nil {
				t.Fatalf("unsafe Containerfile accepted: %s", content)
			}
		})
	}
}

func TestPrepareSpecAndResultAreClosedAndImmutable(t *testing.T) {
	t.Parallel()
	accountID := "019d2eaa-42d0-7f52-8ac7-0aeb932455db"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	spec := PrepareSpec{
		Identity:      hostingidentity.Spec{AccountID: accountID, Username: username, UID: 200000, GID: 200000, HomeDirectory: home},
		ApplicationID: "019d2eaa-52d0-7f52-8ac7-0aeb932455db", Revision: 3,
		Source: ociapps.Source{Kind: ociapps.SourceImageDigest, ImageReference: "registry.example/app@sha256:" + strings.Repeat("b", 64)},
	}
	if err := ValidateSpec(spec); err != nil {
		t.Fatal(err)
	}
	first, err := SemanticDigest(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := SemanticDigest(spec)
	if first != second || len(first) != 64 {
		t.Fatalf("semantic digests = %q / %q", first, second)
	}
	result := Result{
		ImageDigest: "sha256:" + strings.Repeat("c", 64), SourceDigest: "sha256:" + strings.Repeat("b", 64),
		PolicyVersion: PolicyVersion, ScannerProvider: ScannerProvider, ScannerVersion: ScannerVersion,
	}
	if err := ValidateResult(result); err != nil {
		t.Fatal(err)
	}
	result.SourceDigest = ""
	if err := ValidateResult(result); err != ErrInvalid {
		t.Fatalf("missing source digest error = %v", err)
	}
	result.SourceDigest = "sha256:" + strings.Repeat("b", 64)
	result.ScannerVersion = "0.74.1"
	if err := ValidateResult(result); err != ErrInvalid {
		t.Fatalf("unexpected scanner version error = %v", err)
	}
	result.ScannerVersion = ScannerVersion
	result.Vulnerabilities.High = 1
	if err := ValidateResult(result); err != ErrScanRejected {
		t.Fatalf("high severity result error = %v", err)
	}
}

func TestParseTrivyReportCountsAndRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	report := `{"Results":[{"Vulnerabilities":[{"Severity":"LOW"},{"Severity":"HIGH"},{"Severity":"CRITICAL"},{"Severity":"UNKNOWN"}]}]}`
	summary, err := ParseTrivyReport([]byte(report))
	if err != nil || summary.Low != 1 || summary.High != 1 || summary.Critical != 1 || summary.Unknown != 1 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
	if _, err := ParseTrivyReport([]byte(`{"Results":[]} trailing`)); err == nil {
		t.Fatal("trailing scan data was accepted")
	}
}
