// SPDX-License-Identifier: AGPL-3.0-or-later
// One candidate-specific maintainer exception, not a general upgrade-gate switch.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { hash, parseDocument } from './verify-release-readiness.mjs';

export const beta11Candidate = Object.freeze({
  version: '0.1.0-beta.11',
  commit: 'ec117da052b18e051870a224b89bee4b0ddf64e7',
  archive: 'stackfort-0.1.0-beta.11-linux-amd64.tar.gz',
  archiveSHA256: 'd6fec6d5a894d0c120aef5d83829b335a8875b68a181c45a7ad2bb9d139d1304',
  build: { runId: 35991617289, attempt: 1, artifactId: 10804961581, artifactSHA256: '97c6acdb9aaa6695c880ab9a54c26724d3182394989ef38b7ae580d09ebe2c9e' },
});
export const tagQualification = Object.freeze({
  runId: 35993227841, attempt: 1, artifactId: 10805515238,
  artifactSHA256: 'fe4ad0a2656207e0af5e8696138d0b4e9c00c34d899fe319513653e5fe3e2c00',
  attestationSHA256: '16570b5f6f4f8e1bc35f5ac31c80b5194a924956e6337df67953236470a37d1f',
});
export const scopeReport = Object.freeze({
  path: 'docs/release-evidence/2026-09-24-beta11-fresh-install-scope.md',
  sha256: '1dfb16e04524c5b8b6c2813ef762865c94367451fd8e072769feff24037dc3e7',
});
export const upgradeDisclosure = 'Beta.11 is for fresh installations only. Upgrades from Beta.10 or any other installed release are unsupported and unqualified. Do not use the updater or install Beta.11 over an existing installation. The failed predecessor qualification has not been converted into a pass.';

export function verifyFreshScope({ candidate, report, run, artifacts }) {
  assert.deepEqual(candidate, beta11Candidate, 'exception belongs to another candidate');
  assert.equal(hash(report), scopeReport.sha256, 'maintainer scope report changed');
  assert.equal(run.id, tagQualification.runId);
  assert.equal(run.run_attempt, tagQualification.attempt);
  assert.equal(run.head_sha, candidate.commit);
  assert.equal(run.path, '.github/workflows/release.yml');
  assert.equal(run.event, 'push');
  assert.equal(run.head_branch, 'v0.1.0-beta.11');
  assert.equal(run.status, 'completed');
  // This run retained genuine tag attestations BEFORE readiness/upgrade gates.
  // Failure is expected and is NOT used as evidence that those gates passed.
  assert.equal(run.conclusion, 'failure');
  assert.equal(run.repository?.full_name, 'RTBGG/Stackfort');
  assert.equal(run.head_repository?.full_name, 'RTBGG/Stackfort');
  assert.ok(Array.isArray(artifacts.artifacts) && Number.isSafeInteger(artifacts.total_count));
  assert.equal(artifacts.total_count, artifacts.artifacts.length, 'truncated artifact inventory');
  const selected = artifacts.artifacts.filter(a => a.name === 'stackfort-tag-candidate-0.1.0-beta.11-attempt-1');
  assert.equal(selected.length, 1);
  const artifact = selected[0];
  assert.equal(artifact.id, tagQualification.artifactId);
  assert.equal(artifact.expired, false);
  assert.equal(artifact.size_in_bytes, 313174334);
  assert.equal(artifact.digest, 'sha256:' + tagQualification.artifactSHA256);
  assert.equal(artifact.workflow_run?.id, run.id);
  assert.equal(artifact.workflow_run?.head_sha, candidate.commit);
  return {
    schemaVersion: 1, kind: 'fresh-install-only-scope', candidate, tagQualification,
    authorizedBy: 'RTBGG', authorizationReport: scopeReport,
    supportedUpgradePredecessors: [], upgradeQualificationPassed: false,
    unsupportedPredecessor: { version: '0.1.0-beta.10', archiveSHA256: '8e7ce111d6cefda172e74bd7a71380c181e66a726d8f157f6bd1af8944e93263' },
    predecessorGloballyRetired: false, remainingTechnicalGatesRequired: true,
    disclosure: upgradeDisclosure,
  };
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    assert.equal(process.argv.length, 6, 'expected promotion, report, run, artifacts');
    const [promotion, report, run, artifacts] = process.argv.slice(2);
    const json = name => parseDocument(readFileSync(name, 'utf8'), name);
    process.stdout.write(JSON.stringify(verifyFreshScope({ candidate: json(promotion).candidate, report: readFileSync(report), run: json(run), artifacts: json(artifacts) }), null, 2) + '\n');
  } catch (error) {
    process.stderr.write(`Beta.11 fresh-install scope blocked: ${error.message}\n`);
    process.exitCode = 1;
  }
}
