// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostingidentity defines the stable Linux identity assigned to one
// hosting account. The identity is derived from the opaque account ID and is
// deliberately independent from mutable names, slugs, and email addresses.
package hostingidentity

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	ManagedAccountsRoot        = "/srv/hosting/accounts"
	NoLoginShell               = "/usr/sbin/nologin"
	MinimumID           uint32 = 200_000
	MaximumID           uint32 = 599_999
)

var ErrInvalidSpec = errors.New("invalid hosting account Unix identity")

// Spec is the complete immutable Linux identity persisted for one account.
type Spec struct {
	AccountID     string `json:"accountId"`
	Username      string `json:"username"`
	UID           uint32 `json:"uid"`
	GID           uint32 `json:"gid"`
	HomeDirectory string `json:"homeDirectory"`
}

// UsernameForAccount encodes the final 104 UUID bits into a 29-character,
// portable Linux username. For UUIDv7 this retains its complete random portion;
// the database also enforces uniqueness. No user-supplied text is incorporated.
func UsernameForAccount(accountID string) (string, error) {
	parsed, err := parseAccountID(accountID)
	if err != nil {
		return "", err
	}
	compact := fmt.Sprintf("%x", parsed[:])
	return "sf_" + compact[len(compact)-26:], nil
}

func HomeDirectoryForAccount(accountID string) (string, error) {
	parsed, err := parseAccountID(accountID)
	if err != nil {
		return "", err
	}
	return ManagedAccountsRoot + "/" + parsed.String(), nil
}

// Validate verifies every redundant field so protocol callers cannot select a
// different username or path for an otherwise valid account ID.
func Validate(spec Spec) error {
	expectedUsername, err := UsernameForAccount(spec.AccountID)
	if err != nil {
		return err
	}
	expectedHome, err := HomeDirectoryForAccount(spec.AccountID)
	if err != nil {
		return err
	}
	if spec.Username != expectedUsername {
		return fmt.Errorf("%w: username does not match accountId", ErrInvalidSpec)
	}
	if spec.HomeDirectory != expectedHome {
		return fmt.Errorf("%w: homeDirectory does not match accountId", ErrInvalidSpec)
	}
	if spec.UID < MinimumID || spec.UID > MaximumID {
		return fmt.Errorf("%w: uid is outside the reserved range", ErrInvalidSpec)
	}
	if spec.GID != spec.UID {
		return fmt.Errorf("%w: gid must equal uid", ErrInvalidSpec)
	}
	return nil
}

func parseAccountID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value || parsed.Version() != uuid.Version(7) {
		return uuid.Nil, fmt.Errorf("%w: accountId must be a canonical UUIDv7", ErrInvalidSpec)
	}
	return parsed, nil
}
