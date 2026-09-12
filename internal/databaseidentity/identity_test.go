// SPDX-License-Identifier: AGPL-3.0-or-later

package databaseidentity

import (
	"strings"
	"testing"
)

const testAccountID = "019d2ea9-e3f7-7f52-81c7-0aeb932455db"

func TestDeriveUsesFullAccountUUIDAndFitsMariaDB(t *testing.T) {
	physical, err := Derive(testAccountID, "abcdefghijklmnopqrstuvwxyz")
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(physical) != 62 || physical != "sf_019d2ea9e3f77f5281c70aeb932455db_abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("physical name = %q (%d bytes)", physical, len(physical))
	}
}

func TestAliasFitsLiteralGrantPatternBeforeDerivation(t *testing.T) {
	for _, test := range []struct {
		name, alias string
		valid       bool
	}{
		{"letters at limit", strings.Repeat("a", 26), true},
		{"letters over limit", strings.Repeat("a", 27), false},
		{"old maximum", strings.Repeat("a", 28), false},
		{"underscore at limit", strings.Repeat("a", 24) + "_", true},
		{"underscore over limit", strings.Repeat("a", 25) + "_", false},
		{"many underscores at limit", "aa" + strings.Repeat("_", 12), true},
		{"many underscores over limit", "a" + strings.Repeat("_", 13), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateAlias(test.alias) == nil; got != test.valid {
				t.Fatalf("ValidateAlias(%q) success = %v, want %v", test.alias, got, test.valid)
			}
			physical, err := Derive(testAccountID, test.alias)
			if (err == nil) != test.valid {
				t.Fatalf("Derive(%q) = %q, %v", test.alias, physical, err)
			}
			if test.valid && len(strings.ReplaceAll(physical, "_", `\_`)) > PhysicalMaximumBytes {
				t.Fatal("accepted a truncated MariaDB privilege pattern")
			}
		})
	}
}

func TestDeriveRejectsUnsafeOrNonCanonicalInput(t *testing.T) {
	for _, alias := range []string{"", "1site", "Site", "site-name", "site`name", "site name", "abcdefghijklmnopqrstuvwx_1234"} {
		if _, err := Derive(testAccountID, alias); err == nil {
			t.Fatalf("Derive accepted alias %q", alias)
		}
	}
	if _, err := Derive("019d2ea9-e3f7-6f52-81c7-0aeb932455db", "site"); err == nil {
		t.Fatal("Derive accepted a non-UUIDv7 account")
	}
}

func TestDifferentAccountsCannotSharePhysicalName(t *testing.T) {
	first, err := Derive(testAccountID, "site")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Derive("019d2eaa-42d0-7f52-81c7-0aeb932455db", "site")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("account prefix did not separate equal aliases")
	}
}
