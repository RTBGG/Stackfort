#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
"""Retain original package bytes and genuine tag attestation; no rebuild."""
import hashlib
import json
from pathlib import Path
import re
import stat
import sys
import zipfile


def require(value, message):
    if not value:
        raise ValueError(message)


def sha(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def extract_tag(archive, expected_zip, original, verified, destination):
    require(re.fullmatch(r'[0-9a-f]{64}', expected_zip), 'invalid ZIP digest')
    require(archive.is_file() and not archive.is_symlink() and 0 < archive.stat().st_size <= 2 << 30, 'unsafe ZIP')
    require(sha(archive) == expected_zip, 'tag ZIP digest differs')
    originals = {p.name: p for p in original.iterdir()}
    require(len(originals) == 10 and all(p.is_file() and not p.is_symlink() for p in originals.values()), 'original inventory differs')
    expected = set(originals) | {'candidate-promotion.json', 'build-attestation.jsonl'}
    require(len(expected) == 12, 'reserved original filename')
    require(verified.is_file() and not verified.is_symlink() and verified.stat().st_size <= 1 << 20, 'unsafe promotion record')
    record = json.loads(verified.read_bytes())
    require(record['kind'] == 'verified-candidate-promotion' and record['publicationAuthorized'] is False, 'mechanical promotion required')
    with zipfile.ZipFile(archive) as package:
        members = package.infolist()
        require(len(members) == 12 and {m.filename for m in members} == expected, 'unexpected tag inventory')
        require(sum(m.file_size for m in members) <= 2 << 30, 'tag ZIP exceeds bound')
        for member in members:
            require(not member.is_dir() and not member.flag_bits & 1 and
                    stat.S_IFMT(member.external_attr >> 16) in (0, stat.S_IFREG) and
                    0 < member.file_size <= 512 << 20, 'unsafe tag ZIP member')
        destination.mkdir(mode=0o700, parents=False, exist_ok=False)
        for member in members:
            with package.open(member) as source, (destination / member.filename).open('xb') as target:
                count = 0
                while chunk := source.read(1 << 20):
                    count += len(chunk)
                    require(count <= member.file_size, 'tag member exceeds declared bound')
                    target.write(chunk)
                require(count == member.file_size, 'truncated tag member')
    for name, source in originals.items():
        require(sha(source) == sha(destination / name), 'tag member differs from original: ' + name)
    promoted = destination / 'candidate-promotion.json'
    require(promoted.stat().st_size <= 1 << 20 and json.loads(promoted.read_bytes()) == record, 'tag promotion differs')
    bundle = destination / 'build-attestation.jsonl'
    require(0 < bundle.stat().st_size <= 16 << 20, 'bundle exceeds bound')
    require(all(isinstance(json.loads(line), dict) for line in bundle.read_bytes().splitlines()), 'invalid bundle framing')
    return {'originalFilesIdentical': 10, 'tagArtifactSHA256': expected_zip,
            'attestationSHA256': sha(bundle), 'cryptographicOriginVerifiedByExtractor': False}


if __name__ == '__main__':
    require(len(sys.argv) == 6, 'expected ZIP, digest, originals, verified record, new output')
    print(json.dumps(extract_tag(Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3]), Path(sys.argv[4]), Path(sys.argv[5])), indent=2))
