#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Synthetic artifact tests; these never create release evidence or approvals."""

import hashlib
import importlib.util
import io
from pathlib import Path
import stat
import tarfile
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location("candidate", Path(__file__).with_name("extract-release-candidate.py"))
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


def digest(value):
    return hashlib.sha256(value).hexdigest()


def fixture(commit="a" * 40):
    version = "1.2.3-beta.1"
    top = f"stackfort-{version}-linux-amd64"
    installer = b"synthetic test installer, never executable"
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w:gz") as archive:
        for name, value in (("VERSION", (version + "\n").encode()), ("COMMIT", (commit + "\n").encode()), ("bin/stackfort-installer", installer)):
            entry = tarfile.TarInfo(f"{top}/{name}")
            entry.size = len(value)
            archive.addfile(entry, io.BytesIO(value))
    archive_bytes = buffer.getvalue()
    deb = "stackfort-release_1.2.3~beta.1-1_amd64.deb"
    rpm = "stackfort-release-1.2.3~beta.1-1.sf1.x86_64.rpm"
    files = {f"{top}.tar.gz": archive_bytes, f"stackfort-installer-{version}-linux-amd64": installer, deb: b"synthetic DEB", rpm: b"synthetic RPM", deb + ".release.json": b"{}", rpm + ".release.json": b"{}"}
    files["SHA256SUMS"] = "".join(f"{digest(value)}  ./{name}\n" for name, value in sorted(files.items())).encode()
    files[f"stackfort-{version}.spdx.json"] = b'{"spdxVersion":"SPDX-2.3"}'
    record = {"schemaVersion": 1, "kind": "verified-candidate-promotion", "publicationAuthorized": False, "candidate": {"version": version, "commit": commit, "archive": f"{top}.tar.gz", "archiveSHA256": digest(archive_bytes), "build": {"runId": 1, "attempt": 1, "artifactId": 2, "artifactSHA256": ""}}}
    return record, files


class CandidateExtractionTests(unittest.TestCase):
    def run_fixture(self, mutate=None, unsafe_entry=None, existing_destination=False):
        record, files = fixture()
        if mutate:
            mutate(record, files)
        with tempfile.TemporaryDirectory(prefix="stackfort-candidate-test-") as root:
            root = Path(root)
            archive = root / "candidate.zip"
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as package:
                for name, value in files.items():
                    package.writestr(name, value)
                if unsafe_entry:
                    package.writestr(unsafe_entry, b"unsafe")
            record["candidate"]["build"]["artifactSHA256"] = digest(archive.read_bytes())
            if existing_destination:
                (root / "payload").mkdir()
                (root / "payload" / "preserve").write_bytes(b"must not replace")
                with self.assertRaises(FileExistsError):
                    candidate.extract_candidate(record, archive, root / "payload")
                self.assertEqual(list((root / "payload").iterdir()), [root / "payload" / "preserve"])
                self.assertEqual((root / "payload" / "preserve").read_bytes(), b"must not replace")
                return
            candidate.extract_candidate(record, archive, root / "payload")
            self.assertEqual({item.name for item in (root / "payload").iterdir()}, set(files))
            for name, value in files.items():
                self.assertEqual((root / "payload" / name).read_bytes(), value)

    def test_exact_regular_inventory(self):
        self.run_fixture()

    def test_rejected_changed_or_incomplete_payloads(self):
        cases = {
            "missing archive": lambda record, files: files.pop(record["candidate"]["archive"]),
            "extra file": lambda record, files: files.update({"other": b"other"}),
            "changed archive": lambda record, files: record["candidate"].update(archiveSHA256="0" * 64),
            "changed component": lambda record, files: files.update({"stackfort-release_1.2.3~beta.1-1_amd64.deb": b"changed"}),
            "missing checksum": lambda record, files: files.update(SHA256SUMS=files["SHA256SUMS"].split(b"\n", 1)[1]),
            "duplicate checksum": lambda record, files: files.update(SHA256SUMS=files["SHA256SUMS"] * 2),
            "escaping checksum": lambda record, files: files.update(SHA256SUMS=b"0" * 64 + b"  ../outside\n"),
            "invalid SBOM": lambda record, files: files.update({"stackfort-1.2.3-beta.1.spdx.json": b"{}"}),
            "changed commit": lambda record, files: record["candidate"].update(commit="b" * 40),
            "publication claim": lambda record, files: record.update(publicationAuthorized=True),
        }
        for name, mutate in cases.items():
            with self.subTest(name=name), self.assertRaises((ValueError, KeyError)):
                self.run_fixture(mutate)

    def test_rejects_links_traversal_and_duplicate_zip_members(self):
        for name in ("../outside", "/absolute", "SHA256SUMS"):
            entry = zipfile.ZipInfo(name)
            entry.create_system = 3
            entry.external_attr = (stat.S_IFLNK | 0o777) << 16
            with self.subTest(name=name), self.assertRaises(ValueError):
                self.run_fixture(unsafe_entry=entry)

    def test_never_reuses_destination_or_accepts_wrong_zip_digest(self):
        self.run_fixture(existing_destination=True)
        record, _ = fixture()
        with tempfile.TemporaryDirectory(prefix="stackfort-candidate-test-") as root:
            root = Path(root)
            archive = root / "candidate.zip"
            archive.write_bytes(b"not the pinned ZIP")
            with self.assertRaises(ValueError):
                candidate.extract_candidate(record, archive, root / "payload")
            self.assertFalse((root / "payload").exists())


if __name__ == "__main__":
    unittest.main()
