// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

const nativeSetupName = "native-setup.json"

// Only this commitment is persisted. The raw one-use administrator setup code
// is delivered to the controlling terminal before the authorized reboot.
type NativeSetupCommitment struct {
	SchemaVersion int    `json:"schemaVersion"`
	OperationID   string `json:"operationId"`
	ReleaseSHA256 string `json:"releaseSHA256"`
	TokenSHA256   string `json:"tokenSHA256"`
}

func IssueNativeSetup(review NativeOnboardingReview) (string, NativeSetupCommitment, error) {
	if err := review.Validate(); err != nil {
		return "", NativeSetupCommitment{}, err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", NativeSetupCommitment{}, err
	}
	code := "sfb_" + base64.RawURLEncoding.EncodeToString(raw[:])
	clear(raw[:])
	commitment := NativeSetupCommitment{SchemaVersion: 1, OperationID: review.Recovery.Release.Source.OperationID,
		ReleaseSHA256: admissionDigest(nativeBootJSON(review.Recovery.Release)), TokenSHA256: admissionDigest([]byte(code))}
	return code, commitment, nil
}

func (commitment NativeSetupCommitment) Validate(review NativeOnboardingReview) error {
	if err := review.Validate(); err != nil {
		return err
	}
	return commitment.validateBinding(review.Recovery.Release)
}

func (commitment NativeSetupCommitment) validateBinding(binding ReleaseBinding) error {
	if binding.Validate() != nil || commitment.SchemaVersion != 1 || commitment.OperationID != binding.Source.OperationID ||
		commitment.ReleaseSHA256 != admissionDigest(nativeBootJSON(binding)) || !pinDigestPattern.MatchString(commitment.TokenSHA256) || commitment.TokenSHA256 == strings.Repeat("0", 64) {
		return errors.New("setup commitment differs from the authenticated installation")
	}
	return nil
}
