#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
export LC_ALL=C

readonly repository='RTBGG/stackfort'
# This is an explicitly selected experimental release, not GitHub's stable
# /releases/latest channel (which does not select prereleases).
readonly default_version='0.1.0-beta.5'
readonly state_directory='/var/lib/stackfort-installer'
readonly journal='/var/lib/stackfort-installer/install-state.json'
readonly storage_journal='/var/lib/stackfort-installer/storage-state.json'
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
for command in awk curl install mktemp realpath sha256sum stat tar; do
  command -v "$command" >/dev/null 2>&1 || fail "required command is unavailable: $command"
done

canonical_version() {
  local value="$1"
  local number='(0|[1-9][0-9]*)'
  local prerelease='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
  local metadata='[0-9A-Za-z-]+'
  local pattern="^$number\\.$number\\.$number(-$prerelease(\\.$prerelease)*)?(\\+$metadata(\\.$metadata)*)?$"
  [[ ${#value} -le 128 && "$value" =~ $pattern ]]
}

version="${STACKFORT_VERSION:-}"
if [[ -n "$version" ]]; then
  version="${version#v}"
  canonical_version "$version" || fail 'the selected release version is invalid'
fi
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
read_journal_version() {
  local path="$1" limit="$2"
  [[ -f "$path" && ! -L "$path" ]] || fail 'an existing installer journal is not a regular file'
  local uid gid mode size links
  read -r uid gid mode size links < <(stat -Lc '%u %g %a %s %h' -- "$path")
  if [[ "$uid" != '0' || "$gid" != '0' || "$mode" != '600' ||
        "$links" != '1' || "$size" -eq 0 || "$size" -gt "$limit" ]]; then
    fail 'an existing installer journal has unsafe metadata'
  fi
  local hint
  # A narrow download hint from the existing indented journal, not authority
  # to resume. The selected Go handler must validate the entire locked journal
  # and its bound receipts before making changes. Never fall back on parse error.
  hint="$(awk -F'"' '
    /^[[:space:]]*"version"[[:space:]]*:/ {
      if ($0 !~ /^[[:space:]]*"version": "[0-9A-Za-z.+-]+",?$/ || ++found != 1) bad = 1
      value = $4
    }
    END { if (bad || found != 1) exit 1; print value }
  ' "$path")" || fail 'an existing installer journal has no unique canonical version'
  canonical_version "$hint" || fail 'an existing installer journal has an invalid version'
  printf '%s\n' "$hint"
}

installation_exists=false
native_exists=false
if [[ -e "$state_directory" || -L "$state_directory" ]]; then
  [[ -d "$state_directory" && ! -L "$state_directory" &&
     "$(realpath -e -- "$state_directory")" == "$state_directory" ]] ||
    fail 'the existing installer state directory is unsafe'
  read -r state_uid state_gid state_mode < <(stat -Lc '%u %g %a' -- "$state_directory")
  [[ "$state_uid" == '0' && "$state_gid" == '0' && "$state_mode" == '700' ]] ||
    fail 'the existing installer state directory has unsafe metadata'
fi
if [[ -e "$journal" || -L "$journal" ]]; then
  installation_exists=true
  pinned_version="$(read_journal_version "$journal" 1048576)" || exit 1
  [[ -z "$version" || "$version" == "$pinned_version" ]] ||
    fail 'the selected release conflicts with the existing installation journal'
  version="$pinned_version"
fi
if [[ -e "$storage_journal" || -L "$storage_journal" ]]; then
  native_exists=true
  pinned_version="$(read_journal_version "$storage_journal" 65536)" || exit 1
  [[ -z "$version" || "$version" == "$pinned_version" ]] ||
    fail 'the selected release conflicts with the existing native storage journal'
  version="$pinned_version"
fi
if [[ "$native_exists" == false ]]; then
  for evidence in "$state_directory"/native-* "$state_directory"/resume-source \
    "$state_directory"/.storage-state-*; do
    [[ ! -e "$evidence" && ! -L "$evidence" ]] ||
      fail 'partial native preparation evidence exists without a storage journal; preserve it for operator recovery'
  done
fi
if [[ "$installation_exists" == true || "$native_exists" == true ]]; then
  printf 'Resuming the release pinned by the existing installer journal(s).\n'
elif [[ -z "$version" ]]; then
  version="$default_version"
  printf 'Selecting the explicitly pinned experimental release %s (not the stable latest channel).\n' "$version"
fi
canonical_version "$version" || fail 'the selected release version is invalid'

readonly version
readonly tag="v$version"
readonly bundle="stackfort-$version-linux-$architecture"
readonly archive="$bundle.tar.gz"
readonly release_base="https://github.com/$repository/releases/download/$tag"

umask 077
working_directory="$(mktemp -d /var/tmp/stackfort-install.XXXXXXXX)"
readonly working_directory
curl_options=(--fail --silent --show-error --location --proto '=https' --tlsv1.2 --proto-redir '=https')
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
# Kept beside the archive, never inside the extracted source tree. Native
# onboarding authenticates this exact archive against GitHub's tag attestation.
fetch_release_asset build-attestation.jsonl

checksum="$(awk -v plain="$archive" -v dotted="./$archive" '
  $2 == plain || $2 == dotted { if (found) exit 2; print $1; found = 1 }
  END { if (!found) exit 1 }
' "$working_directory/SHA256SUMS")" || fail 'the archive has no unique release checksum'
if [[ ! "$checksum" =~ ^[0-9a-f]{64}$ ]]; then
  fail 'the release checksum is malformed'
fi
printf '%s  %s\n' "$checksum" "$archive" |
  (cd "$working_directory" && sha256sum --check --strict -) || fail 'release checksum verification failed'

# Process-substitution failures do not propagate through `while ... done`.
# Complete both inventories successfully into bounded, private regular files
# before trusting any entries or extracting the release.
list_archive() {
  local option="$1" destination="$working_directory/$2"
  (
    ulimit -f 16384 || exit 1
    tar "$option" "$working_directory/$archive"
  ) >"$destination" || fail 'release archive inventory failed or exceeded its size limit'
  [[ -f "$destination" && ! -L "$destination" && -s "$destination" &&
     "$(stat -Lc '%s' -- "$destination")" -le 16777216 ]] ||
    fail 'release archive inventory is empty or unsafe'
}
list_archive -tzf archive-paths.txt
list_archive -tvzf archive-members.txt
archive_entries=0
while IFS= read -r entry; do
  archive_entries=$((archive_entries + 1))
  [[ "$archive_entries" -le 20000 ]] || fail 'release archive has too many entries'
  entry="${entry%/}"
  if [[ "$entry" != "$bundle" && "$entry" != "$bundle/"* ]]; then
    fail "release archive contains an unexpected path: $entry"
  fi
  case "/$entry/" in
    */../*) fail "release archive contains parent traversal: $entry" ;;
  esac
done <"$working_directory/archive-paths.txt"
while IFS= read -r member; do
  case "${member:0:1}" in
    - | d) ;;
    *) fail 'release archive contains a link or special file' ;;
  esac
done <"$working_directory/archive-members.txt"

tar --extract --gzip --file "$working_directory/$archive" --directory "$working_directory" \
  --no-same-owner --no-same-permissions
source_directory="$working_directory/$bundle"
[[ -x "$source_directory/bin/stackfort-installer" ]] || fail 'release installer is unavailable'

printf 'Preparing Stackfort %s from its checksum-verified release archive...\n' "$version"
printf 'EXPERIMENTAL: Use only a fresh disposable server without user data. Keep provider-console and recovery access.\n'

installer="$source_directory/bin/stackfort-installer"
onboard() {
  # Consent is obtained from the controlling root terminal by Go, not from the
  # curl | bash stdin pipe. Generic --yes is deliberately never passed here.
  "$installer" onboard --source-dir="$source_directory" \
    --archive="$working_directory/$archive" \
    --attestations="$working_directory/build-attestation.jsonl" --version="$version"
}
if [[ "$native_exists" == true ]]; then
  onboard
elif [[ "$installation_exists" == true ]]; then
  "$installer" install --source-dir="$source_directory" --yes --format=text
else
  preflight_exit=0
  "$installer" preflight --format=text || preflight_exit=$?
  if [[ "$preflight_exit" == 0 ]]; then
    "$installer" install --source-dir="$source_directory" --yes --format=text
  elif [[ "$preflight_exit" == 2 ]]; then
    # Read data only: never source an OS-release file as shell code. The native
    # handler repeats full eligibility checks, including why preflight failed.
    os_id="$(awk -F= '$1 == "ID" { gsub(/"/, "", $2); print $2 }' /etc/os-release)"
    os_version="$(awk -F= '$1 == "VERSION_ID" { gsub(/"/, "", $2); print $2 }' /etc/os-release)"
    if [[ "$os_id" == 'debian' && "$os_version" == '13' ]]; then
      onboard
    else
      fail 'preflight blocked installation; automatic native preparation is limited to eligible fresh Debian 13 hosts'
    fi
  else
    fail 'preflight inspection failed; no installation or native preparation was started'
  fi
fi
