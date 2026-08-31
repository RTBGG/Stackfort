// SPDX-License-Identifier: AGPL-3.0-or-later

// Package acmehttp01 defines the closed HTTP-01 token format and the one
// installation-owned challenge directory shared by renderer and host agent.
package acmehttp01

import (
	"errors"
	"regexp"
	"strings"
)

const ChallengeDirectory = "/var/lib/stackfort-agent/acme-http01"

var (
	tokenPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{22,256}$`)
	thumbprintPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	ErrInvalidIntent  = errors.New("invalid ACME HTTP-01 intent")
)

type Action string

const (
	ActionPresent Action = "present"
	ActionCleanup Action = "cleanup"
)

type Intent struct {
	Action           Action `json:"action"`
	Token            string `json:"token"`
	KeyAuthorization string `json:"keyAuthorization,omitempty"`
}

func Validate(intent Intent) error {
	switch intent.Action {
	case ActionPresent:
		return ValidateKeyAuthorization(intent.Token, intent.KeyAuthorization)
	case ActionCleanup:
		if intent.KeyAuthorization != "" {
			return ErrInvalidIntent
		}
		return ValidateToken(intent.Token)
	default:
		return ErrInvalidIntent
	}
}

func ValidateToken(token string) error {
	if !tokenPattern.MatchString(token) {
		return ErrInvalidIntent
	}
	return nil
}

func ValidateKeyAuthorization(token, keyAuthorization string) error {
	if ValidateToken(token) != nil || len(keyAuthorization) > 512 || strings.ContainsAny(keyAuthorization, "\r\n\t ") {
		return ErrInvalidIntent
	}
	prefix, thumbprint, found := strings.Cut(keyAuthorization, ".")
	if !found || prefix != token || !thumbprintPattern.MatchString(thumbprint) {
		return ErrInvalidIntent
	}
	return nil
}
