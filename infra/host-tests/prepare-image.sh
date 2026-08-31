#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
image_id="${1:-}"
cache_root="${STACKFORT_IMAGE_CACHE:-$repository_root/infra/host-tests/cache}"

if [[ -z "$image_id" ]]; then
  printf 'Usage: %s <debian-13|ubuntu-26.04|rocky-10>\n' "$0" >&2
  exit 2
fi

case "$image_id" in
  debian-13)
    image_url='https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2'
    checksum_url='https://cloud.debian.org/images/cloud/trixie/latest/SHA512SUMS'
    algorithm='sha512'
    ;;
  ubuntu-26.04)
    image_url='https://cloud-images.ubuntu.com/releases/26.04/release/ubuntu-26.04-server-cloudimg-amd64.img'
    checksum_url='https://cloud-images.ubuntu.com/releases/26.04/release/SHA256SUMS'
    algorithm='sha256'
    ;;
  rocky-10)
    image_url='https://dl.rockylinux.org/pub/rocky/10/images/x86_64/Rocky-10-GenericCloud-Base.latest.x86_64.qcow2'
    checksum_url="$image_url.CHECKSUM"
    algorithm='sha256-rocky'
    ;;
  *)
    printf 'Unsupported image ID: %s\n' "$image_id" >&2
    exit 2
    ;;
esac

mkdir -p "$cache_root"
image_name="${image_url##*/}"
image_path="$cache_root/$image_name"
checksum_path="$cache_root/$image_name.checksums"
temporary_image="$image_path.partial"

curl --fail --location --proto '=https' --tlsv1.2 --retry 3 \
  --continue-at - --output "$temporary_image" "$image_url"
curl --fail --location --proto '=https' --tlsv1.2 --retry 3 \
  --output "$checksum_path" "$checksum_url"

case "$algorithm" in
  sha256|sha512)
    checksum_line="$(grep -E "[[:space:]*]$image_name$" "$checksum_path" | head -n 1)"
    if [[ -z "$checksum_line" ]]; then
      printf 'No checksum found for %s.\n' "$image_name" >&2
      exit 1
    fi
    expected="${checksum_line%%[[:space:]]*}"
    actual="$(${algorithm}sum "$temporary_image" | cut -d ' ' -f 1)"
    ;;
  sha256-rocky)
    expected="$(grep -E "^SHA256 \($image_name\) = [0-9a-fA-F]{64}$" "$checksum_path" | head -n 1 | awk '{print $4}')"
    if [[ -z "$expected" ]]; then
      printf 'No Rocky checksum found for %s.\n' "$image_name" >&2
      exit 1
    fi
    actual="$(sha256sum "$temporary_image" | cut -d ' ' -f 1)"
    ;;
esac

if [[ "${actual,,}" != "${expected,,}" ]]; then
  printf 'Checksum mismatch for %s.\n' "$image_name" >&2
  exit 1
fi

mv -f -- "$temporary_image" "$image_path"
printf '%s  %s\n' "${actual,,}" "$image_name" >"$image_path.verified"
printf '%s\n' "$image_path"
