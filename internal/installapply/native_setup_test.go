// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"strings"
	"testing"
)

func TestNativeSetupCodesAreBoundAndNeverSerialized(t *testing.T) {
	choice := testRecoveryChoice()
	choice.Review.InstallerSHA256 = choice.Review.Release.Source.InstallerSHA256
	review := NativeOnboardingReview{SchemaVersion: 1, Source: NativeOnboardingSource{
		SourceDirectory: "/var/tmp/bootstrap/source", ArchivePath: "/var/tmp/bootstrap/archive.tar.gz", AttestationPath: "/var/tmp/bootstrap/attestations.jsonl", Origin: choice.Review.Release.Policy}, Recovery: choice.Review}
	first, commitment, err := IssueNativeSetup(review)
	if err != nil || len(first) != 47 || !strings.HasPrefix(first, "sfb_") || commitment.Validate(review) != nil || commitment.TokenSHA256 != admissionDigest([]byte(first)) {
		t.Fatal("invalid setup commitment", err)
	}
	second, next, err := IssueNativeSetup(review)
	if err != nil || second == first || next.TokenSHA256 == commitment.TokenSHA256 {
		t.Fatal("setup codes reused", err)
	}
	if bytes.Contains(nativeBootJSON(commitment), []byte(first)) {
		t.Fatal("raw code serialized")
	}
	for _, edit := range []func(*NativeSetupCommitment){
		func(c *NativeSetupCommitment) { c.SchemaVersion++ },
		func(c *NativeSetupCommitment) { c.OperationID = "foreign" },
		func(c *NativeSetupCommitment) { c.ReleaseSHA256 = strings.Repeat("0", 64) },
		func(c *NativeSetupCommitment) { c.TokenSHA256 = first },
	} {
		altered := commitment
		edit(&altered)
		if altered.Validate(review) == nil {
			t.Fatal("invalid setup binding accepted")
		}
	}
}
