// SPDX-License-Identifier: AGPL-3.0-or-later

// Versioned, fail-closed publication evidence validation. This validates
// recorded human decisions; it cannot perform an independent security audit.
import { createHash } from 'node:crypto';
import { readFileSync, lstatSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repository = 'RTBGG/Stackfort';
const policyName = 'stackfort-release-readiness-v1';
const sha256 = /^[0-9a-f]{64}$/;
const commitPattern = /^[0-9a-f]{40}$/;
const versionPattern = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$/;
const identityPattern = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;
const reportPattern = /^(?:infra\/host-tests\/results|docs\/release-evidence)\/[A-Za-z0-9][A-Za-z0-9._/-]{0,220}\.(?:md|json)$/;
const maximumJSON = 1 << 20;
export const requiredChecks = Object.freeze([
  'fresh-install', 'idempotent-rerun', 'normal-reboot', 'host-security',
  'tenant-isolation', 'quota-enforcement', 'waf-cache', 'rootless-oci',
  'failure-recovery', 'active-uninstall',
]);
export const experimentalChecks = Object.freeze([
  ...requiredChecks.filter((check) => check !== 'active-uninstall'),
  'full-system-reprovision-removal',
]);
export const reprovisionChecks = Object.freeze([
  'active-candidate-installed', 'authenticated-distribution-installer',
  'complete-os-disk-provisioning', 'fresh-os-boot', 'stackfort-state-absent',
  'stackfort-services-absent', 'hosting-data-absent',
]);
export const experimentalRemovalDisclosure = 'No in-place uninstaller is available for this experimental beta. Removal requires complete operating-system reinstallation and irreversibly removes all server data, configuration and services. Removing the passive release package is not removal of Stackfort.';
export const reviewScopes = Object.freeze([
  'authentication', 'agent-rpc', 'file-archive', 'phpmyadmin-handoff',
  'updater', 'native-installer',
]);
export const experimentalDisclosure = 'No independent security review has been performed. Experimental beta for fresh disposable test servers only; not for production or important data.';
export const requiredWorkflows = Object.freeze([
  { path: '.github/workflows/ci.yml', jobs: ['Workflow hygiene', 'Go', 'Web', 'Reproducible artifacts'], optional: [] },
  { path: '.github/workflows/security.yml', jobs: ['Secret scanning', 'Go vulnerability analysis', 'CodeQL (go)', 'CodeQL (javascript-typescript)'], optional: ['Dependency review'] },
]);

function requireValue(condition, message) {
  if (!condition) throw new Error(message);
}

function object(value, keys, label) {
  requireValue(value !== null && typeof value === 'object' && !Array.isArray(value), `${label}: expected object`);
  requireValue(Object.keys(value).sort().join('\0') === [...keys].sort().join('\0'), `${label}: missing or unknown fields`);
}

function text(value, label, maximum = 2048) {
  requireValue(typeof value === 'string' && value.trim() === value && value.length > 0 && value.length <= maximum && !/[\x00-\x1f\x7f]/.test(value), `${label}: invalid text`);
}

function digest(value, label) {
  requireValue(typeof value === 'string' && sha256.test(value), `${label}: invalid SHA-256`);
}

function identity(value, label) {
  requireValue(typeof value === 'string' && identityPattern.test(value), `${label}: human GitHub identity required`);
}

function timestamp(value, now, label) {
  requireValue(typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value), `${label}: UTC timestamp required`);
  const time = Date.parse(value);
  requireValue(Number.isFinite(time) && new Date(time).toISOString() === value.replace('Z', '.000Z') && time <= now, `${label}: invalid or future timestamp`);
  return time;
}

function exactSet(values, expected, label) {
  requireValue(Array.isArray(values) && values.length === expected.length && values.every((value) => typeof value === 'string') && [...values].sort().join('\0') === [...expected].sort().join('\0'), `${label}: missing, duplicate or unexpected items`);
}

function cellKey(cell) {
  return [cell.distribution, cell.version, cell.architecture, cell.entryPoint, cell.storageProfile].join('/');
}

function installationCell(cell, label) {
  object(cell, ['distribution', 'version', 'architecture', 'entryPoint', 'storageProfile'], label);
  requireValue(['debian/13', 'ubuntu/26.04', 'rocky/10'].includes(`${cell.distribution}/${cell.version}`) && cell.architecture === 'amd64' && cell.entryPoint === 'one-line' && cell.storageProfile === 'fresh-default', `${label}: unsupported installation cell`);
}

export function validatePolicy(policy) {
  object(policy, ['schemaVersion', 'policy', 'repository', 'experimentalBeta', 'support', 'installationMatrix'], 'policy');
  requireValue(policy.schemaVersion === 1 && policy.policy === policyName && policy.repository === repository, 'unsupported readiness policy');
  const experimental = policy.experimentalBeta;
  object(experimental, ['authorizedBy', 'authorizedOn', 'independentReview', 'freshDisposableOnly', 'productionUseAllowed', 'importantDataAllowed', 'removal'], 'experimental beta policy');
  requireValue(experimental.authorizedBy === 'RTBGG' && experimental.authorizedOn === '2026-09-12' && experimental.independentReview === 'not-performed' && experimental.freshDisposableOnly === true && experimental.productionUseAllowed === false && experimental.importantDataAllowed === false, 'unsupported experimental beta authorization');
  object(experimental.removal, ['authorizedBy', 'authorizedOn', 'method', 'inPlaceUninstallerAvailable', 'destroysAllServerData'], 'experimental removal policy');
  requireValue(experimental.removal.authorizedBy === 'RTBGG' && experimental.removal.authorizedOn === '2026-09-12' && experimental.removal.method === 'full-system-reprovision' && experimental.removal.inPlaceUninstallerAvailable === false && experimental.removal.destroysAllServerData === true, 'explicit destructive experimental removal authorization required');
  validateCommunitySupport(policy.support);
  requireValue(Array.isArray(policy.installationMatrix) && policy.installationMatrix.length >= 1 && policy.installationMatrix.length <= 3, 'policy: nonempty installation matrix required');
  policy.installationMatrix.forEach((cell) => installationCell(cell, 'policy cell'));
  requireValue(new Set(policy.installationMatrix.map(cellKey)).size === policy.installationMatrix.length, 'policy: duplicate installation cell');
  return policy;
}

function validateCommunitySupport(support) {
  object(support, ['model', 'maintainer', 'channels', 'guaranteedResponse', 'guaranteedFixes', 'supportEnds'], 'community support terms');
  requireValue(support.model === 'community-only' && support.maintainer === 'RTBGG' && support.guaranteedResponse === false && support.guaranteedFixes === false && support.supportEnds === null, 'community-only support without a guaranteed SLA, fixes or end date is required');
  exactSet(support.channels, ['github-issues', 'github-private-security-reporting'], 'community support channels');
}

function reportReference(report, label) {
  object(report, ['path', 'sha256'], label);
  requireValue(typeof report.path === 'string' && reportPattern.test(report.path) && report.path.split('/').every((part) => part !== '.' && part !== '..' && part !== ''), `${label}: invalid repository evidence path`);
  digest(report.sha256, label);
}

export function validateCandidate(candidate, expected) {
  object(candidate, ['version', 'commit', 'archive', 'archiveSHA256', 'build'], 'candidate');
  requireValue(typeof candidate.version === 'string' && versionPattern.test(candidate.version) && typeof candidate.commit === 'string' && commitPattern.test(candidate.commit), 'invalid candidate identity');
  digest(candidate.archiveSHA256, 'candidate archive');
  requireValue(candidate.archive === `stackfort-${candidate.version}-linux-amd64.tar.gz`, 'unexpected candidate archive name');
  requireValue(candidate.version === expected.version && candidate.commit === expected.commit && candidate.archiveSHA256 === expected.archiveSHA256, 'candidate version/commit/archive differs from the built tag');
  object(candidate.build, ['runId', 'attempt', 'artifactId', 'artifactSHA256'], 'candidate build');
  for (const value of [candidate.build.runId, candidate.build.attempt, candidate.build.artifactId]) requireValue(Number.isSafeInteger(value) && value > 0, 'invalid retained candidate build identity');
  digest(candidate.build.artifactSHA256, 'retained candidate ZIP');
}

function validateExperimentalRemoval(removal, candidate, completedAt, reports) {
  object(removal, ['method', 'result', 'candidate', 'targetBefore', 'targetAfter', 'installerMediaSHA256', 'checks', 'completedAt', 'report'], 'experimental removal evidence');
  requireValue(removal.method === 'full-system-reprovision' && removal.result === 'pass', 'tested full-system reprovision removal required; passive carrier removal or snapshot rollback is not accepted');
  validateCandidate(removal.candidate, candidate);
  requireValue(['runId', 'attempt', 'artifactId', 'artifactSHA256'].every((key) => removal.candidate.build[key] === candidate.build[key]), 'removal evidence belongs to a different retained candidate build');
  text(removal.targetBefore, 'installed removal-test target', 256);
  text(removal.targetAfter, 'reprovisioned removal-test target', 256);
  requireValue(removal.targetBefore === removal.targetAfter, 'removal must reprovision the same target that ran the exact active candidate');
  digest(removal.installerMediaSHA256, 'authenticated distribution installation media');
  exactSet(removal.checks, reprovisionChecks, 'experimental removal checks');
  timestamp(removal.completedAt, completedAt, 'full-system reprovision completion');
  reportReference(removal.report, 'full-system reprovision report');
  reports.push(removal.report);
}

export function evidenceInventory(policy, evidence, expected, now = Date.now()) {
  validatePolicy(policy);
  object(evidence, ['schemaVersion', 'kind', 'policy', 'releaseClass', 'candidate', 'approvedBy', 'approvedAt', 'installationResults', 'workflowRuns', 'independentReview', 'supportPolicy', 'publicationDecision'], 'evidence');
  requireValue(evidence.schemaVersion === 1 && evidence.kind === 'release-readiness' && evidence.policy === policyName, 'release-readiness evidence v1 required; rehearsal evidence is not accepted');
  validateCandidate(evidence.candidate, expected);
  requireValue(['reviewed-release', 'experimental-beta'].includes(evidence.releaseClass), 'explicit release class required');
  const experimental = evidence.releaseClass === 'experimental-beta';
  if (experimental) requireValue(/-beta\.[1-9][0-9]*$/.test(expected.version), 'experimental publication requires a beta version');
  identity(evidence.approvedBy, 'readiness approver');
  const approvedAt = timestamp(evidence.approvedAt, now, 'readiness approval');

  requireValue(Array.isArray(evidence.installationResults) && evidence.installationResults.length === policy.installationMatrix.length, 'incomplete clean installation matrix');
  const reports = [];
  const cells = [];
  for (const result of evidence.installationResults) {
    object(result, ['cell', 'result', 'sourceCommit', 'archiveSHA256', 'checks', 'completedAt', 'report', ...(experimental ? ['removal'] : [])], 'installation result');
    installationCell(result.cell, 'installation result cell');
    requireValue(result.result === 'pass' && result.sourceCommit === expected.commit && result.archiveSHA256 === expected.archiveSHA256, 'failed or stale installation result');
    exactSet(result.checks, experimental ? experimentalChecks : requiredChecks, 'installation checks');
    const completedAt = timestamp(result.completedAt, approvedAt, 'installation completion');
    if (experimental) validateExperimentalRemoval(result.removal, evidence.candidate, completedAt, reports);
    reportReference(result.report, 'installation report');
    cells.push(cellKey(result.cell));
    reports.push(result.report);
  }
  exactSet(cells, policy.installationMatrix.map(cellKey), 'installation matrix');

  requireValue(Array.isArray(evidence.workflowRuns) && evidence.workflowRuns.length === requiredWorkflows.length, 'CI and security workflow evidence required');
  for (const run of evidence.workflowRuns) {
    object(run, ['path', 'runId', 'attempt'], 'workflow reference');
    requireValue(Number.isSafeInteger(run.runId) && run.runId > 0 && Number.isSafeInteger(run.attempt) && run.attempt > 0, 'invalid workflow run identity');
  }
  exactSet(evidence.workflowRuns.map((run) => run.path), requiredWorkflows.map((workflow) => workflow.path), 'workflow paths');
  requireValue(new Set(evidence.workflowRuns.map((run) => run.runId)).size === evidence.workflowRuns.length, 'duplicate workflow run ID');

  const review = evidence.independentReview;
  if (experimental) {
    object(review, ['decision', 'disclosure'], 'experimental independent-review disclosure');
    requireValue(review.decision === 'not-performed' && review.disclosure === experimentalDisclosure, 'experimental release must explicitly disclose absence of independent review');
  } else {
    object(review, ['decision', 'reviewer', 'independentOfImplementation', 'completedAt', 'scopes', 'report'], 'independent review');
    requireValue(review.decision === 'approved' && review.independentOfImplementation === true, 'approved independent security review required');
    identity(review.reviewer, 'independent reviewer');
    requireValue(review.reviewer.toLowerCase() !== evidence.approvedBy.toLowerCase(), 'independent reviewer must differ from readiness approver');
    timestamp(review.completedAt, approvedAt, 'independent review completion');
    exactSet(review.scopes, reviewScopes, 'independent review scope');
    reportReference(review.report, 'independent review report');
    reports.push(review.report);
  }

  const support = evidence.supportPolicy;
  object(support, ['decision', 'approvedBy', 'versions', 'terms', 'deploymentLimits', 'securityPolicySHA256', 'report', ...(experimental ? ['removalMethod'] : [])], 'support policy');
  requireValue(support.decision === 'approved', 'explicit supported-version policy approval required');
  identity(support.approvedBy, 'support policy approver');
  requireValue(support.approvedBy.toLowerCase() === policy.support.maintainer.toLowerCase(), 'community support decision requires the recorded maintainer');
  exactSet(support.versions, [expected.version], 'candidate supported versions');
  validateCommunitySupport(support.terms);
  text(support.deploymentLimits, 'deployment limits');
  if (experimental) requireValue(support.removalMethod === 'full-system-reprovision', 'experimental support decision must acknowledge full-system removal');
  digest(support.securityPolicySHA256, 'candidate SECURITY.md');
  reportReference(support.report, 'support policy decision');
  reports.push(support.report);

  const publication = evidence.publicationDecision;
  object(publication, ['decision', 'approvedBy', 'releaseClass', 'freshDisposableOnly', 'productionUseAllowed', 'importantDataAllowed', 'report', ...(experimental ? ['removalMethod'] : [])], 'publication decision');
  requireValue(publication.decision === 'approved', 'explicit publication approval required');
  identity(publication.approvedBy, 'publication approver');
  requireValue(publication.releaseClass === evidence.releaseClass && publication.freshDisposableOnly === true && publication.productionUseAllowed === false && publication.importantDataAllowed === false, 'publication requires explicit fresh-disposable non-production scope');
  requireValue(publication.approvedBy === 'RTBGG', 'candidate publication decision requires RTBGG');
  if (experimental) requireValue(publication.removalMethod === 'full-system-reprovision', 'candidate approval must explicitly acknowledge destructive full-system removal');
  reportReference(publication.report, 'publication approval report');
  reports.push(publication.report);

  const unique = new Map();
  for (const report of reports) {
    requireValue(!unique.has(report.path) || unique.get(report.path) === report.sha256, 'conflicting evidence file digests');
    unique.set(report.path, report.sha256);
  }
  return { reports: [...unique].map(([reportPath, hash]) => ({ path: reportPath, sha256: hash })), workflowRuns: evidence.workflowRuns };
}

export function validateWorkflow(run, jobs, reference, commit) {
  const policy = requiredWorkflows.find((workflow) => workflow.path === reference.path);
  requireValue(policy && run !== null && typeof run === 'object' && run.id === reference.runId && run.run_attempt === reference.attempt && run.head_sha === commit && run.path === reference.path && run.repository?.full_name === repository && run.head_repository?.full_name === repository && ['push', 'workflow_dispatch'].includes(run.event) && run.status === 'completed' && run.conclusion === 'success', 'workflow is not a successful current attempt for the exact candidate/repository');
  requireValue(jobs !== null && typeof jobs === 'object' && Array.isArray(jobs.jobs) && Number.isSafeInteger(jobs.total_count) && jobs.total_count === jobs.jobs.length && jobs.jobs.length >= policy.jobs.length, 'workflow job inventory is incomplete');
  const names = [];
  for (const job of jobs.jobs) {
    requireValue(job.run_id === reference.runId && job.run_attempt === reference.attempt && job.head_sha === commit && job.status === 'completed', 'stale or incomplete workflow job');
    requireValue(policy.jobs.includes(job.name) || policy.optional.includes(job.name), 'unexpected workflow job; review readiness policy');
    requireValue(job.conclusion === 'success' || (policy.optional.includes(job.name) && job.conclusion === 'skipped'), 'required workflow job failed or was skipped');
    names.push(job.name);
  }
  requireValue(new Set(names).size === names.length && policy.jobs.every((name) => names.includes(name)), 'missing or duplicate required workflow job');
}

export function verifyReadiness({ policy, evidence, expected, evidenceCommit, files, workflows, securityPolicy, now = Date.now() }) {
  const inventory = evidenceInventory(policy, evidence, expected, now);
  requireValue(typeof evidenceCommit === 'string' && commitPattern.test(evidenceCommit), 'immutable evidence commit required');
  for (const report of inventory.reports) {
    requireValue(files instanceof Map && files.has(report.path) && hash(files.get(report.path)) === report.sha256, 'missing or changed reviewed evidence file');
  }
  requireValue(Buffer.isBuffer(securityPolicy) && hash(securityPolicy) === evidence.supportPolicy.securityPolicySHA256, 'support policy is not bound to candidate SECURITY.md');
  requireValue(workflows instanceof Map && workflows.size === requiredWorkflows.length, 'fresh workflow API results required');
  for (const reference of inventory.workflowRuns) {
    const actual = workflows.get(reference.runId);
    requireValue(actual, 'missing workflow API response');
    validateWorkflow(actual.run, actual.jobs, reference, expected.commit);
  }
  return { schemaVersion: 1, kind: 'verified-release-readiness', policy: policyName, releaseClass: evidence.releaseClass, candidate: evidence.candidate, evidenceCommit, evidenceSHA256: hash(Buffer.from(JSON.stringify(evidence, null, 2) + '\n')), checkedAt: new Date(now).toISOString(), installationCells: policy.installationMatrix, workflowRuns: inventory.workflowRuns, recordedIndependentReview: evidence.releaseClass === 'reviewed-release', independentReviewDisclosure: evidence.releaseClass === 'experimental-beta' ? experimentalDisclosure : 'Independent review approval recorded; see reviewed report.', removalMethod: evidence.releaseClass === 'experimental-beta' ? 'full-system-reprovision' : 'active-uninstall', removalDisclosure: evidence.releaseClass === 'experimental-beta' ? experimentalRemovalDisclosure : 'Active-installation uninstall qualification recorded; see the exact-candidate report.', recordedSupportDecision: true, recordedPublicationApproval: true, freshDisposableOnly: true, productionUseAllowed: false, importantDataAllowed: false };
}

export function hash(data) {
  return createHash('sha256').update(data).digest('hex');
}

export function parseDocument(source, label, canonical = false) {
  requireValue(Buffer.byteLength(source) <= maximumJSON, `${label}: JSON exceeds size limit`);
  const value = JSON.parse(source);
  // A canonical contract rejects duplicate keys as well as ambiguous encodings.
  if (canonical) requireValue(JSON.stringify(value, null, 2) + '\n' === source, `${label}: canonical two-space JSON with final newline required`);
  return value;
}

function readRegular(filename, maximum = maximumJSON) {
  const stat = lstatSync(filename);
  requireValue(stat.isFile() && !stat.isSymbolicLink() && stat.size > 0 && stat.size <= maximum, 'invalid evidence input file');
  return readFileSync(filename);
}

function parseArgs(args) {
  const options = new Map();
  const allowed = ['mode', 'policy', 'evidence', 'version', 'commit', 'archive-sha256', 'evidence-commit', 'input-directory', 'security-policy'];
  for (let index = 0; index < args.length; index += 2) {
    const name = args[index]?.slice(2);
    requireValue(args[index]?.startsWith('--') && allowed.includes(name) && args[index + 1] && !options.has(name), 'invalid or duplicate readiness argument');
    options.set(name, args[index + 1]);
  }
  const mode = options.get('mode');
  const required = mode === 'policy' ? ['mode', 'policy'] : mode === 'inventory' ? ['mode', 'policy', 'evidence', 'version', 'commit', 'archive-sha256'] : mode === 'verify' ? allowed : [];
  requireValue(required.length > 0 && options.size === required.length && required.every((name) => options.has(name)), 'missing or unexpected readiness arguments');
  return options;
}

export function main(args) {
  const options = parseArgs(args);
  const policy = parseDocument(readRegular(options.get('policy')).toString('utf8'), 'policy', true);
  validatePolicy(policy);
  if (options.get('mode') === 'policy') return { schemaVersion: 1, policy: policyName, installationCells: policy.installationMatrix, experimentalBeta: policy.experimentalBeta, support: policy.support, publicationEnabled: false };
  const evidence = parseDocument(readRegular(options.get('evidence')).toString('utf8'), 'evidence', true);
  const expected = { version: options.get('version'), commit: options.get('commit'), archiveSHA256: options.get('archive-sha256') };
  const inventory = evidenceInventory(policy, evidence, expected);
  if (options.get('mode') === 'inventory') return inventory;
  const directory = options.get('input-directory');
  const files = new Map(inventory.reports.map((report, index) => [report.path, readRegular(path.join(directory, `report-${index}`), 4 << 20)]));
  const workflows = new Map(inventory.workflowRuns.map((reference) => [reference.runId, {
    run: parseDocument(readRegular(path.join(directory, `run-${reference.runId}.json`)).toString('utf8'), 'workflow'),
    jobs: parseDocument(readRegular(path.join(directory, `jobs-${reference.runId}.json`)).toString('utf8'), 'jobs'),
  }]));
  return verifyReadiness({ policy, evidence, expected, evidenceCommit: options.get('evidence-commit'), files, workflows, securityPolicy: readRegular(options.get('security-policy')) });
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === pathToFileURL(fileURLToPath(import.meta.url)).href) {
  try {
    process.stdout.write(JSON.stringify(main(process.argv.slice(2)), null, 2) + '\n');
  } catch (error) {
    process.stderr.write(`Release readiness blocked: ${error.message}\n`);
    process.exitCode = 1;
  }
}
