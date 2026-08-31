// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ociimage defines Stackfort's closed, immutable OCI image
// preparation and vulnerability policy. It does not expose Podman arguments,
// host paths, registry credentials, or a container-engine socket.
package ociimage

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
	"github.com/RTBGG/stackfort/internal/ociapps"
	"github.com/google/uuid"
)

const (
	PolicyVersion             = "stackfort-oci-image-v1"
	ScannerProvider           = "trivy"
	ScannerVersion            = "0.74.0"
	MaximumContainerfileBytes = 1 << 20
	MaximumBuildContextBytes  = 512 << 20
	MaximumBuildContextFiles  = 20_000
	MaximumScanReportBytes    = 16 << 20
	MaximumImageArchiveBytes  = 2 << 30
	BuildMemoryBytes          = 1 << 30
	BuildCPUPeriod            = 100_000
	BuildCPUQuota             = 100_000
	BuildProcessLimit         = 512
	BuildOpenFileLimit        = 1_024
	BuildTimeoutSeconds       = 15 * 60
	PreparationTimeoutSeconds = 35 * 60
	MaximumRevision           = 1_000_000_000
	TransactionRoot           = "/var/lib/stackfort-agent/oci-transactions"
	ArtifactRoot              = "/var/lib/stackfort-agent/oci-images"
	ScannerCacheRoot          = "/var/lib/stackfort-agent/trivy-cache"
	ScannerExecutable         = "/usr/local/libexec/stackfort-trivy"
)

var (
	ErrInvalid       = errors.New("invalid OCI image preparation intent")
	ErrBuildContext  = errors.New("OCI build context is unsafe or exceeds its limits")
	ErrBuildFailed   = errors.New("bounded OCI image build failed")
	ErrPullFailed    = errors.New("digest-pinned OCI image pull failed")
	ErrInspectFailed = errors.New("OCI image identity inspection failed")
	ErrScanFailed    = errors.New("OCI image vulnerability scan failed")
	ErrScanRejected  = errors.New("OCI image rejected by vulnerability policy")
	digestPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type PrepareSpec struct {
	Identity      hostingidentity.Spec `json:"identity"`
	ApplicationID string               `json:"applicationId"`
	Revision      int64                `json:"revision"`
	Source        ociapps.Source       `json:"source"`
}

type VulnerabilitySummary struct {
	Unknown  int64 `json:"unknown"`
	Low      int64 `json:"low"`
	Medium   int64 `json:"medium"`
	High     int64 `json:"high"`
	Critical int64 `json:"critical"`
}

func (summary VulnerabilitySummary) Total() int64 {
	return summary.Unknown + summary.Low + summary.Medium + summary.High + summary.Critical
}

type Result struct {
	ImageDigest     string               `json:"imageDigest"`
	SourceDigest    string               `json:"sourceDigest,omitempty"`
	PolicyVersion   string               `json:"policyVersion"`
	ScannerProvider string               `json:"scannerProvider"`
	ScannerVersion  string               `json:"scannerVersion"`
	Vulnerabilities VulnerabilitySummary `json:"vulnerabilities"`
	Reused          bool                 `json:"reused,omitempty"`
}

func ValidateSpec(spec PrepareSpec) error {
	if err := hostingidentity.Validate(spec.Identity); err != nil {
		return fmt.Errorf("%w: identity", ErrInvalid)
	}
	parsed, err := uuid.Parse(spec.ApplicationID)
	if err != nil || parsed.String() != spec.ApplicationID || parsed.Version() != uuid.Version(7) {
		return fmt.Errorf("%w: applicationId", ErrInvalid)
	}
	if spec.Revision < 1 || spec.Revision > MaximumRevision {
		return fmt.Errorf("%w: revision", ErrInvalid)
	}
	if normalized, err := ociapps.NormalizeSource(spec.Source); err != nil || normalized != spec.Source {
		return fmt.Errorf("%w: source", ErrInvalid)
	}
	return nil
}

func ValidateResult(result Result) error {
	if !ValidDigest(result.ImageDigest) || result.PolicyVersion != PolicyVersion ||
		result.ScannerProvider != ScannerProvider || result.ScannerVersion != ScannerVersion ||
		result.Vulnerabilities.Unknown < 0 || result.Vulnerabilities.Low < 0 ||
		result.Vulnerabilities.Medium < 0 || result.Vulnerabilities.High < 0 ||
		result.Vulnerabilities.Critical < 0 || result.Vulnerabilities.Total() > 1_000_000 {
		return ErrInvalid
	}
	if !ValidDigest(result.SourceDigest) {
		return ErrInvalid
	}
	if result.Vulnerabilities.High != 0 || result.Vulnerabilities.Critical != 0 {
		return ErrScanRejected
	}
	return nil
}

func ValidDigest(value string) bool { return digestPattern.MatchString(value) }

func RequestedDigest(reference string) (string, error) {
	_, digest, found := strings.Cut(reference, "@")
	if !found || !ValidDigest(digest) {
		return "", ErrInvalid
	}
	return digest, nil
}

func SemanticDigest(spec PrepareSpec) (string, error) {
	if err := ValidateSpec(spec); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func LocalTag(spec PrepareSpec) (string, error) {
	if err := ValidateSpec(spec); err != nil {
		return "", err
	}
	return "localhost/stackfort/u" + strconv.FormatUint(uint64(spec.Identity.UID), 10) +
		"/" + spec.ApplicationID + ":r" + strconv.FormatInt(spec.Revision, 10), nil
}

func TransactionDirectory(operationID string) (string, error) {
	parsed, err := uuid.Parse(operationID)
	if err != nil || parsed.String() != operationID || parsed.Version() != uuid.Version(7) {
		return "", ErrInvalid
	}
	return path.Join(TransactionRoot, operationID), nil
}

func InvocationValues(spec PrepareSpec) ([]string, error) {
	if err := ValidateSpec(spec); err != nil {
		return nil, err
	}
	return []string{
		spec.Identity.AccountID, spec.Identity.Username,
		strconv.FormatUint(uint64(spec.Identity.UID), 10), strconv.FormatUint(uint64(spec.Identity.GID), 10),
		spec.Identity.HomeDirectory, spec.ApplicationID, strconv.FormatInt(spec.Revision, 10), string(spec.Source.Kind),
		spec.Source.ImageReference, spec.Source.BuildContext, spec.Source.ContainerfilePath,
	}, nil
}

func FromInvocationValues(values []string) (PrepareSpec, error) {
	if len(values) != 11 {
		return PrepareSpec{}, ErrInvalid
	}
	uid, uidErr := strconv.ParseUint(values[2], 10, 32)
	gid, gidErr := strconv.ParseUint(values[3], 10, 32)
	revision, revisionErr := strconv.ParseInt(values[6], 10, 64)
	if uidErr != nil || gidErr != nil || revisionErr != nil {
		return PrepareSpec{}, ErrInvalid
	}
	spec := PrepareSpec{
		Identity: hostingidentity.Spec{
			AccountID: values[0], Username: values[1], UID: uint32(uid), GID: uint32(gid), HomeDirectory: values[4],
		},
		ApplicationID: values[5], Revision: revision,
		Source: ociapps.Source{
			Kind: ociapps.SourceKind(values[7]), ImageReference: values[8],
			BuildContext: values[9], ContainerfilePath: values[10],
		},
	}
	return spec, ValidateSpec(spec)
}

// ValidateContainerfile rejects indirection that could bypass the fixed build
// policy. Every external base must be explicit-registry and digest-pinned;
// build networking is disabled separately by the execution profile.
func ValidateContainerfile(content []byte) error {
	if len(content) == 0 || len(content) > MaximumContainerfileBytes || bytes.IndexByte(content, 0) >= 0 {
		return fmt.Errorf("%w: Containerfile size", ErrBuildContext)
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	instructions := make([]string, 0, 64)
	continued := ""
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") && continued == "" {
			if strings.HasPrefix(strings.ToLower(line), "# syntax=") || strings.HasPrefix(strings.ToLower(line), "# escape=") {
				return fmt.Errorf("%w: parser directives are not supported", ErrBuildContext)
			}
			continue
		}
		continued += line
		if strings.HasSuffix(continued, "\\") {
			continued = strings.TrimSpace(strings.TrimSuffix(continued, "\\")) + " "
			if len(continued) > 64<<10 {
				return fmt.Errorf("%w: instruction length", ErrBuildContext)
			}
			continue
		}
		instructions = append(instructions, continued)
		continued = ""
		if len(instructions) > 4096 {
			return fmt.Errorf("%w: instruction count", ErrBuildContext)
		}
	}
	if err := scanner.Err(); err != nil || continued != "" {
		return fmt.Errorf("%w: malformed continuation", ErrBuildContext)
	}

	stages := map[string]struct{}{}
	fromCount := 0
	for _, line := range instructions {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return fmt.Errorf("%w: malformed instruction", ErrBuildContext)
		}
		instruction := strings.ToUpper(fields[0])
		switch instruction {
		case "FROM":
			fromCount++
			if err := validateFrom(fields[1:], stages, fromCount); err != nil {
				return err
			}
		case "ADD", "ONBUILD", "VOLUME":
			return fmt.Errorf("%w: %s is outside the bounded build contract", ErrBuildContext, instruction)
		case "RUN":
			lower := strings.ToLower(line)
			if strings.Contains(lower, "--mount=type=secret") || strings.Contains(lower, "--mount=type=ssh") ||
				strings.Contains(lower, "--security=") || strings.Contains(lower, "--network=") {
				return fmt.Errorf("%w: RUN option", ErrBuildContext)
			}
		case "COPY":
			for _, field := range fields[1:] {
				if !strings.HasPrefix(strings.ToLower(field), "--from=") {
					continue
				}
				stage := strings.TrimPrefix(strings.ToLower(field), "--from=")
				if _, exists := stages[stage]; !exists {
					if index, err := strconv.Atoi(stage); err != nil || index < 0 || index >= fromCount {
						return fmt.Errorf("%w: external COPY source", ErrBuildContext)
					}
				}
			}
		case "ARG", "CMD", "ENTRYPOINT", "ENV", "EXPOSE", "HEALTHCHECK", "LABEL", "MAINTAINER", "SHELL", "STOPSIGNAL", "USER", "WORKDIR":
			// These affect only image contents/metadata. Runtime privilege and
			// ingress are imposed later by Stackfort rather than inherited.
		default:
			return fmt.Errorf("%w: unsupported instruction %s", ErrBuildContext, instruction)
		}
	}
	if fromCount == 0 {
		return fmt.Errorf("%w: missing FROM", ErrBuildContext)
	}
	return nil
}

func validateFrom(fields []string, stages map[string]struct{}, index int) error {
	if len(fields) != 1 && len(fields) != 3 || len(fields) == 3 && !strings.EqualFold(fields[1], "AS") {
		return fmt.Errorf("%w: malformed FROM", ErrBuildContext)
	}
	if strings.HasPrefix(fields[0], "--") || fields[0] != "scratch" {
		source := ociapps.Source{Kind: ociapps.SourceImageDigest, ImageReference: fields[0]}
		if _, err := ociapps.NormalizeSource(source); err != nil {
			return fmt.Errorf("%w: FROM must be digest-pinned", ErrBuildContext)
		}
	}
	stages[strconv.Itoa(index-1)] = struct{}{}
	if len(fields) == 3 {
		name := strings.ToLower(fields[2])
		if name == "" || strings.ContainsAny(name, "/:@${} ") {
			return fmt.Errorf("%w: stage name", ErrBuildContext)
		}
		stages[name] = struct{}{}
	}
	return nil
}

type trivyReport struct {
	Results []struct {
		Vulnerabilities []struct {
			Severity string `json:"Severity"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

func ParseTrivyReport(content []byte) (VulnerabilitySummary, error) {
	if len(content) == 0 || len(content) > MaximumScanReportBytes {
		return VulnerabilitySummary{}, ErrScanFailed
	}
	var report trivyReport
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&report); err != nil {
		return VulnerabilitySummary{}, ErrScanFailed
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return VulnerabilitySummary{}, ErrScanFailed
	}
	if len(report.Results) > 4096 {
		return VulnerabilitySummary{}, ErrScanFailed
	}
	summary := VulnerabilitySummary{}
	for _, result := range report.Results {
		for _, vulnerability := range result.Vulnerabilities {
			if summary.Total() >= 1_000_000 {
				return VulnerabilitySummary{}, ErrScanFailed
			}
			switch strings.ToUpper(vulnerability.Severity) {
			case "LOW":
				summary.Low++
			case "MEDIUM":
				summary.Medium++
			case "HIGH":
				summary.High++
			case "CRITICAL":
				summary.Critical++
			default:
				summary.Unknown++
			}
		}
	}
	return summary, nil
}
