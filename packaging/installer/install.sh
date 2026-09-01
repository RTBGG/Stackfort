#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

readonly repository='RTBGG/stackfort'
readonly journal='/var/lib/stackfort-installer/install-state.json'
working_directory=''

fail() {
  printf 'Stackfort bootstrap: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n "$working_directory" ]]; then
    case "$working_directory" in
      /var/tmp/stackfort-install.*) rm -rf -- "$working_directory" ;;
      *) printf 'Stackfort bootstrap refused unsafe cleanup path: %s\n' "$working_directory" >&2 ;;
    esac
  fi
}
trap cleanup EXIT

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  fail 'run this bootstrap as root (for example, pipe it to sudo bash)'
fi
if [[ "$(uname -s)" != 'Linux' ]]; then
  fail 'the installer supports Linux only'
fi
case "$(uname -m)" in
  x86_64 | amd64) architecture='amd64' ;;
  *) fail 'the initial installer supports amd64 only' ;;
esac
for command in awk curl install mktemp realpath sed sha256sum stat tar; do
  command -v "$command" >/dev/null 2>&1 || fail "required command is unavailable: $command"
done

version="${STACKFORT_VERSION:-}"
test_fixture="${STACKFORT_BOOTSTRAP_TEST_FIXTURE:-}"
if [[ -n "$test_fixture" ]]; then
  [[ "${STACKFORT_BOOTSTRAP_TESTING:-}" == '1' ]] ||
    fail 'the local release fixture is restricted to explicit bootstrap qualification'
  [[ "$test_fixture" == /* && -d "$test_fixture" && ! -L "$test_fixture" ]] ||
    fail 'the local release fixture must be an absolute real directory'
  resolved_fixture="$(realpath -e -- "$test_fixture")"
  [[ "$resolved_fixture" == "$test_fixture" ]] ||
    fail 'the local release fixture path must be canonical'
  read -r fixture_uid fixture_gid fixture_mode < <(stat -Lc '%u %g %a' -- "$test_fixture")
  if [[ "$fixture_uid" != '0' || "$fixture_gid" != '0' ||
        $((8#$fixture_mode & 8#022)) -ne 0 ]]; then
    fail 'the local release fixture has unsafe metadata'
  fi
  readonly test_fixture
fi
if [[ -z "$version" && -e "$journal" ]]; then
  [[ -f "$journal" && ! -L "$journal" ]] || fail 'the existing installation journal is not a regular file'
  read -r journal_uid journal_gid journal_mode journal_size < <(stat -Lc '%u %g %a %s' -- "$journal")
  if [[ "$journal_uid" != '0' || "$journal_gid" != '0' || "$journal_mode" != '600' ||
        "$journal_size" -gt 1048576 ]]; then
    fail 'the existing installation journal has unsafe metadata'
  fi
  version="$(awk -F'"' '$2 == "version" { print $4 }' "$journal")"
  [[ $(printf '%s\n' "$version" | sed '/^$/d' | wc -l) -eq 1 ]] ||
    fail 'the existing installation journal has no unique version'
  printf 'Resuming the release pinned by the existing installation journal.\n'
fi
if [[ -z "$version" ]]; then
  [[ -z "$test_fixture" ]] || fail 'bootstrap qualification requires an explicit release version or existing journal'
  latest_url="$(curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
    --output /dev/null --write-out '%{url_effective}' \
    "https://github.com/$repository/releases/latest")"
  latest_url="${latest_url%/}"
  version="${latest_url##*/}"
fi
version="${version#v}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  fail 'the selected release version is invalid'
fi

readonly version
readonly tag="v$version"
readonly bundle="stackfort-$version-linux-$architecture"
readonly archive="$bundle.tar.gz"
readonly release_base="https://github.com/$repository/releases/download/$tag"

umask 077
working_directory="$(mktemp -d /var/tmp/stackfort-install.XXXXXXXX)"
readonly working_directory
curl_options=(--fail --silent --show-error --location --proto '=https' --tlsv1.2)
fetch_release_asset() {
  local name="$1"
  local destination="$working_directory/$name"
  if [[ -z "$test_fixture" ]]; then
    curl "${curl_options[@]}" --output "$destination" "$release_base/$name"
    return
  fi

  local source="$test_fixture/$name"
  [[ -f "$source" && ! -L "$source" ]] || fail "local release fixture asset is invalid: $name"
  read -r asset_uid asset_gid asset_mode < <(stat -Lc '%u %g %a' -- "$source")
  if [[ "$asset_uid" != '0' || "$asset_gid" != '0' ||
        $((8#$asset_mode & 8#022)) -ne 0 ]]; then
    fail "local release fixture asset has unsafe metadata: $name"
  fi
  install -m 0600 -- "$source" "$destination"
}
fetch_release_asset SHA256SUMS
fetch_release_asset "$archive"

checksum="$(awk -v plain="$archive" -v dotted="./$archive" '
  $2 == plain || $2 == dotted { if (found) exit 2; print $1; found = 1 }
  END { if (!found) exit 1 }
' "$working_directory/SHA256SUMS")" || fail 'the archive has no unique release checksum'
if [[ ! "$checksum" =~ ^[0-9a-f]{64}$ ]]; then
  fail 'the release checksum is malformed'
fi
printf '%s  %s\n' "$checksum" "$archive" |
  (cd "$working_directory" && sha256sum --check --strict -) || fail 'release checksum verification failed'

while IFS= read -r entry; do
  entry="${entry%/}"
  if [[ "$entry" != "$bundle" && "$entry" != "$bundle/"* ]]; then
    fail "release archive contains an unexpected path: $entry"
  fi
  case "/$entry/" in
    */../*) fail "release archive contains parent traversal: $entry" ;;
  esac
done < <(tar -tzf "$working_directory/$archive")
while IFS= read -r member; do
  case "${member:0:1}" in
    - | d) ;;
    *) fail 'release archive contains a link or special file' ;;
  esac
done < <(tar -tvzf "$working_directory/$archive")

tar --extract --gzip --file "$working_directory/$archive" --directory "$working_directory" \
  --no-same-owner --no-same-permissions
source_directory="$working_directory/$bundle"
[[ -x "$source_directory/bin/stackfort-installer" ]] || fail 'release installer is unavailable'

printf 'Installing Stackfort %s from its verified GitHub release...\n' "$version"
"$source_directory/bin/stackfort-installer" install \
  --source-dir="$source_directory" --yes --format=text
