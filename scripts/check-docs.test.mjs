// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { anchors, checkDocuments, links } from './check-docs.mjs';

function currentCandidateVersionsAgree(bootstrap, documents) {
  const defaults = [...bootstrap.matchAll(/^readonly default_version='(0\.1\.0-beta\.[1-9][0-9]*)'$/gm)];
  assert.equal(defaults.length, 1, 'one canonical experimental bootstrap default');
  const version = defaults[0][1];
  for (const [file, source] of documents) {
    // A named, explicitly unpublished security-policy section is separate from
    // the public bootstrap selection. Never advance that selection just to build.
    const sections = file === 'SECURITY.md' ? [...source.matchAll(/^## Unpublished candidate\n([\s\S]*?)(?=^## |$(?![\s\S]))/gm)] : [];
    assert.ok(sections.length <= 1, 'unambiguous unpublished candidate section');
    if (sections.length) {
      const section = sections[0][0];
      const versions = [...new Set([...section.matchAll(/0\.1\.0-beta\.[0-9]+/g)].map((match) => match[0]))];
      assert.equal(versions.length, 1, 'one named unpublished candidate');
      assert.match(versions[0], /^0\.1\.0-beta\.[1-9][0-9]*$/);
      assert.notEqual(versions[0], version, 'published default is not unpublished');
      assert.ok(section.includes('**not published or approved for use**'), 'explicit candidate-only disclosure');
    }
    let text = sections.length ? source.replace(sections[0][0], '') : source;
    // Security policy must retain a published predecessor's actual terms rather
    // than erasing it when the bootstrap moves. Only a named historical section
    // is exempt, and it must name exactly one older canonical published version.
    const earlier = file === 'SECURITY.md' ? [...text.matchAll(/^## Earlier published release\n([\s\S]*?)(?=^## |$(?![\s\S]))/gm)] : [];
    assert.ok(earlier.length <= 1, 'unambiguous earlier published release section');
    if (earlier.length) {
      const historical = earlier[0][0];
      const versions = [...new Set([...historical.matchAll(/0\.1\.0-beta\.[0-9]+/g)].map(match => match[0]))];
      assert.equal(versions.length, 1, 'one named earlier published version');
      assert.match(versions[0], /^0\.1\.0-beta\.[1-9][0-9]*$/);
      assert.ok(Number(versions[0].split('.').at(-1)) < Number(version.split('.').at(-1)), 'historical beta precedes default');
      assert.ok(historical.includes('was published on '), 'explicit historical publication statement');
      text = text.replace(historical, '');
    }
    const references = [...text.matchAll(/0\.1\.0-beta\.[0-9]+/g)].map((match) => match[0]);
    assert.ok(references.length > 0, `${file}: current candidate must be named`);
    assert.ok(references.every((value) => value === version), `${file}: candidate references differ from ${version}`);
  }
}

test('current support policy and quick-start versions match the bootstrap', () => {
  const read = (file) => readFileSync(new URL(`../${file}`, import.meta.url), 'utf8').replaceAll('\r', '');
  // Only current support/quick-start documents, not historical evidence or
  // upgrade examples, are required to name one candidate throughout.
  const files = ['README.md', 'SECURITY.md', 'cmd/stackfort-installer/README.md', 'docs/installer-installation.md'];
  currentCandidateVersionsAgree(read('packaging/installer/install.sh'), new Map(files.map((file) => [file, read(file)])));
});

test('candidate documentation guard rejects stale, mixed, missing and ambiguous versions', () => {
  const bootstrap = "readonly default_version='0.1.0-beta.8'\n";
  currentCandidateVersionsAgree(bootstrap, new Map([['synthetic.md', '0.1.0-beta.8']]));
  for (const text of ['0.1.0-beta.6', '0.1.0-beta.8 and 0.1.0-beta.7', 'no version', '0.1.0-beta.08']) {
    assert.throws(() => currentCandidateVersionsAgree(bootstrap, new Map([['synthetic.md', text]])));
  }
  assert.throws(() => currentCandidateVersionsAgree(bootstrap + bootstrap, new Map()));
  assert.throws(() => currentCandidateVersionsAgree("readonly default_version='latest'\n", new Map()));
  const policy = '0.1.0-beta.8\n\n## Unpublished candidate\n0.1.0-beta.9 is **not published or approved for use**.\n\n## Other\n';
  currentCandidateVersionsAgree(bootstrap, new Map([['SECURITY.md', policy]]));
  for (const invalid of [policy.replace('not published or approved for use', 'ready'), policy.replace('beta.9', 'beta.8'), policy.replace('beta.9', 'beta.09'), policy + '0.1.0-beta.9']) {
    assert.throws(() => currentCandidateVersionsAgree(bootstrap, new Map([['SECURITY.md', invalid]])));
  }
  const historical = '0.1.0-beta.8\n\n## Earlier published release\n0.1.0-beta.7 was published on 2026-09-24.\n\n## Other\n';
  currentCandidateVersionsAgree(bootstrap, new Map([['SECURITY.md', historical]]));
  for (const invalid of [historical.replace('was published on ', 'unpublished'), historical.replace('beta.7', 'beta.8'), historical.replace('beta.7', 'beta.9'), historical.replace('beta.7', 'beta.07'), historical + '0.1.0-beta.7', historical + historical]) {
    assert.throws(() => currentCandidateVersionsAgree(bootstrap, new Map([['SECURITY.md', invalid]])));
  }
});

test('extracts inline, image, reference and HTML links with source lines', () => {
  assert.deepEqual(links('[Guide](docs/guide.md)\n![Image](a.png)\n[ref]: <docs/a b.md> "Title"\n<a href="docs/x.md">x</a>'), [
    { target: 'docs/guide.md', line: 1 }, { target: 'a.png', line: 2 },
    { target: 'docs/a b.md', line: 3 }, { target: 'docs/x.md', line: 4 },
  ]);
});

test('ignores comments and inline/fenced examples, including a shorter fence inside', () => {
  const source = '<!--\n[ignored](bad.md)\n-->\n`[ignored](bad.md)`\n````md\n```\n[ignored](bad.md)\n````\n~~~sh\n[ignored](bad.md)\n~~~\n[real](ok.md)';
  assert.deepEqual(links(source), [{ target: 'ok.md', line: 12 }]);
});

test('supports formatted Unicode ATX headings, duplicate slugs and explicit anchors', () => {
  assert.deepEqual([...anchors('# Hello **world**\n## `Hello world`\n## Hello world-1\n## Über uns! ###\n<a id="custom"></a>\n```\n# ignored\n```')], [
    'hello-world', 'hello-world-1', 'hello-world-1-1', 'über-uns', 'custom',
  ]);
});

test('resolves relative paths, encoded spaces, directories, references and anchors', () => {
  const docs = new Map([
    ['README.md', '[guide](docs/guide.md#setup) [dir](docs/) [space](docs/a%20b.md) [web](https://example.test/not-local)'],
    ['docs/guide.md', '# Setup\n[self](#setup) [root](../README.md) [code](../cmd/main.go#L1)'],
    ['docs/a b.md', '# Other'],
  ]);
  assert.deepEqual(checkDocuments(docs, [...docs.keys(), 'cmd/main.go']), { errors: [], checked: 6 });
});

test('rejects missing targets, wrong case, escapes, malformed URLs and stale anchors', () => {
  const docs = new Map([
    ['README.md', '[missing](gone.md) [case](docs/Guide.md) [escape](../out.md) [url](%zz) [anchor](docs/guide.md#gone) [absolute](/docs/guide.md)'],
    ['docs/guide.md', '# Setup'],
  ]);
  const result = checkDocuments(docs, [...docs.keys()]);
  assert.equal(result.errors.length, 6);
  assert.equal(result.checked, 6);
  assert.match(result.errors[0], /README.md:1: missing target/);
  assert.match(result.errors[4], /missing Markdown heading/);
});
