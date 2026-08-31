#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
awk -F '|' 'BEGIN { ok=1 } /^#/ { next } NF { if (NF != 4 || $1 !~ /^[a-z0-9-]+$/ || $2 !~ /^[0-9]+\.[0-9]+\.[0-9]+$/ || $3 !~ /^[0-9a-f]{64}$/ || $4 !~ /^https:\/\//) ok=0; count++ } END { exit !(ok && count==1) }' "$directory/sources.lock"
awk -F '|' 'BEGIN { ok=1 } /^#/ { next } NF { if (NF != 4 || $3 != "amd64" || ($4 != "deb" && $4 != "rpm")) ok=0; seen[$1]++ } END { exit !(ok && seen["debian"]==1 && seen["ubuntu"]==1 && seen["rocky"]==1) }' "$directory/targets.lock"
(
  cd "$directory"
  sha256sum --check --strict managed-vcl.sha256 >/dev/null
)
if command -v go >/dev/null 2>&1; then
  cmp -s "$directory/stackfort.vcl" <(go run "$directory/../../cmd/stackfort-cache-policy")
fi
printf 'Vinyl source, target, and managed VCL locks are consistent.\n'
