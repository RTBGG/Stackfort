// SPDX-License-Identifier: AGPL-3.0-or-later
// One candidate-specific maintainer exception, not a general upgrade-gate switch.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { hash, parseDocument } from './verify-release-readiness.mjs';

export const beta13Candidate = Object.freeze({
  version: '0.1.0-beta.13',
  commit: '991f7df6b27093588a69d0dbea2e3eab7a7ceaae',
  archive: 'stackfort-0.1.0-beta.13-linux-amd64.tar.gz',
  archiveSHA256: 'b56104eebc6f535d88d9b3de17bcf95efa32c97dc88997b1e992619725c2ea91',
  build: { runId: 36235431432, attempt: 1, artifactId: 10904191959, artifactSHA256: '38895891e3641466319f17e8b962780134e814d23fc7b60a0420e3cd1f6d8ea7' },
});
export const tagQualification = Object.freeze({
  runId: 36237290259, attempt: 1, artifactId: 10905165398,
  artifactSHA256: '1e5bbb2a9904be8cd9837acc22fa5ac33083d08905ed2296ee0653812aabaced',
  attestationSHA256: '7c2219d067fe4cd6471ba5b70c7b65e86b61c9c07ca2de41fbc4dba2db06d6ea',
});
export const scopeReport = Object.freeze({
  path: 'docs/release-evidence/2026-09-26-beta13-fresh-install-scope.md',
  sha256: '6f02d3cb0d42ce9fec8b60c8cb2ac427833ccda25caa1e65e0417f9426903b12',
});
export const upgradeDisclosure = 'Beta.13 is for fresh installations only. Upgrades from Beta.10, Beta.11, Beta.12 or any other installed release are unsupported and unqualified. Do not use the updater or install Beta.13 over an existing installation. The failed predecessor qualification has not been converted into a pass.';

export function verifyFreshScope({ candidate, report, run, artifacts }) {
  assert.deepEqual(candidate, beta13Candidate, 'exception belongs to another candidate');
  assert.equal(hash(report), scopeReport.sha256, 'maintainer scope report changed');
  assert.equal(run.id, tagQualification.runId);
  assert.equal(run.run_attempt, tagQualification.attempt);
  assert.equal(run.head_sha, candidate.commit);
  assert.equal(run.path, '.github/workflows/release.yml');
  assert.equal(run.event, 'push');
  assert.equal(run.head_branch, 'v0.1.0-beta.13');
  assert.equal(run.status, 'completed');
  // This run retained genuine tag attestations BEFORE readiness/upgrade gates.
  // Failure is expected and is NOT used as evidence that those gates passed.
  assert.equal(run.conclusion, 'failure');
  assert.equal(run.repository?.full_name, 'RTBGG/Stackfort');
  assert.equal(run.head_repository?.full_name, 'RTBGG/Stackfort');
  assert.ok(Array.isArray(artifacts.artifacts) && Number.isSafeInteger(artifacts.total_count));
  assert.equal(artifacts.total_count, artifacts.artifacts.length, 'truncated artifact inventory');
  const selected = artifacts.artifacts.filter(a => a.name === 'stackfort-tag-candidate-0.1.0-beta.13-attempt-1');
  assert.equal(selected.length, 1);
  const artifact = selected[0];
  assert.equal(artifact.id, tagQualification.artifactId);
  assert.equal(artifact.expired, false);
  assert.equal(artifact.size_in_bytes, 313239196);
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
    process.stderr.write(`Beta.13 fresh-install scope blocked: ${error.message}\n`);
    process.exitCode = 1;
  }
}
