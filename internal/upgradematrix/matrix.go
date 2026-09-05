// SPDX-License-Identifier: AGPL-3.0-or-later

// Package upgradematrix defines the exhaustive, artifact-bound release upgrade gate.
package upgradematrix

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/RTBGG/stackfort/internal/updateapply"
)

const SchemaVersion = 1

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Release struct {
	Version       string `json:"version"`
	ArchiveSHA256 string `json:"archiveSHA256"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
}

type Catalog struct {
	SchemaVersion int       `json:"schemaVersion"`
	Releases      []Release `json:"releases"`
}

type Cell struct {
	From                string `json:"from"`
	To                  string `json:"to"`
	Image               string `json:"image"`
	Architecture        string `json:"architecture"`
	Scenario            string `json:"scenario"`
	SourceArchiveSHA256 string `json:"sourceArchiveSHA256"`
}

type Plan struct {
	SchemaVersion int    `json:"schemaVersion"`
	Target        string `json:"target"`
	Cells         []Cell `json:"cells"`
}

type Result struct {
	Cell
	TargetArchiveSHA256 string `json:"targetArchiveSHA256"`
	Status              string `json:"status"`
}

type Evidence struct {
	SchemaVersion int      `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	Results       []Result `json:"results"`
}

// Decode rejects unknown fields, trailing JSON and oversized input.
func Decode(reader io.Reader, value any) error {
	content, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(content) > 4<<20 {
		return errors.New("upgrade matrix JSON exceeds 4 MiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("upgrade matrix JSON has trailing content")
	}
	return nil
}

func Build(catalog Catalog, target string) (Plan, error) {
	plan := Plan{SchemaVersion: SchemaVersion, Target: target, Cells: []Cell{}}
	if _, err := updateapply.ParseVersion(target); err != nil {
		return Plan{}, err
	}
	if catalog.SchemaVersion != SchemaVersion || catalog.Releases == nil || len(catalog.Releases) > 100 {
		return Plan{}, errors.New("invalid upgrade support catalog")
	}
	seen := map[string]bool{}
	for _, release := range catalog.Releases {
		comparison, err := updateapply.CompareVersions(release.Version, target)
		if err != nil || comparison >= 0 || seen[release.Version] || !digestPattern.MatchString(release.ArchiveSHA256) {
			return Plan{}, fmt.Errorf("invalid or non-prior catalog release %q", release.Version)
		}
		seen[release.Version] = true
		switch release.Status {
		case "retired":
			if len(strings.TrimSpace(release.Reason)) < 20 || len(release.Reason) > 1024 {
				return Plan{}, errors.New("retired releases require a documented support decision")
			}
			continue
		case "supported":
			if release.Reason != "" {
				return Plan{}, errors.New("supported release must not have a retirement reason")
			}
		default:
			return Plan{}, errors.New("release status must be supported or retired")
		}
		for _, image := range []string{"debian-13", "ubuntu-26.04", "rocky-10"} {
			for _, scenario := range []string{"success", "health-rollback", "interrupted-recovery"} {
				plan.Cells = append(plan.Cells, Cell{From: release.Version, To: target, Image: image,
					Architecture: "amd64", Scenario: scenario, SourceArchiveSHA256: release.ArchiveSHA256})
			}
		}
	}
	sort.Slice(plan.Cells, func(i, j int) bool { return key(plan.Cells[i]) < key(plan.Cells[j]) })
	return plan, nil
}

// CheckEvidence requires each expected cell exactly once and binds it to the
// source and target archives. Rehearsals cannot satisfy the publication gate.
func CheckEvidence(plan Plan, evidence Evidence, targetDigest string) error {
	if !digestPattern.MatchString(targetDigest) || evidence.SchemaVersion != SchemaVersion || evidence.Kind != "release-candidate" {
		return errors.New("publication requires release-candidate evidence and a valid target archive digest")
	}
	wanted := make(map[Cell]bool, len(plan.Cells))
	for _, cell := range plan.Cells {
		wanted[cell] = false
	}
	if len(evidence.Results) != len(wanted) {
		return errors.New("upgrade evidence does not cover the complete matrix")
	}
	for _, result := range evidence.Results {
		seen, exists := wanted[result.Cell]
		if !exists || seen || result.Status != "passed" || result.TargetArchiveSHA256 != targetDigest {
			return fmt.Errorf("invalid, duplicate, failed or stale upgrade evidence: %s", key(result.Cell))
		}
		wanted[result.Cell] = true
	}
	return nil
}

func key(cell Cell) string { return cell.From + "/" + cell.Image + "/" + cell.Scenario }

// PublishedRelease is the bounded subset read from GitHub's public release API.
type PublishedRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Immutable  bool   `json:"immutable"`
	Assets     []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// CheckPublished prevents an accidentally empty catalog or a silently omitted
// predecessor from turning a required matrix into a vacuous pass.
func CheckPublished(catalog Catalog, target string, published []PublishedRelease) error {
	if _, err := Build(catalog, target); err != nil {
		return err
	}
	expected := map[string]Release{}
	for _, release := range catalog.Releases {
		expected[release.Version] = release
	}
	seen := map[string]bool{}
	for _, release := range published {
		if release.Draft {
			continue
		}
		version := strings.TrimPrefix(release.Tag, "v")
		comparison, err := updateapply.CompareVersions(version, target)
		if err != nil || release.Tag != "v"+version {
			return fmt.Errorf("published release has unsupported tag %q", release.Tag)
		}
		if comparison >= 0 {
			continue
		}
		entry, exists := expected[version]
		if !exists || seen[version] || !release.Immutable || release.Prerelease != strings.Contains(version, "-beta.") {
			return fmt.Errorf("published prior release %s is missing, duplicated or violates release policy", version)
		}
		seen[version] = true
		matches := 0
		for _, asset := range release.Assets {
			if asset.Name == "stackfort-"+version+"-linux-amd64.tar.gz" {
				matches++
				if asset.Digest != "sha256:"+entry.ArchiveSHA256 {
					return fmt.Errorf("published archive digest changed for %s", version)
				}
			}
		}
		if matches != 1 {
			return fmt.Errorf("published archive inventory is invalid for %s", version)
		}
	}
	if len(seen) != len(expected) {
		return errors.New("catalog contains a release absent from the published inventory")
	}
	return nil
}
