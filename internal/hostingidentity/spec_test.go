// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingidentity

import (
	"errors"
	"testing"
)

func TestSpecIsStableAndIndependentFromDisplayData(t *testing.T) {
	t.Parallel()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, err := UsernameForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if username != "sf_3456787abc8def0123456789ab" || len(username) != 29 {
		t.Fatalf("username = %q", username)
	}
	home, err := HomeDirectoryForAccount(accountID)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{AccountID: accountID, Username: username, UID: MinimumID, GID: MinimumID, HomeDirectory: home}
	if err := Validate(spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestSpecRejectsSubstitutedIdentityFields(t *testing.T) {
	t.Parallel()
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := UsernameForAccount(accountID)
	home, _ := HomeDirectoryForAccount(accountID)
	valid := Spec{AccountID: accountID, Username: username, UID: MinimumID, GID: MinimumID, HomeDirectory: home}
	tests := []struct {
		name   string
		mutate func(*Spec)
	}{
		{"uuid version", func(spec *Spec) { spec.AccountID = "550e8400-e29b-41d4-a716-446655440000" }},
		{"username", func(spec *Spec) { spec.Username = "customer" }},
		{"uid", func(spec *Spec) { spec.UID = MinimumID - 1 }},
		{"gid", func(spec *Spec) { spec.GID++ }},
		{"home", func(spec *Spec) { spec.HomeDirectory = "/home/customer" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := valid
			test.mutate(&spec)
			if err := Validate(spec); !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
