// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

// The API command emits json.Encoder's compact JSON plus LF, unlike private
// on-disk native records (indented JSON). Enforce that exact producer contract
// without allowing duplicate/unknown fields, trailing data, or weaker bounds.
func decodeNativeSetupImportResponse(data []byte) (core.RegisteredBootstrapCapability, error) {
	var result core.RegisteredBootstrapCapability
	if len(data) == 0 || len(data) > 4096 || json.Unmarshal(data, &result) != nil {
		return core.RegisteredBootstrapCapability{}, errors.New("invalid local setup registration response")
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) {
		return core.RegisteredBootstrapCapability{}, errors.New("noncanonical local setup registration response")
	}
	if _, err := core.ParseID(string(result.ID)); err != nil || result.CreatedAt.IsZero() || result.ExpiresAt.Sub(result.CreatedAt) != time.Hour {
		return core.RegisteredBootstrapCapability{}, errors.New("invalid local setup registration identity or lifetime")
	}
	return result, nil
}
