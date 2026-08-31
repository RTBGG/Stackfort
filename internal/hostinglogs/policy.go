// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostinglogs defines the shared, fixed storage and retention policy
// for account-scoped web-server logs. It deliberately accepts no caller-owned
// filesystem path.
package hostinglogs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
)

const (
	RootDirectory      = "/var/log/stackfort/accounts"
	FormatName         = "stackfort_redacted"
	RetentionDays      = 7
	MaximumRotations   = 7
	MaximumActiveBytes = 8 << 20
)

// DomainFile derives an opaque filename from server-validated account and
// domain identities. The ASCII domain is never exposed in the log tree.
func DomainFile(accountID, domainASCII, kind string) string {
	digest := sha256.Sum256([]byte(domainASCII))
	name := "domain-" + hex.EncodeToString(digest[:]) + "." + kind + ".log"
	return path.Join(RootDirectory, accountID, name)
}

// RetentionConfiguration is installed into logrotate's closed configuration
// directory. Numbered rotation is explicit because the agent uses the active
// and delay-compressed prior file for bounded pagination.
func RetentionConfiguration() string {
	return fmt.Sprintf(`# Managed by Stackfort. Do not edit.
%[1]s/*/*.access.log %[1]s/*/*.error.log {
    daily
    rotate %[2]d
    maxage %[3]d
    maxsize %[4]dM
    missingok
    notifempty
    compress
    delaycompress
    nodateext
    create 0640 root root
    su root root
    sharedscripts
    postrotate
        if [ -s /run/nginx.pid ]; then
            kill -USR1 "$(cat /run/nginx.pid)"
        fi
    endscript
}
`, RootDirectory, MaximumRotations, RetentionDays, MaximumActiveBytes>>20)
}
