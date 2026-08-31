// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingpath

import (
	"strings"
	"testing"
)

func TestNormalizeFileManagerDirectory(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "public_html", "domains/example.test", ".well-known/acme-challenge", "public_html/Grüße"} {
		if normalized, err := NormalizeFileManagerDirectory(value); err != nil || normalized != value {
			t.Errorf("NormalizeFileManagerDirectory(%q) = %q, %v", value, normalized, err)
		}
	}
	for _, value := range []string{"/etc", "../etc", "public_html/../tmp", "public_html//asset", "public_html\\asset", " public_html", "public_html\nsecret", strings.Repeat("x", MaximumFilenameBytes+1)} {
		if _, err := NormalizeFileManagerDirectory(value); err == nil {
			t.Errorf("NormalizeFileManagerDirectory(%q) accepted invalid input", value)
		}
	}
}

func TestNormalizeFileManagerFile(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"index.html", "public_html/assets/site.css", ".env"} {
		if normalized, err := NormalizeFileManagerFile(value); err != nil || normalized != value {
			t.Errorf("NormalizeFileManagerFile(%q) = %q, %v", value, normalized, err)
		}
	}
	for _, value := range []string{"", "/etc/passwd", "../etc/passwd", "public_html/../secret"} {
		if _, err := NormalizeFileManagerFile(value); err == nil {
			t.Errorf("NormalizeFileManagerFile(%q) accepted invalid input", value)
		}
	}
}

func TestValidFilenameRejectsNonRoundTrippableNames(t *testing.T) {
	t.Parallel()
	if !ValidFilename(".env") || !ValidFilename("Grüße.txt") {
		t.Fatal("valid file name rejected")
	}
	for _, value := range []string{"", ".", "..", "a/b", "a\\b", " trailing ", "line\nbreak", string([]byte{0xff})} {
		if ValidFilename(value) {
			t.Errorf("ValidFilename(%q) = true", value)
		}
	}
}
