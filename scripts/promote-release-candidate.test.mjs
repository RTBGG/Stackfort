// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict';
import test from 'node:test';
import { cpSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs';
import { execFileSync, spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { validatePromotion, validateRetainedBuild, main } from './promote-release-candidate.mjs';

function fixture() {
  const candidate = { version: '1.2.3-beta.1', commit: 'a'.repeat(40), archive: 'stackfort-1.2.3-beta.1-linux-amd64.tar.gz', archiveSHA256: 'b'.repeat(64), build: { runId: 100, attempt: 1, artifactId: 200, artifactSHA256: 'c'.repeat(64) } };
  return {
    promotion: { schemaVersion: 1, kind: 'candidate-promotion', candidate },
    version: candidate.version, commit: candidate.commit,
    run: { id: 100, run_attempt: 1, head_sha: candidate.commit, path: '.github/workflows/release.yml', event: 'workflow_dispatch', status: 'completed', conclusion: 'success', repository: { full_name: 'RTBGG/Stackfort' }, head_repository: { full_name: 'RTBGG/Stackfort' } },
    artifacts: { total_count: 1, artifacts: [{ id: 200, name: 'stackfort-1.2.3-beta.1', size_in_bytes: 1024, expired: false, digest: 'sha256:' + 'c'.repeat(64), workflow_run: { id: 100, head_sha: candidate.commit } }] },
  };
}

const verify = (data) => validateRetainedBuild(data.promotion, data.version, data.commit, data.run, data.artifacts);

test('mechanical promotion validates retained exact build without authorizing publication', () => {
  const data = fixture();
  assert.deepEqual(validatePromotion(data.promotion, data.version, data.commit), data.promotion.candidate);
  const result = verify(data);
  assert.equal(result.kind, 'verified-candidate-promotion');
  assert.equal(result.publicationAuthorized, false);
});

const failures = {
  'approval field in mechanical record': (data) => { data.promotion.approved = true; },
  'rehearsal record': (data) => { data.promotion.kind = 'rehearsal'; },
  'wrong version': (data) => { data.version = '1.2.3'; },
  'wrong source': (data) => { data.commit = 'd'.repeat(40); },
  'unsafe run identity': (data) => { data.promotion.candidate.build.runId = '../run'; },
  'wrong run': (data) => { data.run.id = 101; },
  'superseded attempt': (data) => { data.run.run_attempt = 2; },
  'failed build': (data) => { data.run.conclusion = 'failure'; },
  'running build': (data) => { data.run.status = 'in_progress'; },
  'tag instead of dispatched build': (data) => { data.run.event = 'push'; },
  'wrong workflow': (data) => { data.run.path = '.github/workflows/ci.yml'; },
  'different run source': (data) => { data.run.head_sha = 'd'.repeat(40); },
  'foreign repository': (data) => { data.run.repository.full_name = 'other/Stackfort'; },
  'fork source': (data) => { data.run.head_repository.full_name = 'other/Stackfort'; },
  'missing artifact': (data) => { data.artifacts.artifacts = []; data.artifacts.total_count = 0; },
  'truncated inventory': (data) => { data.artifacts.total_count = 2; },
  'duplicate named artifact': (data) => { data.artifacts.artifacts.push({ ...data.artifacts.artifacts[0], id: 201 }); data.artifacts.total_count++; },
  'expired artifact': (data) => { data.artifacts.artifacts[0].expired = true; },
  'wrong artifact ID': (data) => { data.artifacts.artifacts[0].id = 201; },
  'wrong artifact digest': (data) => { data.artifacts.artifacts[0].digest = 'sha256:' + 'd'.repeat(64); },
  'missing artifact digest': (data) => { delete data.artifacts.artifacts[0].digest; },
  'empty artifact': (data) => { data.artifacts.artifacts[0].size_in_bytes = 0; },
  'oversized artifact': (data) => { data.artifacts.artifacts[0].size_in_bytes = 2 ** 31 + 1; },
  'artifact from different run': (data) => { data.artifacts.artifacts[0].workflow_run.id = 101; },
  'artifact from different source': (data) => { data.artifacts.artifacts[0].workflow_run.head_sha = 'd'.repeat(40); },
};
for (const [name, mutate] of Object.entries(failures)) test(`rejects ${name}`, () => {
  const data = fixture();
  mutate(data);
  assert.throws(() => verify(data));
});

test('rejects absent/unknown CLI modes instead of promoting or publishing', () => {
  for (const args of [[], ['--force', 'yes'], ['--mode', 'inventory'], ['--mode', 'verify', '--mode', 'inventory']]) assert.throws(() => main(args));
});

test('tag workflow retains genuine tag proof before gates and never rebuilds promoted payload', () => {
  const workflow = readFileSync(new URL('../.github/workflows/release.yml', import.meta.url), 'utf8');
  const promote = workflow.slice(workflow.indexOf('\n  promote:'));
  assert.match(promote, /if: github\.event_name == 'push'/);
  assert.doesNotMatch(promote, /build-release\.sh|build-native-package\.sh/);
  assert.ok(promote.indexOf('id: qualification_attestation') < promote.indexOf('run: bash scripts/verify-release-readiness.sh'));
  assert.ok(promote.indexOf('stackfort-tag-candidate-') < promote.indexOf('run: bash scripts/verify-release-readiness.sh'));
  assert.ok(promote.indexOf('run: bash scripts/verify-release-readiness.sh') < promote.indexOf('Publish immutable GitHub release assets'));
  assert.ok(promote.indexOf('run: bash scripts/verify-upgrade-matrix.sh') < promote.indexOf('Publish immutable GitHub release assets'));
  assert.match(promote, /build-attestation\.jsonl/);
  assert.match(promote, /No independent|independentReviewDisclosure/);
});

test('promotion shell uses exact pinned API bytes and never rebuilds or publishes on failure', { skip: process.platform === 'win32' }, () => {
  const directory = mkdtempSync(path.join(tmpdir(), 'stackfort-promotion-test-'));
  try {
    const root = fileURLToPath(new URL('..', import.meta.url));
    for (const name of ['scripts', 'test-bin']) mkdirSync(path.join(directory, name));
    for (const name of ['promote-release-candidate.sh', 'promote-release-candidate.mjs', 'verify-release-readiness.mjs', 'extract-release-candidate.py', 'extract-release-candidate.test.py']) cpSync(path.join(root, 'scripts', name), path.join(directory, 'scripts', name));
    const gitOptions = { cwd: directory, env: { ...process.env, GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null' } };
    execFileSync('git', ['init', '-q'], gitOptions);
    execFileSync('git', ['add', '.'], gitOptions);
    execFileSync('git', ['-c', 'user.name=Promotion Test', '-c', 'user.email=promotion@example.invalid', '-c', 'commit.gpgsign=false', 'commit', '-qm', 'Synthetic retained candidate fixture'], gitOptions);
    const commit = execFileSync('git', ['rev-parse', 'HEAD'], { ...gitOptions, encoding: 'utf8' }).trim();
    const generated = JSON.parse(execFileSync('python3', ['-B', '-c', `import base64, io, json, runpy, sys, zipfile
fixtures = runpy.run_path('scripts/extract-release-candidate.test.py')
record, files = fixtures['fixture'](sys.argv[1])
buffer = io.BytesIO()
with zipfile.ZipFile(buffer, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
    for name, value in files.items():
        archive.writestr(name, value)
record['candidate']['build']['artifactSHA256'] = fixtures['digest'](buffer.getvalue())
print(json.dumps({'candidate': record['candidate'], 'zip': base64.b64encode(buffer.getvalue()).decode(), 'archive': base64.b64encode(files[record['candidate']['archive']]).decode()}))
`, commit], { cwd: directory, encoding: 'utf8' }));
    const data = fixture();
    const candidate = generated.candidate;
    candidate.build.runId = data.run.id;
    candidate.build.artifactId = data.artifacts.artifacts[0].id;
    data.promotion.candidate = candidate;
    data.run.head_sha = commit;
    Object.assign(data.artifacts.artifacts[0], { size_in_bytes: Buffer.from(generated.zip, 'base64').length, digest: 'sha256:' + candidate.build.artifactSHA256, workflow_run: { id: candidate.build.runId, head_sha: commit } });
    const evidenceCommit = 'd'.repeat(40);
    const prefix = 'repos/RTBGG/Stackfort/';
    const selectionEndpoint = prefix + `contents/packaging/releases/promotion/${candidate.version}.json?ref=${evidenceCommit}`;
    const runEndpoint = prefix + `actions/runs/${candidate.build.runId}`;
    const artifactEndpoint = runEndpoint + '/artifacts?per_page=100';
    const zipEndpoint = prefix + `actions/artifacts/${candidate.build.artifactId}/zip`;
    const comparisonEndpoint = prefix + `compare/${commit}...${evidenceCommit}`;
    const replies = {
      [prefix + 'git/ref/heads/main']: evidenceCommit,
      [comparisonEndpoint]: 'ahead',
      [selectionEndpoint]: JSON.stringify(data.promotion, null, 2) + '\n',
      [runEndpoint]: JSON.stringify(data.run),
      [artifactEndpoint]: JSON.stringify([data.artifacts]),
      [zipEndpoint]: { base64: generated.zip },
    };
    const responseFile = path.join(directory, 'test-replies.json');
    writeFileSync(responseFile, JSON.stringify(replies));
    writeFileSync(path.join(directory, 'test-bin/gh'), `#!/usr/bin/env node\nconst fs = require('node:fs');\nconst endpoint = process.argv.find((arg) => arg.startsWith('repos/'));\nconst replies = JSON.parse(fs.readFileSync(process.env.STACKFORT_PROMOTION_TEST_DATA, 'utf8'));\nif (!(endpoint in replies)) process.exit(1);\nconst value = replies[endpoint];\nprocess.stdout.write(typeof value === 'string' ? value : Buffer.from(value.base64, 'base64'));\n`, { mode: 0o755 });
    const env = { ...process.env, PATH: path.join(directory, 'test-bin') + path.delimiter + process.env.PATH, STACKFORT_PROMOTION_TEST_DATA: responseFile, GITHUB_EVENT_NAME: 'push', GITHUB_REPOSITORY: 'RTBGG/Stackfort', GITHUB_SHA: commit, GITHUB_REF: `refs/tags/v${candidate.version}`, VERSION: candidate.version };
    const run = (overrides = {}) => spawnSync('bash', ['scripts/promote-release-candidate.sh'], { cwd: directory, env: { ...env, ...overrides }, encoding: 'utf8' });
    const dist = path.join(directory, 'dist');
    const success = run();
    assert.equal(success.status, 0, success.stdout + success.stderr);
    assert.deepEqual(readFileSync(path.join(dist, candidate.archive)), Buffer.from(generated.archive, 'base64'));
    const receipt = JSON.parse(readFileSync(path.join(dist, 'candidate-promotion.json'), 'utf8'));
    assert.deepEqual(receipt.candidate, candidate);
    assert.equal(receipt.publicationAuthorized, false);
    const protectedArchive = readFileSync(path.join(dist, candidate.archive));
    assert.notEqual(run().status, 0, 'existing candidate was replaced');
    assert.deepEqual(readFileSync(path.join(dist, candidate.archive)), protectedArchive);
    rmSync(dist, { recursive: true });
    const failures = {
      'missing selection': (values) => { delete values[selectionEndpoint]; },
      'changed selection source': (values) => { const changed = structuredClone(data.promotion); changed.candidate.commit = 'e'.repeat(40); values[selectionEndpoint] = JSON.stringify(changed, null, 2) + '\n'; },
      'diverged evidence branch': (values) => { values[comparisonEndpoint] = 'diverged'; },
      'missing source run': (values) => { delete values[runEndpoint]; },
      'superseded run attempt': (values) => { values[runEndpoint] = JSON.stringify({ ...data.run, run_attempt: 2 }); },
      'failed source run': (values) => { values[runEndpoint] = JSON.stringify({ ...data.run, conclusion: 'failure' }); },
      'truncated artifact pages': (values) => { values[artifactEndpoint] = JSON.stringify([{ ...data.artifacts, total_count: 2 }]); },
      'missing artifact inventory': (values) => { values[artifactEndpoint] = '[]'; },
      'expired artifact': (values) => { const changed = structuredClone(data.artifacts); changed.artifacts[0].expired = true; values[artifactEndpoint] = JSON.stringify([changed]); },
      'changed artifact digest': (values) => { const changed = structuredClone(data.artifacts); changed.artifacts[0].digest = 'sha256:' + 'f'.repeat(64); values[artifactEndpoint] = JSON.stringify([changed]); },
      'deleted ZIP': (values) => { delete values[zipEndpoint]; },
      'changed ZIP bytes': (values) => { values[zipEndpoint] = { base64: Buffer.from('not the pinned ZIP').toString('base64') }; },
    };
    for (const [name, mutate] of Object.entries(failures)) {
      const changed = structuredClone(replies);
      mutate(changed);
      writeFileSync(responseFile, JSON.stringify(changed));
      const result = run();
      assert.notEqual(result.status, 0, `${name} passed: ${result.stdout}${result.stderr}`);
      assert.equal(existsSync(dist), false, `${name} created publication files`);
    }
    writeFileSync(responseFile, JSON.stringify(replies));
    assert.notEqual(run({ GITHUB_EVENT_NAME: 'workflow_dispatch' }).status, 0, 'manual build invoked tag promotion');
    assert.notEqual(run({ GITHUB_REF: 'refs/tags/v9.9.9' }).status, 0, 'wrong tag promoted');
    assert.equal(existsSync(dist), false);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
