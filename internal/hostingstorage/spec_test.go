// SPDX-License-Identifier: AGPL-3.0-or-later

package hostingstorage

import (
	"testing"

	"github.com/RTBGG/stackfort/internal/hostingidentity"
)

func TestValidate(t *testing.T) {
	accountID := "019c1234-5678-7abc-8def-0123456789ab"
	username, _ := hostingidentity.UsernameForAccount(accountID)
	home, _ := hostingidentity.HomeDirectoryForAccount(accountID)
	identity := hostingidentity.Spec{
		AccountID: accountID, Username: username, UID: 200000, GID: 200000, HomeDirectory: home,
	}
	valid := Spec{Identity: identity, ProjectID: identity.UID, ByteLimit: 10 << 30, InodeLimit: 100000}
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate(valid): %v", err)
	}
	for name, mutate := range map[string]func(*Spec){
		"project mismatch": func(value *Spec) { value.ProjectID++ },
		"sub-block bytes":  func(value *Spec) { value.ByteLimit = 512 },
		"unaligned bytes":  func(value *Spec) { value.ByteLimit++ },
		"overflow bytes":   func(value *Spec) { value.ByteLimit = 1 << 63 },
		"overflow inodes":  func(value *Spec) { value.InodeLimit = 1 << 63 },
	} {
		t.Run(name, func(t *testing.T) {
			value := valid
			mutate(&value)
			if err := Validate(value); err == nil {
				t.Fatal("Validate accepted an invalid specification")
			}
		})
	}
}
