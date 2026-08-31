// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RTBGG/stackfort/internal/hostingpath"
	"golang.org/x/net/idna"
)

var (
	// Keep these options explicit. idna.Lookup may change its profile between
	// dependency releases, while routing identifiers must remain reproducible.
	domainIDNAProfile = idna.New(
		idna.MapForLookup(),
		idna.Transitional(false),
		idna.StrictDomainName(true),
		idna.ValidateLabels(true),
		idna.VerifyDNSLength(true),
		idna.BidiRule(),
	)
)

// NormalizeDomainName converts a DNS host name into stable display and routing
// forms. One terminal root-label separator is accepted but not persisted.
func NormalizeDomainName(input string) (NormalizedDomainName, error) {
	value := strings.TrimSpace(input)
	if value == "" || containsControl(value) {
		return NormalizedDomainName{}, fmt.Errorf("%w: domain name is empty or contains control characters", ErrInvalidInput)
	}
	if strings.Contains(value, "*") {
		return NormalizedDomainName{}, fmt.Errorf("%w: wildcard labels are configured as redirect behavior, not domain names", ErrInvalidInput)
	}
	if strings.ContainsAny(value, "/\\@:#?[]") {
		return NormalizedDomainName{}, fmt.Errorf("%w: domain name must not include a scheme, port, path, or credentials", ErrInvalidInput)
	}
	value = trimOneRootSeparator(value)
	if value == "" {
		return NormalizedDomainName{}, fmt.Errorf("%w: domain name has no labels", ErrInvalidInput)
	}

	asciiName, err := domainIDNAProfile.ToASCII(value)
	if err != nil {
		return NormalizedDomainName{}, fmt.Errorf("%w: invalid IDNA domain: %v", ErrInvalidInput, err)
	}
	asciiName = strings.ToLower(asciiName)
	labels := strings.Split(asciiName, ".")
	if len(labels) < 2 {
		return NormalizedDomainName{}, fmt.Errorf("%w: domain name must contain at least two DNS labels", ErrInvalidInput)
	}
	if _, err := netip.ParseAddr(asciiName); err == nil {
		return NormalizedDomainName{}, fmt.Errorf("%w: an IP address is not a domain name", ErrInvalidInput)
	}

	displayName, err := domainIDNAProfile.ToUnicode(asciiName)
	if err != nil {
		return NormalizedDomainName{}, fmt.Errorf("%w: decode normalized IDNA domain: %v", ErrInvalidInput, err)
	}
	roundTrip, err := domainIDNAProfile.ToASCII(displayName)
	if err != nil || strings.ToLower(roundTrip) != asciiName {
		return NormalizedDomainName{}, fmt.Errorf("%w: domain name does not have a stable IDNA round trip", ErrInvalidInput)
	}
	return NormalizedDomainName{Display: displayName, ASCII: asciiName}, nil
}

func normalizeDomainBase(input string) (NormalizedDomainName, error) {
	name, err := NormalizeDomainName(input)
	if err != nil {
		return NormalizedDomainName{}, err
	}
	if strings.HasPrefix(name.ASCII, "www.") && strings.Contains(name.ASCII[len("www."):], ".") {
		return NormalizeDomainName(name.ASCII[len("www."):])
	}
	return name, nil
}

func trimOneRootSeparator(value string) string {
	if value == "" {
		return value
	}
	last, size := utf8.DecodeLastRuneInString(value)
	switch last {
	case '.', '\u3002', '\uff0e', '\uff61':
		return value[:len(value)-size]
	default:
		return value
	}
}

func containsControl(value string) bool {
	return strings.ContainsRune(value, utf8.RuneError) || strings.IndexFunc(value, unicode.IsControl) >= 0
}

func normalizeDocumentRoot(value string) (string, error) {
	normalized, err := hostingpath.NormalizeDocumentRoot(value)
	if err != nil {
		return "", fmt.Errorf("%w: document root is not a canonical account-relative path", ErrInvalidInput)
	}
	return normalized, nil
}

// NormalizeRedirectURL converts one absolute HTTPS redirect target into the
// stable representation shared by persistence and generated configuration.
func NormalizeRedirectURL(value string) (normalized string, asciiHost string, port string, err error) {
	target := strings.TrimSpace(value)
	if target == "" || len(target) > 2048 || containsControl(target) {
		return "", "", "", fmt.Errorf("%w: redirect target is empty, too long, or contains control characters", ErrInvalidInput)
	}
	parsed, parseErr := url.Parse(target)
	if parseErr != nil || parsed.Opaque != "" || !parsed.IsAbs() {
		return "", "", "", fmt.Errorf("%w: redirect target must be an absolute URL", ErrInvalidInput)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", "", "", fmt.Errorf("%w: redirect target must use HTTPS", ErrInvalidInput)
	}
	if parsed.User != nil || parsed.Fragment != "" || parsed.Hostname() == "" {
		return "", "", "", fmt.Errorf("%w: redirect target must not contain credentials or a fragment", ErrInvalidInput)
	}
	if containsControl(parsed.Path) {
		return "", "", "", fmt.Errorf("%w: redirect path contains control characters", ErrInvalidInput)
	}
	decodedQuery, queryErr := url.QueryUnescape(parsed.RawQuery)
	if queryErr != nil || containsControl(decodedQuery) {
		return "", "", "", fmt.Errorf("%w: redirect query is malformed or contains control characters", ErrInvalidInput)
	}

	host := parsed.Hostname()
	if address, addressErr := netip.ParseAddr(host); addressErr == nil {
		asciiHost = address.String()
	} else {
		normalizedHost, normalizeErr := NormalizeDomainName(host)
		if normalizeErr != nil {
			return "", "", "", fmt.Errorf("%w: redirect host: %v", ErrInvalidInput, normalizeErr)
		}
		asciiHost = normalizedHost.ASCII
	}
	port = parsed.Port()
	if port != "" {
		portNumber, portErr := strconv.Atoi(port)
		if portErr != nil || portNumber < 1 || portNumber > 65_535 {
			return "", "", "", fmt.Errorf("%w: redirect target port is outside the valid range", ErrInvalidInput)
		}
	}
	if port == "443" {
		port = ""
	}
	if port == "" {
		if strings.Contains(asciiHost, ":") {
			parsed.Host = "[" + asciiHost + "]"
		} else {
			parsed.Host = asciiHost
		}
	} else {
		parsed.Host = net.JoinHostPort(asciiHost, port)
	}
	parsed.Scheme = "https"
	return parsed.String(), asciiHost, port, nil
}

func normalizeRedirectURL(value string) (normalized string, asciiHost string, port string, err error) {
	return NormalizeRedirectURL(value)
}
