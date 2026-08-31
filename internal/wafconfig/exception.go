// SPDX-License-Identifier: AGPL-3.0-or-later

package wafconfig

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	MinimumExceptionRuleID = 920000
	MaximumExceptionRuleID = 944999
	MaximumExceptions      = 64
	MaximumExceptionPath   = 512
	MaximumParameterName   = 128
	MinimumExceptionTTL    = 5 * time.Minute
	MaximumExceptionTTL    = 30 * 24 * time.Hour
)

var (
	parameterNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	pathPattern          = regexp.MustCompile(`^/[A-Za-z0-9/_.~,!$&()+;=:@%-]{0,511}$`)
)

// ValidateExceptionScope accepts only a closed CRS rule range and an exact
// request path and/or exact argument name. It deliberately rejects regexes,
// SecLang fragments, headers, request bodies, and wildcard paths.
func ValidateExceptionScope(ruleID uint32, requestPath, parameter string) error {
	if ruleID < MinimumExceptionRuleID || ruleID > MaximumExceptionRuleID {
		return errors.New("WAF exception rule ID is outside the supported inbound CRS range")
	}
	if requestPath == "" && parameter == "" {
		return errors.New("WAF exception requires an exact path or parameter")
	}
	if requestPath != strings.TrimSpace(requestPath) ||
		(requestPath != "" && (!pathPattern.MatchString(requestPath) || strings.Contains(requestPath, "//"))) {
		return errors.New("WAF exception path is invalid")
	}
	if parameter != strings.TrimSpace(parameter) ||
		(parameter != "" && !parameterNamePattern.MatchString(parameter)) {
		return errors.New("WAF exception parameter is invalid")
	}
	return nil
}

// ValidateExceptionExpiry enforces the short administrator-facing lifetime.
func ValidateExceptionExpiry(now, expiresAt time.Time) error {
	now = now.UTC()
	expiresAt = expiresAt.UTC()
	if expiresAt.Before(now.Add(MinimumExceptionTTL)) || expiresAt.After(now.Add(MaximumExceptionTTL)) {
		return errors.New("WAF exception expiry must be between five minutes and thirty days")
	}
	return nil
}
