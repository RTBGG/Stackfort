#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Extract only the fixed, digest-pinned candidate ZIP into a new directory.

Artifact processing, not source editing. Standard-library ZIP/TAR readers reject
corrupt members; no archive-supplied path is passed to extract()/extractall().
"""

import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import tarfile
import zipfile

MAX_FILE = 512 << 20
MAX_TOTAL = 2 << 30


def digest_file(filename):
    with open(filename, "rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def extract_candidate(promotion, archive, destination):
    require(promotion.get("kind") == "verified-candidate-promotion" and
            promotion.get("publicationAuthorized") is False, "verified promotion required")
    candidate = promotion["candidate"]
    version = candidate["version"]
    require(re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?", version), "invalid version")
    archive_stat = archive.lstat()
    require(stat.S_ISREG(archive_stat.st_mode) and 0 < archive_stat.st_size <= MAX_TOTAL, "invalid candidate ZIP")
    require(digest_file(archive) == candidate["build"]["artifactSHA256"], "candidate ZIP digest mismatch")
    package_version = version.replace("-", "~", 1)
    deb = f"stackfort-release_{package_version}-1_amd64.deb"
    rpm = f"stackfort-release-{package_version}-1.sf1.x86_64.rpm"
    installer = f"stackfort-installer-{version}-linux-amd64"
    sbom = f"stackfort-{version}.spdx.json"
    checksummed = {candidate["archive"], installer, deb, rpm, deb + ".release.json", rpm + ".release.json"}
    expected = checksummed | {"SHA256SUMS", sbom}
    require(candidate["archive"] == f"stackfort-{version}-linux-amd64.tar.gz", "invalid archive name")
    with zipfile.ZipFile(archive) as package:
        members = package.infolist()
        require(len(members) == len(expected) and {entry.filename for entry in members} == expected, "incomplete, duplicate or unexpected candidate ZIP members")
        require(sum(entry.file_size for entry in members) <= MAX_TOTAL, "candidate exceeds size limit")
        for entry in members:
            kind = stat.S_IFMT(entry.external_attr >> 16)
            require(not entry.is_dir() and kind in (0, stat.S_IFREG) and
                    not entry.flag_bits & 1 and 0 < entry.file_size <= MAX_FILE,
                    "unsafe candidate ZIP member")
        destination.mkdir(mode=0o700, parents=False, exist_ok=False)
        for entry in members:
            with package.open(entry) as source, (destination / entry.filename).open("xb") as target:
                os.chmod(target.name, 0o600)
                copied = 0
                while chunk := source.read(1 << 20):
                    copied += len(chunk)
                    require(copied <= entry.file_size, "candidate member exceeds declared size")
                    target.write(chunk)
                require(copied == entry.file_size, "candidate member truncated")
    payload = destination / candidate["archive"]
    require(digest_file(payload) == candidate["archiveSHA256"], "candidate archive SHA-256 mismatch")
    checksum_file = destination / "SHA256SUMS"
    require(checksum_file.stat().st_size <= 1 << 20, "checksum file exceeds limit")
    seen = set()
    for line in checksum_file.read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  (?:\./)?([A-Za-z0-9._~+-]+)", line)
        require(match is not None, "malformed candidate checksum")
        name = match[2]
        require(name in checksummed and name not in seen, "unexpected or duplicate candidate checksum")
        require(digest_file(destination / name) == match[1], "candidate payload checksum mismatch")
        seen.add(name)
    require(seen == checksummed, "incomplete candidate checksums")
    require((destination / sbom).stat().st_size <= 16 << 20, "SBOM exceeds size limit")
    require(json.loads((destination / sbom).read_text(encoding="utf-8")).get("spdxVersion", "").startswith("SPDX-"), "missing SPDX SBOM")
    top = f"stackfort-{version}-linux-amd64"
    with tarfile.open(payload, "r:gz") as source:
        members = []
        total = 0
        for entry in source:
            members.append(entry)
            total += entry.size
            require(len(members) <= 100_000 and 0 <= entry.size <= MAX_TOTAL and total <= MAX_TOTAL,
                    "candidate TAR member/expanded-size limit")
        for name, expected_value in (("VERSION", version), ("COMMIT", candidate["commit"])):
            matches = [entry for entry in members if entry.name == f"{top}/{name}"]
            require(len(matches) == 1 and matches[0].isfile() and 0 < matches[0].size <= 128, "candidate identity member missing or unsafe")
            require(source.extractfile(matches[0]).read().decode("ascii").strip() == expected_value, "archive source identity mismatch")
        matches = [entry for entry in members if entry.name == f"{top}/bin/stackfort-installer"]
        require(len(matches) == 1 and matches[0].isfile() and 0 < matches[0].size <= MAX_FILE, "archived installer missing or unsafe")
        require(hashlib.file_digest(source.extractfile(matches[0]), "sha256").hexdigest() == digest_file(destination / installer), "standalone and archived installer differ")


if __name__ == "__main__":
    try:
        require(len(sys.argv) == 4, "usage: extract-release-candidate.py PROMOTION.json CANDIDATE.zip NEW_DIRECTORY")
        record = Path(sys.argv[1])
        require(record.is_file() and not record.is_symlink() and record.stat().st_size <= 1 << 20, "invalid promotion record")
        extract_candidate(json.loads(record.read_text(encoding="utf-8")), Path(sys.argv[2]), Path(sys.argv[3]))
    except (OSError, ValueError, KeyError, TypeError, AttributeError, zipfile.BadZipFile, tarfile.TarError) as error:
        print(f"Candidate extraction blocked: {error}", file=sys.stderr)
        sys.exit(1)
