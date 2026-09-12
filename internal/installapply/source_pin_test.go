// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"strings"
	"testing"

	"github.com/RTBGG/stackfort/internal/storageprep"
)

const sourceOperation = "10000000-0000-4000-8000-000000000001"

func testSourcePin() SourcePin {
	return SourcePin{1, sourceOperation, "0.1.0-beta.3", strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)}
}

func pinPlan(pin SourcePin) storageprep.Plan {
	return storageprep.Plan{OperationID: pin.OperationID, Version: pin.Version, SourceDigest: pin.SourceDigest,
		Distribution: "debian", MachineID: "10000000-0000-4000-8000-000000000002",
		RootUUID: "10000000-0000-4000-8000-000000000003", PartitionUUID: "10000000-0000-4000-8000-000000000004",
		PreviousBootID: "10000000-0000-4000-8000-000000000005", Kernel: "6.12.107+deb13-cloud-amd64", ManifestDigest: strings.Repeat("e", 64)}
}

func TestSourcePinCanonicalBinding(t *testing.T) {
	pin := testSourcePin()
	content, err := pin.encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeSourcePin(content)
	if err != nil || decoded != pin {
		t.Fatal(err)
	}
	digest, err := pin.Digest()
	if err != nil || !pinDigestPattern.MatchString(digest) {
		t.Fatal(err)
	}
	if err := pin.ValidatePlan(pinPlan(pin)); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SourcePin){
		func(p *SourcePin) { p.OperationID = "10000000-0000-4000-8000-000000000099" },
		func(p *SourcePin) { p.Version = "0.1.0-beta.4" },
		func(p *SourcePin) { p.SourceDigest = strings.Repeat("f", 64) },
	} {
		changed := pin
		mutate(&changed)
		if err := changed.ValidatePlan(pinPlan(pin)); err == nil {
			t.Fatal("rebound pin accepted")
		}
		other, err := changed.Digest()
		if err != nil || other == digest {
			t.Fatal("pin digest did not bind identity")
		}
	}
}

func TestSourcePinRejectsMalformedReceipt(t *testing.T) {
	content, _ := testSourcePin().encode()
	for _, bad := range []string{
		"{}", string(content) + "{}", string(content[:len(content)-4]), strings.Repeat("x", maximumSourcePinBytes+1),
		strings.Replace(string(content), `"version":`, `"version": "9.9.9", "version":`, 1),
		strings.Replace(string(content), `"version":`, `"Version":`, 1),
		strings.Replace(string(content), `"version":`, `"unknown": 1, "version":`, 1),
		strings.Replace(string(content), sourceOperation, "../escape", 1),
		strings.Replace(string(content), strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
	} {
		if _, err := decodeSourcePin([]byte(bad)); err == nil {
			t.Fatal("unsafe receipt accepted")
		}
	}
}
