// SPDX-License-Identifier: AGPL-3.0-or-later
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { beta11Candidate, tagQualification, verifyFreshScope } from './verify-beta11-fresh-scope.mjs';

function fixture() {
  return {
    candidate: structuredClone(beta11Candidate),
    report: readFileSync(new URL('../docs/release-evidence/2026-09-24-beta11-fresh-install-scope.md', import.meta.url)),
    run: { id: 35993227841, run_attempt: 1, head_sha: beta11Candidate.commit, path: '.github/workflows/release.yml', event: 'push', head_branch: 'v0.1.0-beta.11', status: 'completed', conclusion: 'failure', repository: { full_name: 'RTBGG/Stackfort' }, head_repository: { full_name: 'RTBGG/Stackfort' } },
    artifacts: { total_count: 1, artifacts: [{ id: 10805515238, name: 'stackfort-tag-candidate-0.1.0-beta.11-attempt-1', expired: false, size_in_bytes: 313174334, digest: 'sha256:' + tagQualification.artifactSHA256, workflow_run: { id: 35993227841, head_sha: beta11Candidate.commit } }] },
  };
}
test('fresh-only exception records no upgrade pass or global predecessor retirement', () => {
  const result = verifyFreshScope(fixture());
  assert.deepEqual(result.supportedUpgradePredecessors, []);
  assert.equal(result.upgradeQualificationPassed, false);
  assert.equal(result.predecessorGloballyRetired, false);
  assert.equal(result.remainingTechnicalGatesRequired, true);
});
test('different candidate, scope, source, attempt or artifact fail closed', () => {
  const changes = [
    v => { v.candidate.version = '0.1.0-beta.12'; },
    v => { v.candidate.commit = 'a'.repeat(40); },
    v => { v.candidate.archiveSHA256 = 'a'.repeat(64); },
    v => { v.candidate.build.artifactId++; },
    v => { v.report = Buffer.from('waive all tests'); },
    v => { v.run.run_attempt++; },
    v => { v.run.head_sha = 'b'.repeat(40); },
    v => { v.run.head_branch = 'main'; },
    v => { v.run.event = 'workflow_dispatch'; },
    v => { v.run.status = 'in_progress'; },
    v => { v.run.path = '.github/workflows/other.yml'; },
    v => { v.run.repository.full_name = 'other/Stackfort'; },
    v => { v.run.head_repository.full_name = 'other/Stackfort'; },
    v => { v.artifacts.total_count++; },
    v => { v.artifacts.artifacts = []; v.artifacts.total_count = 0; },
    v => { v.artifacts.artifacts.push(v.artifacts.artifacts[0]); v.artifacts.total_count++; },
    ...['id', 'name', 'expired', 'size_in_bytes', 'digest'].map(key => v => { v.artifacts.artifacts[0][key] = null; }),
    v => { v.artifacts.artifacts[0].workflow_run.id++; },
    v => { v.artifacts.artifacts[0].workflow_run.head_sha = 'c'.repeat(40); },
  ];
  for (const change of changes) { const value = fixture(); change(value); assert.throws(() => verifyFreshScope(value)); }
});
