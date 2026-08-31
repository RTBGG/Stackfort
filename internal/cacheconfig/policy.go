// SPDX-License-Identifier: AGPL-3.0-or-later

// Package cacheconfig defines Stackfort's closed Vinyl Cache policy. Customer
// input selects an enum and safe purge scope; it never becomes VCL source.
package cacheconfig

import (
	"errors"
	"regexp"
	"strings"
)

const (
	VinylVersion      = "9.0.1"
	VinylSourceSHA256 = "2e8ec67cd213ea6864c763939d64912025557342fad2a5ffda6c7c5b59bdeb17"
	PackageName       = "vinyl-cache"
	ServiceName       = "vinyl.service"
	VCLPath           = "/etc/vinyl-cache/stackfort.vcl"
	SecretPath        = "/etc/vinyl-cache/secret"
	WorkDirectory     = "/var/lib/vinyl-cache"
	CacheAddress      = "127.0.0.1:6081"
	ManagementAddress = "127.0.0.1:6082"
	// Port 9000 is already classified http_port_t by the stock EL10 SELinux
	// policy. Stackfort disables the vendor-wide TCP PHP-FPM service and uses
	// account-scoped Unix sockets, leaving this loopback listener unambiguous.
	OriginAddress    = "127.0.0.1:9000"
	MaximumPurgePath = 512
)

type Preset string

const (
	PresetDisabled      Preset = "disabled"
	PresetRespectOrigin Preset = "respect_origin"
	PresetWordPress     Preset = "wordpress"
)

var purgePathPattern = regexp.MustCompile(`^/[A-Za-z0-9/_.~,!$&()+;=:@%-]{0,511}$`)

func NormalizePreset(value Preset) (Preset, error) {
	if value == "" {
		return PresetDisabled, nil
	}
	if value != PresetDisabled && value != PresetRespectOrigin && value != PresetWordPress {
		return "", errors.New("unsupported cache preset")
	}
	return value, nil
}

// NormalizePurgePath accepts only an exact path prefix. The caller appends a
// renderer-owned boundary expression, so regex and query injection are absent.
func NormalizePurgePath(value string) (string, error) {
	if value == "" {
		value = "/"
	}
	if value != strings.TrimSpace(value) || !purgePathPattern.MatchString(value) ||
		strings.Contains(value, "//") || strings.Contains("/"+value+"/", "/../") ||
		strings.Contains("/"+value+"/", "/./") {
		return "", errors.New("invalid cache purge path")
	}
	return value, nil
}

// ManagedVCL returns the fixed Vinyl policy installed by the native package.
// The edge overwrites X-Stackfort-Cache-Preset. Vinyl still independently
// bypasses all authenticated, cookie-bearing, unsafe-method, and sensitive-path
// traffic and never stores Set-Cookie/private/no-store responses.
func ManagedVCL() string {
	return `vcl 4.1;

backend default {
    .host = "127.0.0.1";
    .port = "9000";
    .connect_timeout = 1s;
    .first_byte_timeout = 30s;
    .between_bytes_timeout = 5s;
}

sub vcl_recv {
    if (req.http.X-Stackfort-Cache-Preset != "respect_origin" &&
        req.http.X-Stackfort-Cache-Preset != "wordpress") {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if (req.method != "GET" && req.method != "HEAD") {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if ((req.http.Content-Length && req.http.Content-Length != "0") || req.http.Transfer-Encoding) {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if (req.http.Authorization || req.http.Cookie) {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    if (req.url ~ "^/(?:wp-admin(?:/|$)|wp-login\\.php(?:/|\\?|$)|admin(?:/|$)|login(?:/|$)|account(?:/|$)|cart(?:/|$)|checkout(?:/|$)|api(?:/|$))") {
        set req.http.X-Stackfort-Cache-Decision = "BYPASS";
        return (pass);
    }
    return (hash);
}

sub vcl_hash {
    hash_data(req.http.host);
    hash_data(req.url);
}

sub vcl_backend_response {
    set beresp.grace = 30s;
    if (beresp.http.Set-Cookie || beresp.http.Cache-Control ~ "(?i)(private|no-store|no-cache)" ||
        beresp.status < 200 || beresp.status >= 400) {
        set beresp.uncacheable = true;
        set beresp.ttl = 0s;
        return (deliver);
    }
    if (bereq.http.X-Stackfort-Cache-Preset == "respect_origin" &&
        beresp.http.Cache-Control !~ "(?i)(s-maxage|max-age)") {
        set beresp.uncacheable = true;
        set beresp.ttl = 0s;
        return (deliver);
    }
    if (bereq.http.X-Stackfort-Cache-Preset == "wordpress" && beresp.ttl <= 0s) {
        set beresp.ttl = 120s;
    }
}

sub vcl_deliver {
    if (req.http.X-Stackfort-Cache-Decision == "BYPASS") {
        set resp.http.X-Stackfort-Cache = "BYPASS";
    } else if (obj.hits > 0) {
        set resp.http.X-Stackfort-Cache = "HIT";
    } else {
        set resp.http.X-Stackfort-Cache = "MISS";
    }
    unset resp.http.Via;
    unset resp.http.X-Varnish;
}
`
}
