#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
export LC_ALL=C

[[ "${EUID:-$(id -u)}" -eq 0 ]] || {
  printf 'Run the bootstrap qualification as root.\n' >&2
  exit 1
}
# Exercise the fixed production paths in a private mount namespace. There is no
# production journal-path or OS-detection override for the test to enable.
if [[ $# -eq 0 ]]; then
  exec unshare --mount --fork -- bash "$0" --isolated
fi
[[ $# -eq 1 && "$1" == '--isolated' &&
   "$(readlink /proc/self/ns/mnt)" != "$(readlink /proc/1/ns/mnt)" ]] || {
  printf 'Bootstrap qualification requires its private mount namespace.\n' >&2
  exit 1
}
mount --make-rprivate /

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bootstrap="$script_directory/install.sh"
version='1.2.3-test.1'
workspace="$(mktemp -d /var/tmp/stackfort-bootstrap-test.XXXXXXXX)"
cleanup() {
  case "$workspace" in
    /var/tmp/stackfort-bootstrap-test.*) rm -rf -- "$workspace" ;;
    *) printf 'Refusing unsafe bootstrap-test cleanup: %s\n' "$workspace" >&2 ;;
  esac
}
trap cleanup EXIT

fixture="$workspace/fixture"
result="$workspace/result"
state="$workspace/var-lib/stackfort-installer"
mkdir -p "$fixture" "$workspace/payload" "$workspace/var-lib"
mount --bind "$workspace/var-lib" /var/lib
printf 'ID=debian\nVERSION_ID="13"\n' >"$workspace/os-release"
mount --bind "$workspace/os-release" /etc/os-release

cat >"$workspace/fixture-installer" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >>"$STACKFORT_BOOTSTRAP_TEST_RESULT"
case "$1" in
  preflight)
    [[ $# -eq 2 && "$2" == '--format=text' ]]
    printf 'Fixture read-only preflight\n'
    exit "${STACKFORT_BOOTSTRAP_TEST_PREFLIGHT_EXIT:-0}"
    ;;
  install)
    [[ $# -eq 4 && "$2" == --source-dir=* && "$3" == '--yes' && "$4" == '--format=text' ]]
    ;;
  onboard)
    [[ $# -eq 5 && "$2" == --source-dir=* && "$3" == --archive=* &&
       "$4" == --attestations=* && "$5" == --version=* ]]
    source="${2#--source-dir=}"
    archive="${3#--archive=}"
    attestations="${4#--attestations=}"
    [[ -f "$archive" && -f "$attestations" && "$archive" != "$source/"* && "$attestations" != "$source/"* ]]
    # A test double checks invocation, never offers a production verifier skip.
    if [[ "${STACKFORT_BOOTSTRAP_TEST_HANDLER_FAILURE:-}" == 1 ]]; then
      printf 'Fixture authoritative native validation rejected remaining state.\n' >&2
      exit 1
    fi
    ;;
  *) exit 1 ;;
esac
printf 'Fixture installer completed selected handler.\n'
EOF

make_fixture() {
  local selected="$1"
  bundle="stackfort-$selected-linux-amd64"
  archive="$bundle.tar.gz"
  local payload="$workspace/payload/$bundle"
  mkdir -p "$payload/bin"
  install -m 0755 "$workspace/fixture-installer" "$payload/bin/stackfort-installer"
  tar --sort=name --owner=0 --group=0 --numeric-owner -C "$workspace/payload" -czf "$fixture/$archive" "$bundle"
  (cd "$fixture" && sha256sum "$archive" >SHA256SUMS)
  printf '{"fixture":"not a production attestation"}\n' >"$fixture/build-attestation.jsonl"
  chmod 0755 "$fixture"
  chmod 0644 "$fixture/SHA256SUMS" "$fixture/$archive" "$fixture/build-attestation.jsonl"
}

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

expect_no_match() {
  if grep -Eq -- "$1" "$2"; then
    printf 'Bootstrap qualification found a forbidden match: %s\n' "$1" >&2
    exit 1
  fi
}

run_fixture() {
  : >"$result"
  env STACKFORT_VERSION="$version" STACKFORT_BOOTSTRAP_TESTING=1 \
    STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" STACKFORT_BOOTSTRAP_TEST_RESULT="$result" \
    "$@" bash "$bootstrap"
}

write_install_journal() {
  install -d -m 0700 "$state"
  printf '{\n  "version": "%s",\n  "status": "complete"\n}\n' "$1" >"$state/install-state.json"
  chmod 0600 "$state/install-state.json"
}

write_native_journal() {
  install -d -m 0700 "$state"
  # Only download-hint fields are mocked. Actual native structural/binding
  # validation is covered by the Go handler tests, not this fixture executable.
  printf '{\n  "plan": {\n    "version": "%s"\n  },\n  "phase": "ready"\n}\n' "$1" >"$state/storage-state.json"
  chmod 0600 "$state/storage-state.json"
}

make_fixture "$version"
expect_failure 'restricted to explicit bootstrap qualification' \
  env STACKFORT_VERSION="$version" STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" bash "$bootstrap"
run_fixture >"$workspace/success.out"
grep -Fq 'Preparing Stackfort 1.2.3-test.1 from its checksum-verified release archive' "$workspace/success.out"
grep -Fq 'EXPERIMENTAL: Use only a fresh disposable server without user data.' "$workspace/success.out"
mapfile -t arguments <"$result"
[[ ${#arguments[@]} -eq 6 && "${arguments[0]}" == preflight && "${arguments[1]}" == --format=text ]]
[[ "${arguments[2]}" == install && "${arguments[3]}" == --source-dir=/var/tmp/stackfort-install.*"/$bundle" ]]
[[ "${arguments[4]}" == --yes && "${arguments[5]}" == --format=text ]]

run_fixture STACKFORT_BOOTSTRAP_TEST_PREFLIGHT_EXIT=2 >"$workspace/native.out"
mapfile -t arguments <"$result"
[[ ${#arguments[@]} -eq 7 && "${arguments[0]}" == preflight && "${arguments[2]}" == onboard ]]
[[ "${arguments[3]}" == --source-dir=/var/tmp/stackfort-install.*"/$bundle" ]]
[[ "${arguments[4]}" == --archive=/var/tmp/stackfort-install.*"/$archive" ]]
[[ "${arguments[5]}" == --attestations=/var/tmp/stackfort-install.*/build-attestation.jsonl ]]
[[ "${arguments[6]}" == "--version=$version" ]]
expect_no_match '^--yes$' "$result"
expect_failure 'preflight inspection failed' run_fixture STACKFORT_BOOTSTRAP_TEST_PREFLIGHT_EXIT=1
[[ "$(head -n 1 "$result")" == preflight && "$(wc -l <"$result")" -eq 2 ]]
printf 'ID=ubuntu\nVERSION_ID="26.04"\n' >"$workspace/os-release"
expect_failure 'automatic native preparation is limited' run_fixture STACKFORT_BOOTSTRAP_TEST_PREFLIGHT_EXIT=2
expect_no_match '^(install|onboard)$' "$result"
printf 'ID=debian\nVERSION_ID="13"\n' >"$workspace/os-release"

make_fixture '0.1.0-beta.5'
env STACKFORT_VERSION= STACKFORT_BOOTSTRAP_TESTING=1 STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" \
  STACKFORT_BOOTSTRAP_TEST_RESULT="$result" bash "$bootstrap" >"$workspace/default.out"
grep -Fq 'explicitly pinned experimental release 0.1.0-beta.5' "$workspace/default.out"
make_fixture "$version"
for invalid in '01.2.3' '1.02.3' '1.2.03' '1.2.3-01' '1.2.3-a..b' '1.2.3+' '1.2.3+..' 'latest' 'vv1.2.3' '1.2.3/evil'; do
  expect_failure 'selected release version is invalid' \
    env STACKFORT_VERSION="$invalid" STACKFORT_BOOTSTRAP_TESTING=1 \
    STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" bash "$bootstrap"
done
env STACKFORT_VERSION="v$version" STACKFORT_BOOTSTRAP_TESTING=1 \
  STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" STACKFORT_BOOTSTRAP_TEST_RESULT="$result" bash "$bootstrap" >/dev/null

write_install_journal "$version"
: >"$result"
env STACKFORT_VERSION= STACKFORT_BOOTSTRAP_TESTING=1 STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" \
  STACKFORT_BOOTSTRAP_TEST_RESULT="$result" STACKFORT_BOOTSTRAP_TEST_PREFLIGHT_EXIT=1 bash "$bootstrap" >/dev/null
mapfile -t arguments <"$result"
[[ ${#arguments[@]} -eq 4 && "${arguments[0]}" == install ]]
write_native_journal "$version"
run_fixture >"$workspace/resume.out"
mapfile -t arguments <"$result"
[[ ${#arguments[@]} -eq 5 && "${arguments[0]}" == onboard ]]
expect_no_match '^--yes$' "$result"
expect_failure 'authoritative native validation rejected remaining state' \
  run_fixture STACKFORT_BOOTSTRAP_TEST_HANDLER_FAILURE=1
expect_no_match '^install$' "$result"
write_native_journal '9.9.9'
expect_failure 'conflicts with the existing native storage journal' run_fixture
write_native_journal "$version"
expect_failure 'conflicts with the existing installation journal' \
  env STACKFORT_VERSION=9.9.9 STACKFORT_BOOTSTRAP_TESTING=1 STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" bash "$bootstrap"
rm -- "$state/install-state.json"
: >"$result"
env STACKFORT_VERSION= STACKFORT_BOOTSTRAP_TESTING=1 STACKFORT_BOOTSTRAP_TEST_FIXTURE="$fixture" \
  STACKFORT_BOOTSTRAP_TEST_RESULT="$result" bash "$bootstrap" >/dev/null
[[ "$(head -n 1 "$result")" == onboard ]]

printf '{\n  "phase": "ready"\n}\n' >"$state/storage-state.json"
expect_failure 'no unique canonical version' run_fixture
printf '{\n  "version": "%s",\n  "version": "%s"\n}\n' "$version" "$version" >"$state/storage-state.json"
expect_failure 'no unique canonical version' run_fixture
write_native_journal "$version"
chmod 0644 "$state/storage-state.json"
expect_failure 'journal has unsafe metadata' run_fixture
chmod 0600 "$state/storage-state.json"
ln "$state/storage-state.json" "$workspace/journal-hardlink"
expect_failure 'journal has unsafe metadata' run_fixture
rm -- "$workspace/journal-hardlink" "$state/storage-state.json"
ln -s "$workspace/missing" "$state/storage-state.json"
expect_failure 'journal is not a regular file' run_fixture
rm -- "$state/storage-state.json"
ln -s "$workspace/missing" "$state/native-runtime-intent.json"
expect_failure 'partial native preparation evidence exists' run_fixture
rm -- "$state/native-runtime-intent.json"
mkdir "$state/resume-source"
expect_failure 'partial native preparation evidence exists' run_fixture
rmdir "$state/resume-source"
chmod 0755 "$state"
expect_failure 'state directory has unsafe metadata' run_fixture
chmod 0700 "$state"

chmod 0775 "$fixture"
expect_failure 'local release fixture has unsafe metadata' run_fixture
chmod 0755 "$fixture"
mv "$fixture/build-attestation.jsonl" "$workspace/attestation"
expect_failure 'local release fixture asset is invalid: build-attestation.jsonl' run_fixture
mv "$workspace/attestation" "$fixture/build-attestation.jsonl"
chmod 0666 "$fixture/build-attestation.jsonl"
expect_failure 'local release fixture asset has unsafe metadata: build-attestation.jsonl' run_fixture
chmod 0644 "$fixture/build-attestation.jsonl"
cp "$fixture/SHA256SUMS" "$workspace/checksums"
printf '%064d  %s\n' 0 "$archive" >>"$fixture/SHA256SUMS"
expect_failure 'archive has no unique release checksum' run_fixture
mv "$workspace/checksums" "$fixture/SHA256SUMS"
printf 'not a gzip archive\n' >"$fixture/$archive"
(cd "$fixture" && sha256sum "$archive" >SHA256SUMS)
expect_failure 'release archive inventory failed' run_fixture
[[ ! -s "$result" ]]
make_fixture "$version"
bad_bundle="$workspace/bad/$bundle"
mkdir -p "$bad_bundle/bin"
ln -s /bin/sh "$bad_bundle/bin/stackfort-installer"
tar --sort=name --owner=0 --group=0 --numeric-owner -C "$workspace/bad" -czf "$fixture/$archive" "$bundle"
(cd "$fixture" && sha256sum "$archive" >SHA256SUMS)
expect_failure 'release archive contains a link or special file' run_fixture

grep -Fq "readonly repository='RTBGG/stackfort'" "$bootstrap"
grep -Fq "readonly default_version='0.1.0-beta.5'" "$bootstrap"
grep -Fq "readonly release_base=\"https://github.com/\$repository/releases/download/\$tag\"" "$bootstrap"
grep -Fq -- "--proto '=https' --tlsv1.2" "$bootstrap"
if grep -Fq "\"https://github.com/\$repository/releases/latest\"" "$bootstrap"; then
  printf 'Bootstrap qualification found the forbidden dynamic latest channel.\n' >&2
  exit 1
fi
printf 'STACKFORT_QUALIFICATION bootstrap-production-lock=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-pinned-experimental-channel=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-local-fixture=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-native-routing=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-pinned-rerun=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-native-evidence-failclosed=passed\n'
printf 'STACKFORT_QUALIFICATION bootstrap-archive-boundary=passed\n'
