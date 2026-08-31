// SPDX-License-Identifier: AGPL-3.0-or-later

// Package databaseidentity derives bounded MariaDB object names from the
// owning Stackfort account. Callers never accept physical names from browsers.
package databaseidentity

import (
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const (
	AliasMaximumBytes    = 28
	PhysicalMaximumBytes = 64
	LocalHost            = "localhost"
)

var aliasPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,27}$`)

func ValidateAlias(alias string) error {
	if !aliasPattern.MatchString(alias) {
		return errors.New("database alias must be lowercase ASCII, start with a letter, and contain at most 28 characters")
	}
	return nil
}

func Derive(accountID, alias string) (string, error) {
	parsed, err := uuid.Parse(accountID)
	if err != nil || parsed.String() != accountID || parsed.Version() != uuid.Version(7) {
		return "", errors.New("database account identifier must be a canonical UUIDv7")
	}
	if err := ValidateAlias(alias); err != nil {
		return "", err
	}
	physical := "sf_" + strings.ReplaceAll(accountID, "-", "") + "_" + alias
	if len(physical) > PhysicalMaximumBytes {
		return "", errors.New("derived database identifier exceeds the MariaDB limit")
	}
	return physical, nil
}

func ValidateDerived(accountID, alias, physical string) error {
	expected, err := Derive(accountID, alias)
	if err != nil {
		return err
	}
	if physical != expected {
		return errors.New("physical database identifier does not match its account and alias")
	}
	return nil
}
