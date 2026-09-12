// SPDX-License-Identifier: AGPL-3.0-or-later

package hostresources

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/RTBGG/stackfort/internal/hostingresources"
)

const managedUnitHeader = "# Managed by Stackfort. Do not edit.\n"

type renderedUnit struct {
	directory string
	name      string
	content   string
}

func renderUnits(spec hostingresources.Spec, processorCount int) ([]renderedUnit, error) {
	if err := hostingresources.Validate(spec); err != nil || processorCount < 1 || processorCount > 16_384 {
		return nil, ErrMutationFailed
	}
	accountUnit, err := hostingresources.AccountSliceName(spec.Identity.UID)
	if err != nil {
		return nil, ErrMutationFailed
	}
	properties, err := hostingresources.SystemdProperties(spec)
	if err != nil {
		return nil, ErrMutationFailed
	}

	core := hostingresources.CoreSliceUnit()
	accounts := hostingresources.AccountsSliceUnit(uint64(processorCount))
	var account strings.Builder
	account.WriteString(managedUnitHeader)
	account.WriteString("[Unit]\nDescription=Stackfort hosting account ")
	account.WriteString(strconv.FormatUint(uint64(spec.Identity.UID), 10))
	account.WriteString(" resource boundary\nRequires=stackfort-accounts.slice\nAfter=stackfort-accounts.slice\n\n[Slice]\n")
	for _, property := range properties {
		account.WriteString(property)
		account.WriteByte('\n')
	}
	if strings.Contains(account.String(), "\x00") {
		return nil, fmt.Errorf("%w: rendered unit contains NUL", ErrMutationFailed)
	}
	userManager, err := hostingresources.UserManagerUnitName(spec.Identity.UID)
	if err != nil {
		return nil, ErrMutationFailed
	}
	userManagerDropIn := managedUnitHeader + `[Service]
Slice=` + accountUnit + `
`
	return []renderedUnit{
		{name: "stackfort-core.slice", content: core},
		{name: "stackfort-accounts.slice", content: accounts},
		{name: accountUnit, content: account.String()},
		{
			directory: userManager + ".d", name: "50-stackfort-account-boundary.conf",
			content: userManagerDropIn,
		},
	}, nil
}
