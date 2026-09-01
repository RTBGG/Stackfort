#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

[[ "${EUID:-$(id -u)}" -eq 0 ]] || {
  printf 'Run the bootstrap qualification as root.\n' >&2
  exit 1
}

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bootstrap="$script_directory/install.sh"
version='1.2.3-test.1'
bundle="stackfort-$version-linux-amd64"
archive="$bundle.tar.gz"
workspace="$(mktemp -d /var/tmp/stackfort-bootstrap-test.XXXXXXXX)"
cleanup() {
  case "$workspace" in
    /var/tmp/stackfort-bootstrap-test.*) rm -rf -- "$workspace" ;;
    *) printf 'Refusing unsafe bootstrap-test cleanup: %s\n' "$workspace" >&2 ;;
  esac
}
trap cleanup EXIT

fixture="$workspace/fixture"
payload="$workspace/payload/$bundle"
result="$workspace/result"
mkdir -p "$fixture" "$payload/bin"
cat >"$payload/bin/stackfort-installer" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$@" >"$STACKFORT_BOOTSTRAP_TEST_RESULT"
printf 'Stackfort installation: complete\nAlready installed: false\n'
EOF
chmod 0755 "$payload/bin/stackfort-installer"
tar --sort=name --owner=0 --group=0 --numeric-owner -C "$workspace/payload" -czf "$fixture/$archive" "$bundle"
(cd "$fixture" && sha256sum "$archive" >SHA256SUMS)
chmod 0755 "$fixture"
chmod 0644 "$fixture/SHA256SUMS" "$fixture/$archive"

expect_failure() {
  local expected="$1"
  shift
  if "$@" >"$workspace/failure.out" 2>&1; then
    printf 'Bootstrap qualification expected failure: %s\n' "$expected" >&2
    exit 1
  fi
  grep -Fq "$expected" "$workspace/failure.out" || {
    cat "$workspace/failure.out" >&2
    printf 'Bootstrap qualification omitted expected error: %s\n' "$expected" >&2
    exit 1
  }
}

expect_failure 'restricted to explicit bootstrap qualification' \
  env STACKFORT_VERSION="$version" STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" \
  bash "$bootstrap"

env STACKFORT_VERSION="$version" STACKFORT_BOOTSTRAP_TESTING=1 \
  STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" STACKFORT_BOOTSTRAP_TEST_RESULT="$result" \
  bash "$bootstrap" >"$workspace/success.out"
grep -Fq 'Installing Stackfort 1.2.3-test.1 from its verified GitHub release' "$workspace/success.out"
mapfile -t arguments <"$result"
[[ "${#arguments[@]}" -eq 4 ]]
[[ "${arguments[0]}" == install ]]
[[ "${arguments[1]}" == --source-dir=/var/tmp/stackfort-install.*"/$bundle" ]]
[[ "${arguments[2]}" == --yes && "${arguments[3]}" == --format=text ]]

chmod 0775 "$fixture"
expect_failure 'local release fixture has unsafe metadata' \
  env STACKFORT_VERSION="$version" STACKFORT_BOOTSTRAP_TESTING=1 \
  STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" bash "$bootstrap"
chmod 0755 "$fixture"

cp "$fixture/SHA256SUMS" "$workspace/checksums"
printf '%064d  %s\n' 0 "$archive" >>"$fixture/SHA256SUMS"
expect_failure 'archive has no unique release checksum' \
  env STACKFORT_VERSION="$version" STACKFORT_BOOTSTRAP_TESTING=1 \
  STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" bash "$bootstrap"
mv "$workspace/checksums" "$fixture/SHA256SUMS"

bad_bundle="$workspace/bad/$bundle"
mkdir -p "$bad_bundle/bin"
ln -s /bin/sh "$bad_bundle/bin/stackfort-installer"
tar --sort=name --owner=0 --group=0 --numeric-owner -C "$workspace/bad" \
  -czf "$fixture/$archive" "$bundle"
(cd "$fixture" && sha256sum "$archive" >SHA256SUMS)
expect_failure 'release archive contains a link or special file' \
  env STACKFORT_VERSION="$version" STACKFORT_BOOTSTRAP_TESTING=1 \
  STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" bash "$bootstrap"

grep -Fq "readonly repository='RTBGG/stackfort'" "$bootstrap"
grep -Fq "readonly release_base=\"https://github.com/\$repository/releases/download/\$tag\"" "$bootstrap"
grep -Fq -- "--proto '=https' --tlsv1.2" "$bootstrap"

printf 'STACKFORT_QUALIFICATION bootstrap-production-lock=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-local-fixture=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-archive-boundary=passed\n'
