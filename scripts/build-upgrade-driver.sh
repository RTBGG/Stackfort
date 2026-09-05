#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
version="${PRIOR_VERSION:?Set PRIOR_VERSION to a supported published predecessor}"
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$ ]]
if [[ -n "${STACKFORT_UPGRADE_REHEARSAL_COMMIT:-}" ]]; then
  # Explicit local rehearsal before any prior public tag exists.
  [[ "$STACKFORT_UPGRADE_REHEARSAL_COMMIT" =~ ^[0-9a-f]{40}$ ]]
  commit="$(git rev-parse "$STACKFORT_UPGRADE_REHEARSAL_COMMIT^{commit}")"
else
  commit="$(git rev-parse "v$version^{commit}")"
fi
[[ "$commit" =~ ^[0-9a-f]{40}$ ]]
workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT
git archive "$commit" | tar -xf - -C "$workspace"
cp internal/updateapply/upgrade_matrix_linux_test.go "$workspace/internal/updateapply/"
output="$repository_root/infra/host-tests/work/upgrade-drivers"
mkdir -p "$output"
test ! -e "$output/upgrade-$version.test"
cd "$workspace"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -tags=integration -c \
  -ldflags "-X github.com/RTBGG/stackfort/internal/buildinfo.Version=$version -X github.com/RTBGG/stackfort/internal/buildinfo.Commit=$commit" \
  -o "$output/upgrade-$version.test" ./internal/updateapply
printf 'Prior-source driver created at %s/upgrade-%s.test\n' "$output" "$version"
