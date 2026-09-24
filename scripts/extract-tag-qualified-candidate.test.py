# SPDX-License-Identifier: AGPL-3.0-or-later
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location('tag', Path(__file__).with_name('extract-tag-qualified-candidate.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class TagContracts(unittest.TestCase):
    def attempt(self, mutation=None, wrong_hash=False):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            originals = root / 'originals'
            originals.mkdir()
            files = {f'payload-{i}': str(i).encode() for i in range(10)}
            for key, value in files.items():
                (originals / key).write_bytes(value)
            record = {'kind': 'verified-candidate-promotion', 'publicationAuthorized': False, 'candidate': {}}
            verified = root / 'verified.json'
            verified.write_text(json.dumps(record))
            files.update({'candidate-promotion.json': json.dumps(record).encode(), 'build-attestation.jsonl': b'{"testOnly":true}\n'})
            if mutation:
                mutation(files)
            archive = root / 'candidate.zip'
            with zipfile.ZipFile(archive, 'w') as bundle:
                for key, value in files.items():
                    bundle.writestr(key, value)
            digest = '0' * 64 if wrong_hash else hashlib.sha256(archive.read_bytes()).hexdigest()
            return module.extract_tag(archive, digest, originals, verified, root / 'out')

    def test_original_bytes_preserved_without_claiming_signature_verification(self):
        result = self.attempt()
        self.assertEqual(result['originalFilesIdentical'], 10)
        self.assertFalse(result['cryptographicOriginVerifiedByExtractor'])

    def test_changed_missing_traversal_or_wrong_promotion_rejected(self):
        changes = [lambda f: f.pop('payload-0'), lambda f: f.update({'../escape': b'x'}),
                   lambda f: f.update({'payload-0': b'changed'}),
                   lambda f: f.update({'candidate-promotion.json': b'{}'}),
                   lambda f: f.update({'build-attestation.jsonl': b'invalid'}),
                   lambda f: f.update({'payload-0': b''})]
        for change in changes:
            with self.assertRaises((ValueError, KeyError)):
                self.attempt(change)
        with self.assertRaises(ValueError):
            self.attempt(wrong_hash=True)


if __name__ == '__main__':
    unittest.main()
