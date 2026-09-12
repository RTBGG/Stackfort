// SPDX-License-Identifier: AGPL-3.0-or-later

package installapply

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RTBGG/stackfort/internal/core"
)

func TestNativeSetupImportResponseMatchesProductionCompactEncoder(t *testing.T) {
	created := time.Date(2026, 9, 12, 10, 0, 0, 123456789, time.UTC)
	expected := core.RegisteredBootstrapCapability{ID: core.ID("019ffd13-9819-7c51-9f53-0d0ed3b36c42"), CreatedAt: created, ExpiresAt: created.Add(time.Hour)}
	for _, replayed := range []bool{false, true} {
		expected.AlreadyRegistered = replayed
		var output bytes.Buffer
		if err := json.NewEncoder(&output).Encode(expected); err != nil {
			t.Fatal(err)
		}
		actual, err := decodeNativeSetupImportResponse(output.Bytes())
		if err != nil || actual != expected {
			t.Fatal(actual, err)
		}
	}
	for _, scenario := range []string{"empty", "truncated", "indented", "duplicate", "unknown", "trailing", "no-lf", "extra-lf", "oversized", "identity", "zero-created", "lifetime"} {
		value := expected
		switch scenario {
		case "identity":
			value.ID = "invalid"
		case "zero-created":
			value.CreatedAt = time.Time{}
		case "lifetime":
			value.ExpiresAt = value.ExpiresAt.Add(time.Second)
		}
		data, _ := json.Marshal(value)
		data = append(data, '\n')
		switch scenario {
		case "empty":
			data = nil
		case "truncated":
			data = []byte("{")
		case "indented":
			data = nativeBootJSON(value)
		case "duplicate":
			data = append([]byte("{\"alreadyRegistered\":true,"), data[1:]...)
		case "unknown":
			data = append([]byte("{\"extra\":true,"), data[1:]...)
		case "trailing":
			data = append(data, []byte("{}\n")...)
		case "no-lf":
			data = data[:len(data)-1]
		case "extra-lf":
			data = append(data, '\n')
		case "oversized":
			data = []byte(strings.Repeat(" ", 4097))
		}
		if _, err := decodeNativeSetupImportResponse(data); err == nil {
			t.Fatal("unsafe response accepted", scenario)
		}
	}
}
