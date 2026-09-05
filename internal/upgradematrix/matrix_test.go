// SPDX-License-Identifier: AGPL-3.0-or-later

package upgradematrix

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMatrixIncludesSkippedAndBetaPredecessors(t *testing.T) {
	catalog := Catalog{SchemaVersion: 1, Releases: []Release{
		{Version: "0.1.0-beta.1", ArchiveSHA256: strings.Repeat("a", 64), Status: "supported"},
		{Version: "0.1.0-beta.2", ArchiveSHA256: strings.Repeat("b", 64), Status: "supported"},
		{Version: "0.1.0", ArchiveSHA256: strings.Repeat("c", 64), Status: "supported"},
	}}
	plan, err := Build(catalog, "0.2.0")
	if err != nil || len(plan.Cells) != 27 {
		t.Fatalf("cells=%d err=%v", len(plan.Cells), err)
	}
	target := strings.Repeat("d", 64)
	evidence := Evidence{SchemaVersion: 1, Kind: "release-candidate"}
	for _, cell := range plan.Cells {
		evidence.Results = append(evidence.Results, Result{Cell: cell, TargetArchiveSHA256: target, Status: "passed"})
	}
	if err := CheckEvidence(plan, evidence, target); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing", "duplicate", "failed", "target-changed", "source-changed", "wrong-os", "rehearsal", "unknown-schema"} {
		t.Run(name, func(t *testing.T) {
			mutated := evidence
			mutated.Results = append([]Result{}, evidence.Results...)
			switch name {
			case "missing":
				mutated.Results = mutated.Results[1:]
			case "duplicate":
				mutated.Results[0] = mutated.Results[1]
			case "failed":
				mutated.Results[0].Status = "failed"
			case "target-changed":
				mutated.Results[0].TargetArchiveSHA256 = strings.Repeat("e", 64)
			case "source-changed":
				mutated.Results[0].SourceArchiveSHA256 = strings.Repeat("e", 64)
			case "wrong-os":
				mutated.Results[0].Image = "ubuntu-24.04"
			case "rehearsal":
				mutated.Kind = "rehearsal"
			case "unknown-schema":
				mutated.SchemaVersion = 2
			}
			if err := CheckEvidence(plan, mutated, target); err == nil {
				t.Fatal("unsafe evidence accepted")
			}
		})
	}
}

func TestCatalogCannotSilentlyOmitPublishedPredecessor(t *testing.T) {
	digest := strings.Repeat("a", 64)
	var published []PublishedRelease
	if err := json.Unmarshal([]byte(`[{"tag_name":"v0.1.0-beta.1","prerelease":true,"immutable":true,"assets":[{"name":"stackfort-0.1.0-beta.1-linux-amd64.tar.gz","digest":"sha256:`+digest+`"}]}]`), &published); err != nil {
		t.Fatal(err)
	}
	catalog := Catalog{SchemaVersion: 1, Releases: []Release{}}
	if err := CheckPublished(catalog, "0.1.0", published); err == nil {
		t.Fatal("empty catalog hid a published predecessor")
	}
	catalog.Releases = []Release{{Version: "0.1.0-beta.1", ArchiveSHA256: digest, Status: "supported"}}
	if err := CheckPublished(catalog, "0.1.0", published); err != nil {
		t.Fatal(err)
	}
	if err := CheckPublished(catalog, "0.1.0", nil); err == nil {
		t.Fatal("unpublished baseline accepted")
	}
	published[0].Immutable = false
	if err := CheckPublished(catalog, "0.1.0", published); err == nil {
		t.Fatal("mutable baseline accepted")
	}
	published[0].Immutable = true
	catalog.Releases[0].ArchiveSHA256 = strings.Repeat("b", 64)
	if err := CheckPublished(catalog, "0.1.0", published); err == nil {
		t.Fatal("wrong archive accepted")
	}
}

func TestCatalogValidationAndExplicitRetirement(t *testing.T) {
	valid := Release{Version: "1.0.0", ArchiveSHA256: strings.Repeat("a", 64), Status: "supported"}
	for _, release := range []Release{
		{Version: "01.0.0", ArchiveSHA256: valid.ArchiveSHA256, Status: "supported"},
		{Version: "2.0.0", ArchiveSHA256: valid.ArchiveSHA256, Status: "supported"},
		{Version: "1.0.0", ArchiveSHA256: "unverified", Status: "supported"},
		{Version: "1.0.0", ArchiveSHA256: valid.ArchiveSHA256, Status: "retired"},
		{Version: "1.0.0", ArchiveSHA256: valid.ArchiveSHA256, Status: "unknown"},
	} {
		if _, err := Build(Catalog{SchemaVersion: 1, Releases: []Release{release}}, "2.0.0"); err == nil {
			t.Fatalf("accepted %#v", release)
		}
	}
	if _, err := Build(Catalog{SchemaVersion: 1, Releases: []Release{valid, valid}}, "2.0.0"); err == nil {
		t.Fatal("duplicate accepted")
	}
	valid.Status, valid.Reason = "retired", "Support ended; use the documented intermediate release first."
	plan, err := Build(Catalog{SchemaVersion: 1, Releases: []Release{valid}}, "2.0.0")
	if err != nil || len(plan.Cells) != 0 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	for _, input := range []string{`{"schemaVersion":1,"releases":[],"typo":true}`, `{"schemaVersion":1,"releases":[]} {}`, strings.Repeat(" ", (4<<20)+1)} {
		var catalog Catalog
		if err := Decode(strings.NewReader(input), &catalog); err == nil {
			t.Fatal("malformed catalog accepted")
		}
	}
}
