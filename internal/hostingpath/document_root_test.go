// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingpath

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDocumentRoot(t *testing.T) {
	for _, value := range []string{"public_html", "domains/example.test/public", "sites/a_b-c.1"} {
		if normalized, err := NormalizeDocumentRoot(value); err != nil || normalized != value {
			t.Errorf("NormalizeDocumentRoot(%q) = %q, %v", value, normalized, err)
		}
	}
	for _, value := range []string{
		"", "/public_html", "public_html/../private", "public_html/./assets",
		"public_html//assets", "public_html\\assets", ".hidden", "trailing/", " padded",
		"public_html/evil\x00", strings.Repeat("a", MaximumDocumentRootBytes+1),
	} {
		if _, err := NormalizeDocumentRoot(value); !errors.Is(err, ErrInvalidDocumentRoot) {
			t.Errorf("NormalizeDocumentRoot(%q) error = %v", value, err)
		}
	}
}
