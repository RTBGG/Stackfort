#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

# Local, explicitly unpublished rehearsal. Production releases use build-release.sh.
set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
baseline="${BASELINE_COMMIT:?Set the exact prior source commit}"
[[ "$baseline" =~ ^[0-9a-f]{40}$ ]]
test "$(git rev-parse "$baseline^{commit}")" = "$baseline"
output="$(mktemp -d "$repository_root/infra/host-tests/work/upgrade-rehearsal.XXXXXX")"
workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT
git archive "$baseline" | tar -xf - -C "$workspace"
cp internal/updateapply/upgrade_matrix_linux_test.go "$workspace/internal/updateapply/"

for version in 0.1.0-beta.1 0.1.0-beta.2; do
  stage="$output/stackfort-$version-linux-amd64"
  test ! -e "$stage"
  extract="$workspace/extract-$version"
  mkdir -p "$extract"
  tar -xzf dist/stackfort-0.0.0-dev-linux-amd64.tar.gz -C "$extract"
  mv "$extract/stackfort-0.0.0-dev-linux-amd64" "$stage"
  source_root="$repository_root"
  commit="$(git rev-parse HEAD)"
  if [[ "$version" == 0.1.0-beta.1 ]]; then source_root="$workspace"; commit="$baseline"; fi
  flags="-s -w -buildid= -X github.com/RTBGG/stackfort/internal/buildinfo.Version=$version -X github.com/RTBGG/stackfort/internal/buildinfo.Commit=$commit -X github.com/RTBGG/stackfort/internal/buildinfo.BuildDate=2026-09-05T00:00:00Z"
  for binary in stackfort-api stackfort-agent stackfort-installer stackfort-updater; do
    (cd "$source_root"; GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$flags" -o "$stage/bin/$binary" "./cmd/$binary")
  done
  if [[ "$version" == 0.1.0-beta.1 ]]; then
    (cd "$source_root"; GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -tags=integration -c -ldflags "$flags" -o "$output/upgrade-matrix.test" ./internal/updateapply)
  fi
  printf '%s\n' "$version" >"$stage/VERSION"
  printf '%s\n' "$commit" >"$stage/COMMIT"
  # Exercise replacement plus obsolete/new fingerprinted UI assets in the
  # real payload runner, while keeping these markers out of product builds.
  printf '%s\n' "$version" >"$stage/web/upgrade-rehearsal.txt"
  printf '%s\n' "$version" >"$stage/web/assets/upgrade-$version.txt"
  rm -- "$stage/RELEASE-MANIFEST.json"
  go run ./cmd/stackfort-release-manifest --destination "$stage" --version "$version" --architecture amd64 \
    --package-dir "$repository_root/infra/host-tests/work/waf-packages-coraza" \
    --vinyl-package-dir "$repository_root/infra/host-tests/work/vinyl-packages"
  name="stackfort-$version-linux-amd64"
  tar --sort=name --mtime='2026-09-05T00:00:00Z' --owner=0 --group=0 --numeric-owner \
    --mode='u+rwX,go+rX,go-w' --exclude="$name/bin/*" -C "$output" -cf "$output/$name.tar" "$name"
  tar --sort=name --mtime='2026-09-05T00:00:00Z' --owner=0 --group=0 --numeric-owner \
    --mode=0755 -C "$output" -rf "$output/$name.tar" \
    "$name/bin/stackfort-api" "$name/bin/stackfort-agent" "$name/bin/stackfort-installer" \
    "$name/bin/stackfort-updater" "$name/bin/stackfort-gh" "$name/bin/stackfort-trivy"
  gzip -n -c "$output/$name.tar" >"$output/$name.tar.gz"
done
printf 'Unpublished rehearsal archives and prior-source driver: %s\n' "$output"
