// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict';
import test from 'node:test';
import { anchors, checkDocuments, links } from './check-docs.mjs';

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
