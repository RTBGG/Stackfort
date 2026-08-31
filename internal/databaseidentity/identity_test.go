// SPDX-License-Identifier: AGPL-3.0-or-later

package databaseidentity

import "testing"

const testAccountID = "019d2ea9-e3f7-7f52-81c7-0aeb932455db"

func TestDeriveUsesFullAccountUUIDAndFitsMariaDB(t *testing.T) {
	physical, err := Derive(testAccountID, "abcdefghijklmnopqrstuvwx_123")
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(physical) != PhysicalMaximumBytes || physical != "sf_019d2ea9e3f77f5281c70aeb932455db_abcdefghijklmnopqrstuvwx_123" {
		t.Fatalf("physical name = %q (%d bytes)", physical, len(physical))
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
