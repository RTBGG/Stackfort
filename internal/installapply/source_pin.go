// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/RTBGG/stackfort/internal/storageprep"
	"github.com/google/uuid"
)

const maximumSourcePinBytes = 4096

var pinDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SourcePin binds a locally trusted extracted release to one storage operation.
// It proves persistence/integrity, not publisher identity. The future offline
// manifest must itself bind this pin; a receipt read from disk is not authority.
type SourcePin struct {
	SchemaVersion   int    `json:"schemaVersion"`
	OperationID     string `json:"operationId"`
	Version         string `json:"version"`
	SourceDigest    string `json:"sourceDigest"`
	TreeSHA256      string `json:"treeSHA256"`
	InstallerSHA256 string `json:"installerSHA256"`
	ManifestSHA256  string `json:"manifestSHA256"`
}

func (pin SourcePin) Validate() error {
	if !validSourceOperation(pin.OperationID) || pin.SchemaVersion != 1 ||
		len(pin.Version) > 128 || !semanticVersionPattern.MatchString(pin.Version) {
		return errors.New("invalid resume source pin")
	}
	for _, digest := range []string{pin.SourceDigest, pin.TreeSHA256, pin.InstallerSHA256, pin.ManifestSHA256} {
		if !pinDigestPattern.MatchString(digest) {
			return errors.New("invalid resume source digest")
		}
	}
	return nil
}

func validSourceOperation(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func (pin SourcePin) ValidatePlan(plan storageprep.Plan) error {
	if err := pin.Validate(); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if plan.OperationID != pin.OperationID || plan.Version != pin.Version || plan.SourceDigest != pin.SourceDigest {
		return errors.New("resume source differs from storage plan")
	}
	return nil
}

// Digest is intended for inclusion in a separately validated boot manifest.
func (pin SourcePin) Digest() (string, error) {
	content, err := pin.encode()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}

func (pin SourcePin) encode() ([]byte, error) {
	if err := pin.Validate(); err != nil {
		return nil, err
	}
	content, err := json.MarshalIndent(pin, "", "  ")
	return append(content, '\n'), err
}

func decodeSourcePin(content []byte) (SourcePin, error) {
	if len(content) > maximumSourcePinBytes {
		return SourcePin{}, errors.New("resume source receipt exceeds size limit")
	}
	var pin SourcePin
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pin); err != nil {
		return SourcePin{}, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return SourcePin{}, errors.New("resume source receipt contains trailing data")
	}
	canonical, err := pin.encode()
	if err != nil || !bytes.Equal(content, canonical) {
		return SourcePin{}, errors.New("resume source receipt is not canonical")
	}
	return pin, nil
}
