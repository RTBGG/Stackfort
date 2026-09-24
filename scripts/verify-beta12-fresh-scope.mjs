// SPDX-License-Identifier: AGPL-3.0-or-later
// One candidate-specific maintainer exception, not a general upgrade-gate switch.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { hash, parseDocument } from './verify-release-readiness.mjs';

export const beta12Candidate = Object.freeze({
  version: '0.1.0-beta.12',
  commit: 'f4d1a7947af4ffbdc2cbf28d2f618c10833cefec',
  archive: 'stackfort-0.1.0-beta.12-linux-amd64.tar.gz',
  archiveSHA256: '446cf63a51994993b1b9cb337b0067967714122d41c6ff155720571f887268a9',
  build: { runId: 36005061649, attempt: 1, artifactId: 10811230131, artifactSHA256: '8cf13cd7084cc1a21497a1017bbd45225cdf924347fa4ee95fc66e6906dca485' },
});
export const tagQualification = Object.freeze({
  runId: 36007265703, attempt: 1, artifactId: 10810852574,
  artifactSHA256: '110088185a8d53af35572453d46b984867aed194fe5e1ef13d4da2bc08326eba',
  attestationSHA256: '67307840e6be9d9ff659fa22d6cb965098f4edc3742b4929a956448ef0659555',
});
export const scopeReport = Object.freeze({
  path: 'docs/release-evidence/2026-09-24-beta12-fresh-install-scope.md',
  sha256: 'c03cd084548615551909c8cc866ab8cda80e91fb000a2868d4ab41a9df957c2a',
});
export const upgradeDisclosure = 'Beta.12 is for fresh installations only. Upgrades from Beta.10, Beta.11 or any other installed release are unsupported and unqualified. Do not use the updater or install Beta.12 over an existing installation. The failed predecessor qualification has not been converted into a pass.';

export function verifyFreshScope({ candidate, report, run, artifacts }) {
  assert.deepEqual(candidate, beta12Candidate, 'exception belongs to another candidate');
  assert.equal(hash(report), scopeReport.sha256, 'maintainer scope report changed');
  assert.equal(run.id, tagQualification.runId);
  assert.equal(run.run_attempt, tagQualification.attempt);
  assert.equal(run.head_sha, candidate.commit);
  assert.equal(run.path, '.github/workflows/release.yml');
  assert.equal(run.event, 'push');
  assert.equal(run.head_branch, 'v0.1.0-beta.12');
  assert.equal(run.status, 'completed');
  // This run retained genuine tag attestations BEFORE readiness/upgrade gates.
  // Failure is expected and is NOT used as evidence that those gates passed.
  assert.equal(run.conclusion, 'failure');
  assert.equal(run.repository?.full_name, 'RTBGG/Stackfort');
  assert.equal(run.head_repository?.full_name, 'RTBGG/Stackfort');
  assert.ok(Array.isArray(artifacts.artifacts) && Number.isSafeInteger(artifacts.total_count));
  assert.equal(artifacts.total_count, artifacts.artifacts.length, 'truncated artifact inventory');
  const selected = artifacts.artifacts.filter(a => a.name === 'stackfort-tag-candidate-0.1.0-beta.12-attempt-1');
  assert.equal(selected.length, 1);
  const artifact = selected[0];
  assert.equal(artifact.id, tagQualification.artifactId);
  assert.equal(artifact.expired, false);
  assert.equal(artifact.size_in_bytes, 313208244);
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
    process.stderr.write(`Beta.12 fresh-install scope blocked: ${error.message}\n`);
    process.exitCode = 1;
  }
}
