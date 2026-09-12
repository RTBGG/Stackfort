// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, cpSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync, spawnSync } from 'node:child_process';
import { evidenceInventory, experimentalChecks, experimentalDisclosure, experimentalRemovalDisclosure, hash, main, parseDocument, requiredChecks, requiredWorkflows, reprovisionChecks, reviewScopes, validatePolicy, verifyReadiness } from './verify-release-readiness.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const policy = JSON.parse(readFileSync(path.join(root, 'packaging/releases/readiness-policy.json'), 'utf8'));
const canonical = (value) => JSON.stringify(value, null, 2) + '\n';

// Synthetic in-memory validator fixtures only: no approval, audit or host-test
// evidence from this function is checked into packaging/releases/evidence.
function fixture() {
  const now = Date.parse('2030-01-02T00:00:00Z');
  const archive = Buffer.from('synthetic unit-test archive');
  const expected = { version: '1.2.3-beta.1', commit: 'a'.repeat(40), archiveSHA256: hash(archive) };
  const securityPolicy = Buffer.from('Synthetic test support policy only.');
  const files = new Map();
  const report = (name) => {
    const filename = `docs/release-evidence/${name}.md`;
    const content = Buffer.from(`Synthetic unit-test report: ${name}`);
    files.set(filename, content);
    return { path: filename, sha256: hash(content) };
  };
  const evidence = {
    schemaVersion: 1, kind: 'release-readiness', policy: 'stackfort-release-readiness-v1', releaseClass: 'reviewed-release',
    candidate: { ...expected, archive: `stackfort-${expected.version}-linux-amd64.tar.gz`, build: { runId: 200, attempt: 1, artifactId: 300, artifactSHA256: 'e'.repeat(64) } },
    approvedBy: 'TestMaintainer', approvedAt: '2030-01-01T12:00:00Z',
    installationResults: policy.installationMatrix.map((cell) => ({
      cell: structuredClone(cell), result: 'pass', sourceCommit: expected.commit, archiveSHA256: expected.archiveSHA256,
      checks: [...requiredChecks], completedAt: '2030-01-01T10:00:00Z', report: report(cell.distribution),
    })),
    workflowRuns: requiredWorkflows.map((workflow, index) => ({ path: workflow.path, runId: 100 + index, attempt: 1 })),
    independentReview: { decision: 'approved', reviewer: 'TestIndependentReviewer', independentOfImplementation: true, completedAt: '2030-01-01T11:00:00Z', scopes: [...reviewScopes], report: report('independent-review') },
    supportPolicy: { decision: 'approved', approvedBy: 'RTBGG', versions: [expected.version], terms: structuredClone(policy.support), deploymentLimits: 'Synthetic disposable-host qualification only.', securityPolicySHA256: hash(securityPolicy), report: report('support-decision') },
    publicationDecision: { decision: 'approved', approvedBy: 'RTBGG', releaseClass: 'reviewed-release', freshDisposableOnly: true, productionUseAllowed: false, importantDataAllowed: false, report: report('publication-decision') },
  };
  const workflows = new Map(requiredWorkflows.map((workflow, index) => {
    const runId = 100 + index;
    const names = [...workflow.jobs, ...workflow.optional];
    return [runId, {
      run: { id: runId, run_attempt: 1, head_sha: expected.commit, path: workflow.path, repository: { full_name: 'RTBGG/Stackfort' }, head_repository: { full_name: 'RTBGG/Stackfort' }, event: 'push', status: 'completed', conclusion: 'success' },
      jobs: { total_count: names.length, jobs: names.map((name) => ({ name, run_id: runId, run_attempt: 1, head_sha: expected.commit, status: 'completed', conclusion: workflow.optional.includes(name) ? 'skipped' : 'success' })) },
    }];
  }));
  return { policy: structuredClone(policy), evidence, expected, evidenceCommit: 'b'.repeat(40), files, workflows, securityPolicy, now, archive };
}

function experimentalFixture() {
  const data = fixture();
  data.evidence.releaseClass = 'experimental-beta';
  data.evidence.publicationDecision.releaseClass = 'experimental-beta';
  data.evidence.publicationDecision.removalMethod = 'full-system-reprovision';
  data.evidence.supportPolicy.removalMethod = 'full-system-reprovision';
  data.evidence.independentReview = { decision: 'not-performed', disclosure: experimentalDisclosure };
  for (const result of data.evidence.installationResults) {
    const filename = `docs/release-evidence/synthetic-removal-${result.cell.distribution}.md`;
    const content = Buffer.from('Synthetic unit-test removal report; no system was reinstalled and no removal qualification occurred.');
    data.files.set(filename, content);
    result.checks = [...experimentalChecks];
    result.removal = {
      method: 'full-system-reprovision', result: 'pass', candidate: structuredClone(data.evidence.candidate),
      targetBefore: 'synthetic-disposable-vm-id', targetAfter: 'synthetic-disposable-vm-id',
      installerMediaSHA256: 'd'.repeat(64), checks: [...reprovisionChecks],
      completedAt: '2030-01-01T09:59:00Z', report: { path: filename, sha256: hash(content) },
    };
  }
  return data;
}

test('accepts complete digest-bound evidence and fresh successful exact-commit workflows', () => {
  const data = fixture();
  const result = verifyReadiness(data);
  assert.equal(result.kind, 'verified-release-readiness');
  assert.equal(result.candidate.archiveSHA256, data.expected.archiveSHA256);
  assert.equal(result.evidenceSHA256, hash(Buffer.from(canonical(data.evidence))));
  assert.deepEqual(result.installationCells, policy.installationMatrix);
  assert.equal(result.recordedIndependentReview, true);
  assert.equal(result.removalMethod, 'active-uninstall');
  assert.equal('independentlyAudited' in result, false);
});

test('current policy explicitly narrows fresh one-line support to Debian and grants no candidate approval', () => {
  assert.deepEqual(policy.installationMatrix.map((cell) => `${cell.distribution}/${cell.version}`), ['debian/13']);
  assert.equal(main(['--mode', 'policy', '--policy', path.join(root, 'packaging/releases/readiness-policy.json')]).publicationEnabled, false);
  assert.equal(existsSync(path.join(root, 'packaging/releases/evidence/1.2.3-beta.1.json')), false);
  assert.throws(() => evidenceInventory(policy, undefined, fixture().expected), /expected object/);
});

const evidenceFailures = {
  'unknown evidence field': (data) => { data.evidence.force = true; },
  'missing field': (data) => { delete data.evidence.approvedBy; },
  'rehearsal evidence': (data) => { data.evidence.kind = 'rehearsal'; },
  'unsupported schema': (data) => { data.evidence.schemaVersion = 2; },
  'different policy': (data) => { data.evidence.policy = 'permissive'; },
  'different version': (data) => { data.expected.version = '1.2.3'; },
  'different commit': (data) => { data.expected.commit = 'c'.repeat(40); },
  'different archive': (data) => { data.expected.archiveSHA256 = 'c'.repeat(64); },
  'wrong archive name': (data) => { data.evidence.candidate.archive = 'other.tar.gz'; },
  'malformed SHA': (data) => { data.evidence.candidate.archiveSHA256 = 'A'.repeat(64); },
  'noncanonical version': (data) => { data.evidence.candidate.version = '01.2.3'; },
  'bot approver': (data) => { data.evidence.approvedBy = 'automation[bot]'; },
  'future approval': (data) => { data.evidence.approvedAt = '2030-01-03T12:00:00Z'; },
  'invalid date': (data) => { data.evidence.approvedAt = '2029-02-30T12:00:00Z'; },
  'missing OS': (data) => { data.evidence.installationResults.pop(); },
  'duplicate OS': (data) => { data.evidence.installationResults.push(structuredClone(data.evidence.installationResults[0])); },
  'prepared storage instead of fresh default': (data) => { data.evidence.installationResults[0].cell.storageProfile = 'prepared-quota'; },
  'manual path instead of one line': (data) => { data.evidence.installationResults[0].cell.entryPoint = 'archive'; },
  'failed install': (data) => { data.evidence.installationResults[0].result = 'fail'; },
  'stale install archive': (data) => { data.evidence.installationResults[0].archiveSHA256 = 'd'.repeat(64); },
  'stale install commit': (data) => { data.evidence.installationResults[0].sourceCommit = 'd'.repeat(40); },
  'missing active uninstall': (data) => { data.evidence.installationResults[0].checks.pop(); },
  'duplicate check': (data) => { data.evidence.installationResults[0].checks[1] = 'fresh-install'; },
  'late install evidence': (data) => { data.evidence.installationResults[0].completedAt = '2030-01-01T13:00:00Z'; },
  'escaping report path': (data) => { data.evidence.installationResults[0].report.path = 'docs/release-evidence/../private.md'; },
  'URL report': (data) => { data.evidence.installationResults[0].report.path = 'https://example.test/pass.md'; },
  'missing report': (data) => { data.files.clear(); },
  'changed report': (data) => { data.files.set(data.evidence.installationResults[0].report.path, Buffer.from('changed')); },
  'contradictory reused report': (data) => { data.evidence.independentReview.report = { ...data.evidence.supportPolicy.report, sha256: 'f'.repeat(64) }; },
  'absent independent approval': (data) => { data.evidence.independentReview.decision = 'pending'; },
  'not independent': (data) => { data.evidence.independentReview.independentOfImplementation = false; },
  'self review': (data) => { data.evidence.independentReview.reviewer = data.evidence.approvedBy.toLowerCase(); },
  'incomplete review scope': (data) => { data.evidence.independentReview.scopes.pop(); },
  'late independent review': (data) => { data.evidence.independentReview.completedAt = '2030-01-01T13:00:00Z'; },
  'unapproved support': (data) => { data.evidence.supportPolicy.decision = 'pending'; },
  'wrong supported release': (data) => { data.evidence.supportPolicy.versions = ['1.2.2']; },
  'invented support end date': (data) => { data.evidence.supportPolicy.terms.supportEnds = '2031-01-01'; },
  'invented response guarantee': (data) => { data.evidence.supportPolicy.terms.guaranteedResponse = true; },
  'invented fix guarantee': (data) => { data.evidence.supportPolicy.terms.guaranteedFixes = true; },
  'paid support claim': (data) => { data.evidence.supportPolicy.terms.model = 'commercial'; },
  'unapproved support maintainer': (data) => { data.evidence.supportPolicy.approvedBy = 'TestOtherMaintainer'; },
  'missing limits': (data) => { data.evidence.supportPolicy.deploymentLimits = ''; },
  'changed SECURITY.md': (data) => { data.securityPolicy = Buffer.from('unreviewed'); },
  'unapproved publication': (data) => { data.evidence.publicationDecision.decision = 'pending'; },
  'unapproved publisher': (data) => { data.evidence.publicationDecision.approvedBy = 'SomeoneElse'; },
  'production authorization': (data) => { data.evidence.publicationDecision.productionUseAllowed = true; },
  'important data authorization': (data) => { data.evidence.publicationDecision.importantDataAllowed = true; },
  'existing workloads authorized': (data) => { data.evidence.publicationDecision.freshDisposableOnly = false; },
  'unknown release class': (data) => { data.evidence.releaseClass = 'approved-anything'; },
  'missing evidence commit': (data) => { data.evidenceCommit = ''; },
  'missing workflow reference': (data) => { data.evidence.workflowRuns.pop(); },
  'duplicate workflow reference': (data) => { data.evidence.workflowRuns[1] = data.evidence.workflowRuns[0]; },
  'unsafe run ID': (data) => { data.evidence.workflowRuns[0].runId = '100/../../'; },
  'fractional attempt': (data) => { data.evidence.workflowRuns[0].attempt = 1.5; },
  'missing live workflow data': (data) => { data.workflows.clear(); },
};
for (const [name, mutate] of Object.entries(evidenceFailures)) {
  test(`rejects ${name}`, () => {
    const data = fixture();
    mutate(data);
    assert.throws(() => verifyReadiness(data));
  });
}

const workflowFailures = {
  'failed workflow': (actual) => { actual.run.conclusion = 'failure'; },
  'in-progress workflow': (actual) => { actual.run.status = 'in_progress'; },
  'different source commit': (actual) => { actual.run.head_sha = 'c'.repeat(40); },
  'foreign repository': (actual) => { actual.run.repository.full_name = 'someone/Stackfort'; },
  'fork source': (actual) => { actual.run.head_repository.full_name = 'someone/Stackfort'; },
  'PR run': (actual) => { actual.run.event = 'pull_request'; },
  'wrong workflow': (actual) => { actual.run.path = '.github/workflows/other.yml'; },
  'superseded successful attempt': (actual) => { actual.run.run_attempt = 2; },
  'wrong run ID': (actual) => { actual.run.id = 999; },
  'truncated jobs': (actual) => { actual.jobs.jobs.pop(); },
  'missing required job': (actual) => { actual.jobs.jobs.pop(); actual.jobs.total_count--; },
  'unexpected job': (actual) => { actual.jobs.jobs[0].name = 'Fake success'; },
  'failed job': (actual) => { actual.jobs.jobs[0].conclusion = 'failure'; },
  'skipped required job': (actual) => { actual.jobs.jobs[0].conclusion = 'skipped'; },
  'old attempt job': (actual) => { actual.jobs.jobs[0].run_attempt = 2; },
  'foreign run job': (actual) => { actual.jobs.jobs[0].run_id = 999; },
  'wrong job commit': (actual) => { actual.jobs.jobs[0].head_sha = 'c'.repeat(40); },
  'in-progress job': (actual) => { actual.jobs.jobs[0].status = 'in_progress'; },
  'duplicate job': (actual) => { actual.jobs.jobs[1] = actual.jobs.jobs[0]; },
};
for (const [name, mutate] of Object.entries(workflowFailures)) {
  test(`rejects ${name}`, () => {
    const data = fixture();
    mutate(data.workflows.get(100));
    assert.throws(() => verifyReadiness(data));
  });
}

test('rejects empty/duplicate/unknown policies and allows explicitly narrowed supported scope', () => {
  for (const matrix of [[], [policy.installationMatrix[0], policy.installationMatrix[0]], [{ ...policy.installationMatrix[0], architecture: 'arm64' }]]) {
    assert.throws(() => validatePolicy({ ...policy, installationMatrix: matrix }));
  }
  const data = fixture();
  data.policy.installationMatrix = data.policy.installationMatrix.slice(0, 1);
  data.evidence.installationResults = data.evidence.installationResults.slice(0, 1);
  assert.equal(verifyReadiness(data).installationCells.length, 1);
  for (const terms of [
    { ...policy.support, guaranteedResponse: true },
    { ...policy.support, guaranteedFixes: true },
    { ...policy.support, supportEnds: '2099-01-01' },
    { ...policy.support, channels: ['github-issues'] },
  ]) assert.throws(() => validatePolicy({ ...policy, support: terms }));
});

test('canonical evidence rejects duplicate keys, truncation, oversized documents and unknown CLI flags', () => {
  assert.deepEqual(parseDocument('{\n  "schemaVersion": 1\n}\n', 'test', true), { schemaVersion: 1 });
  for (const source of ['{"schemaVersion":1,"schemaVersion":2}', '{', ' '.repeat((1 << 20) + 1), '{"schemaVersion":1}']) {
    assert.throws(() => parseDocument(source, 'test', true));
  }
  for (const args of [[], ['--force', 'yes'], ['--mode', 'policy'], ['--mode', 'policy', '--mode', 'policy'], ['--mode', 'policy', '--policy', 'missing', '--evidence', 'ignored']]) {
    assert.throws(() => main(args));
  }
});

test('authorized experimental beta retains other checks and requires exact full-system removal evidence', () => {
  const result = verifyReadiness(experimentalFixture());
  assert.equal(result.recordedIndependentReview, false);
  assert.equal(result.productionUseAllowed, false);
  assert.equal(result.independentReviewDisclosure, experimentalDisclosure);
  assert.equal(result.removalMethod, 'full-system-reprovision');
  assert.equal(result.removalDisclosure, experimentalRemovalDisclosure);
  assert.deepEqual(experimentalChecks.filter((check) => check !== 'full-system-reprovision-removal'), requiredChecks.filter((check) => check !== 'active-uninstall'));
  for (const mutate of [
    (data) => { data.evidence.independentReview.decision = 'approved'; },
    (data) => { data.evidence.independentReview.disclosure = 'Audited and production ready.'; },
    (data) => { data.evidence.independentReview.reviewer = 'InventedReviewer'; },
    (data) => { data.evidence.publicationDecision.releaseClass = 'reviewed-release'; },
    (data) => { data.evidence.publicationDecision.decision = 'pending'; },
    (data) => { data.evidence.installationResults[0].checks.pop(); },
    (data) => { data.workflows.get(101).run.conclusion = 'failure'; },
    (data) => {
      data.expected.version = '1.2.3';
      data.evidence.candidate.version = '1.2.3';
      data.evidence.candidate.archive = 'stackfort-1.2.3-linux-amd64.tar.gz';
    },
  ]) {
    const data = experimentalFixture();
    mutate(data);
    assert.throws(() => verifyReadiness(data));
  }
});

const removalFailures = {
  'missing whole-system removal record': (data) => { delete data.evidence.installationResults[0].removal; },
  'passive package removal only': (data) => { data.evidence.installationResults[0].removal.method = 'passive-carrier-removal'; },
  'snapshot rollback only': (data) => { data.evidence.installationResults[0].removal.method = 'snapshot-rollback'; },
  'claimed in-place uninstall': (data) => { data.evidence.installationResults[0].removal.method = 'active-uninstall'; },
  'failed reprovisioning': (data) => { data.evidence.installationResults[0].removal.result = 'fail'; },
  'unknown removal field': (data) => { data.evidence.installationResults[0].removal.force = true; },
  'different active target': (data) => { data.evidence.installationResults[0].removal.targetBefore = 'other-vm'; },
  'missing target identity': (data) => { data.evidence.installationResults[0].removal.targetAfter = ''; },
  'missing authenticated installation media hash': (data) => { data.evidence.installationResults[0].removal.installerMediaSHA256 = ''; },
  'different removal version': (data) => { data.evidence.installationResults[0].removal.candidate.version = '1.2.2'; },
  'different removal source commit': (data) => { data.evidence.installationResults[0].removal.candidate.commit = 'f'.repeat(40); },
  'different removal archive hash': (data) => { data.evidence.installationResults[0].removal.candidate.archiveSHA256 = 'f'.repeat(64); },
  'different removal build run': (data) => { data.evidence.installationResults[0].removal.candidate.build.runId++; },
  'different removal build attempt': (data) => { data.evidence.installationResults[0].removal.candidate.build.attempt++; },
  'different removal artifact ID': (data) => { data.evidence.installationResults[0].removal.candidate.build.artifactId++; },
  'different removal ZIP hash': (data) => { data.evidence.installationResults[0].removal.candidate.build.artifactSHA256 = 'f'.repeat(64); },
  'duplicate removal check': (data) => { data.evidence.installationResults[0].removal.checks[1] = data.evidence.installationResults[0].removal.checks[0]; },
  'late removal completion': (data) => { data.evidence.installationResults[0].removal.completedAt = '2030-01-01T10:01:00Z'; },
  'missing removal report': (data) => { data.files.delete(data.evidence.installationResults[0].removal.report.path); },
  'changed removal report': (data) => { data.files.set(data.evidence.installationResults[0].removal.report.path, Buffer.from('altered')); },
  'unsafe removal report': (data) => { data.evidence.installationResults[0].removal.report.path = 'docs/release-evidence/../outside.md'; },
  'no candidate removal approval': (data) => { delete data.evidence.publicationDecision.removalMethod; },
  'different candidate removal approval': (data) => { data.evidence.publicationDecision.removalMethod = 'passive-carrier-removal'; },
  'no support removal disclosure': (data) => { delete data.evidence.supportPolicy.removalMethod; },
  'different support removal promise': (data) => { data.evidence.supportPolicy.removalMethod = 'in-place'; },
  'missing general removal authorization': (data) => { delete data.policy.experimentalBeta.removal; },
  'unapproved removal policy': (data) => { data.policy.experimentalBeta.removal.authorizedBy = 'SomeoneElse'; },
  'different removal policy date': (data) => { data.policy.experimentalBeta.removal.authorizedOn = '2026-09-11'; },
  'invented in-place uninstaller': (data) => { data.policy.experimentalBeta.removal.inPlaceUninstallerAvailable = true; },
  'data-preserving removal claim': (data) => { data.policy.experimentalBeta.removal.destroysAllServerData = false; },
};
for (const check of experimentalChecks) removalFailures[`missing experimental ${check}`] = (data) => {
  data.evidence.installationResults[0].checks = data.evidence.installationResults[0].checks.filter((value) => value !== check);
};
for (const check of reprovisionChecks) removalFailures[`missing reprovision ${check}`] = (data) => {
  data.evidence.installationResults[0].removal.checks = data.evidence.installationResults[0].removal.checks.filter((value) => value !== check);
};
for (const [name, mutate] of Object.entries(removalFailures)) {
  test(`rejects experimental ${name}`, () => {
    const data = experimentalFixture();
    mutate(data);
    assert.throws(() => verifyReadiness(data));
  });
}

test('reviewed releases cannot substitute reprovision evidence for active uninstall', () => {
  const data = fixture();
  data.evidence.installationResults[0].checks = [...experimentalChecks];
  assert.throws(() => verifyReadiness(data), /installation checks/);
  data.evidence.installationResults[0].checks = [...requiredChecks];
  data.evidence.installationResults[0].removal = experimentalFixture().evidence.installationResults[0].removal;
  assert.throws(() => verifyReadiness(data), /unknown fields/);
});

test('release workflow renders the validated removal warning', () => {
  const workflow = readFileSync(path.join(root, '.github/workflows/release.yml'), 'utf8');
  assert.match(workflow, /jq -er '\.removalDisclosure' dist\/release-readiness\.json >>"\$notes"/);
});

test('CLI fails closed on absent evidence and does not print a success receipt', () => {
  const result = spawnSync(process.execPath, [path.join(root, 'scripts/verify-release-readiness.mjs'), '--mode', 'inventory', '--policy', path.join(root, 'packaging/releases/readiness-policy.json'), '--evidence', path.join(root, 'packaging/releases/evidence/absent.json'), '--version', '1.2.3', '--commit', 'a'.repeat(40), '--archive-sha256', 'b'.repeat(64)], { encoding: 'utf8' });
  assert.equal(result.status, 1);
  assert.equal(result.stdout, '');
  assert.match(result.stderr, /^Release readiness blocked:/);
});

for (const releaseClass of ['reviewed-release', 'experimental-beta']) test(`publication wrapper pins ${releaseClass} files, verifies API jobs, and stays closed on failure`, { skip: process.platform === 'win32' }, () => {
  const directory = mkdtempSync(path.join(tmpdir(), 'stackfort-readiness-test-'));
  try {
    for (const name of ['scripts', 'packaging/releases', 'dist', 'test-bin']) mkdirSync(path.join(directory, name), { recursive: true });
    for (const name of ['verify-release-readiness.sh', 'verify-release-readiness.mjs']) cpSync(path.join(root, 'scripts', name), path.join(directory, 'scripts', name));
    writeFileSync(path.join(directory, 'packaging/releases/readiness-policy.json'), canonical(policy));
    const data = releaseClass === 'experimental-beta' ? experimentalFixture() : fixture();
    // Use the real clock for the executable CLI; fixture decisions remain fake.
    data.evidence.approvedAt = '2020-01-01T12:00:00Z';
    for (const result of data.evidence.installationResults) {
      result.completedAt = '2020-01-01T10:00:00Z';
      if (result.removal) result.removal.completedAt = '2020-01-01T09:59:00Z';
    }
    if (releaseClass === 'reviewed-release') data.evidence.independentReview.completedAt = '2020-01-01T11:00:00Z';
    writeFileSync(path.join(directory, 'SECURITY.md'), data.securityPolicy);
    writeFileSync(path.join(directory, 'dist', data.evidence.candidate.archive), data.archive);
    const gitOptions = { cwd: directory, env: { ...process.env, GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null' } };
    execFileSync('git', ['init', '-q'], gitOptions);
    execFileSync('git', ['add', '.'], gitOptions);
    execFileSync('git', ['-c', 'user.name=Readiness Test', '-c', 'user.email=readiness@example.invalid', '-c', 'commit.gpgsign=false', 'commit', '-qm', 'Synthetic readiness wrapper fixture'], gitOptions);
    const commit = execFileSync('git', ['rev-parse', 'HEAD'], { ...gitOptions, encoding: 'utf8' }).trim();
    data.expected.commit = commit;
    data.evidence.candidate.commit = commit;
    writeFileSync(path.join(directory, 'dist/candidate-promotion.json'), canonical({ schemaVersion: 1, kind: 'verified-candidate-promotion', candidate: data.evidence.candidate, publicationAuthorized: false }));
    for (const result of data.evidence.installationResults) {
      result.sourceCommit = commit;
      if (result.removal) result.removal.candidate.commit = commit;
    }
    for (const actual of data.workflows.values()) {
      actual.run.head_sha = commit;
      for (const job of actual.jobs.jobs) job.head_sha = commit;
    }
    const replies = {};
    const prefix = 'repos/RTBGG/Stackfort/';
    replies[prefix + 'git/ref/heads/main'] = data.evidenceCommit;
    replies[prefix + `compare/${commit}...${data.evidenceCommit}`] = 'ahead';
    const contents = (name) => prefix + `contents/${name}?ref=${data.evidenceCommit}`;
    const evidenceEndpoint = contents(`packaging/releases/evidence/${data.expected.version}.json`);
    replies[evidenceEndpoint] = canonical(data.evidence);
    replies[contents('packaging/releases/readiness-policy.json')] = canonical(policy);
    for (const [name, content] of data.files) replies[contents(name)] = content.toString('utf8');
    for (const [id, actual] of data.workflows) {
      replies[prefix + `actions/runs/${id}`] = JSON.stringify(actual.run);
      replies[prefix + `actions/runs/${id}/attempts/1/jobs?per_page=100`] = JSON.stringify([actual.jobs]);
    }
    const responseFile = path.join(directory, 'test-replies.json');
    writeFileSync(responseFile, JSON.stringify(replies));
    writeFileSync(path.join(directory, 'test-bin/gh'), `#!/usr/bin/env node\nconst fs = require('node:fs');\nconst endpoint = process.argv.find((arg) => arg.startsWith('repos/'));\nconst replies = JSON.parse(fs.readFileSync(process.env.STACKFORT_READINESS_TEST_DATA, 'utf8'));\nif (!(endpoint in replies)) process.exit(1);\nprocess.stdout.write(replies[endpoint] + (endpoint.includes('/contents/') ? '' : '\\n'));\n`, { mode: 0o755 });
    const env = { ...process.env, PATH: path.join(directory, 'test-bin') + path.delimiter + process.env.PATH, STACKFORT_READINESS_TEST_DATA: responseFile, GITHUB_EVENT_NAME: 'push', GITHUB_REPOSITORY: 'RTBGG/Stackfort', GITHUB_SHA: commit, GITHUB_REF: `refs/tags/v${data.expected.version}`, VERSION: data.expected.version };
    const run = (overrides = {}) => spawnSync('bash', ['scripts/verify-release-readiness.sh'], { cwd: directory, env: { ...env, ...overrides }, encoding: 'utf8' });
    const success = run();
    assert.equal(success.status, 0, success.stdout + success.stderr);
    assert.equal(JSON.parse(readFileSync(path.join(directory, 'dist/release-readiness.json'), 'utf8')).candidate.commit, commit);
    assert.equal(JSON.parse(readFileSync(path.join(directory, 'dist/release-readiness.json'), 'utf8')).removalMethod, releaseClass === 'experimental-beta' ? 'full-system-reprovision' : 'active-uninstall');
    const protectedReceipt = readFileSync(path.join(directory, 'dist/release-readiness.json'));
    const failures = {
      'missing readiness evidence': (values) => { delete values[evidenceEndpoint]; },
      'changed evidence-branch policy': (values) => { values[contents('packaging/releases/readiness-policy.json')] = canonical({ ...policy, installationMatrix: [...policy.installationMatrix, { ...policy.installationMatrix[0], distribution: 'ubuntu', version: '26.04' }] }); },
      'diverged evidence history': (values) => { values[prefix + `compare/${commit}...${data.evidenceCommit}`] = 'diverged'; },
      'changed report bytes': (values) => { values[contents(data.evidence.installationResults[0].report.path)] = 'changed'; },
      'missing review or removal report': (values) => { delete values[contents(releaseClass === 'experimental-beta' ? data.evidence.installationResults[0].removal.report.path : data.evidence.independentReview.report.path)]; },
      'failed live workflow': (values) => { values[prefix + 'actions/runs/100'] = JSON.stringify({ ...data.workflows.get(100).run, conclusion: 'failure' }); },
      'missing workflow API response': (values) => { delete values[prefix + 'actions/runs/100']; },
      'truncated paginated jobs': (values) => { const jobs = structuredClone(data.workflows.get(100).jobs); jobs.jobs.pop(); values[prefix + 'actions/runs/100/attempts/1/jobs?per_page=100'] = JSON.stringify([jobs]); },
      'malformed jobs pages': (values) => { values[prefix + 'actions/runs/100/attempts/1/jobs?per_page=100'] = '[]'; },
    };
    if (releaseClass === 'experimental-beta') {
      failures['missing tested reprovisioning'] = (values) => {
        const evidence = JSON.parse(values[evidenceEndpoint]);
        delete evidence.installationResults[0].removal;
        values[evidenceEndpoint] = canonical(evidence);
      };
      failures['passive-carrier removal substituted'] = (values) => {
        const evidence = JSON.parse(values[evidenceEndpoint]);
        evidence.installationResults[0].removal.method = 'passive-carrier-removal';
        values[evidenceEndpoint] = canonical(evidence);
      };
    }
    for (const [name, mutate] of Object.entries(failures)) {
      const changed = structuredClone(replies);
      mutate(changed);
      writeFileSync(responseFile, JSON.stringify(changed));
      assert.notEqual(run().status, 0, `${name} passed`);
      assert.deepEqual(readFileSync(path.join(directory, 'dist/release-readiness.json')), protectedReceipt, `${name} replaced a previous receipt`);
    }
    writeFileSync(responseFile, JSON.stringify(replies));
    assert.notEqual(run({ GITHUB_EVENT_NAME: 'workflow_dispatch' }).status, 0, 'manual build claimed publication');
    assert.notEqual(run({ GITHUB_REF: 'refs/tags/v9.9.9' }).status, 0, 'wrong tag passed');
    assert.deepEqual(readFileSync(path.join(directory, 'dist/release-readiness.json')), protectedReceipt, 'failure replaced a previous receipt');
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
