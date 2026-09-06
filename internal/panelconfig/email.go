// SPDX-License-Identifier: AGPL-3.0-or-later

package panelconfig

import (
	"net/mail"
	"strings"
)

func Email(value string) error {
	parsed, err := mail.ParseAddress(value)
	if err != nil || len(value) > 254 || parsed.Name != "" || parsed.Address != value || strings.ContainsAny(value, "\r\n\x00") {
		return ErrInvalid
	}
	return nil
}
