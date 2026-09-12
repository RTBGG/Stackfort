// SPDX-License-Identifier: AGPL-3.0-or-later

// Read-only validation of retained build identity before an exact tag promotion.
import { readFileSync, lstatSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseDocument, validateCandidate } from './verify-release-readiness.mjs';

function requireValue(condition, message) {
  if (!condition) throw new Error(message);
}

export function validatePromotion(promotion, version, commit) {
  requireValue(promotion && Object.keys(promotion).sort().join(',') === 'candidate,kind,schemaVersion' && promotion.schemaVersion === 1 && promotion.kind === 'candidate-promotion', 'mechanical candidate promotion record v1 required');
  validateCandidate(promotion.candidate, { version, commit, archiveSHA256: promotion.candidate?.archiveSHA256 });
  return promotion.candidate;
}

export function validateRetainedBuild(promotion, version, commit, run, artifacts) {
  const candidate = validatePromotion(promotion, version, commit);
  const build = candidate.build;
  requireValue(run && run.id === build.runId && run.run_attempt === build.attempt && run.head_sha === commit && run.path === '.github/workflows/release.yml' && run.event === 'workflow_dispatch' && run.status === 'completed' && run.conclusion === 'success' && run.repository?.full_name === 'RTBGG/Stackfort' && run.head_repository?.full_name === 'RTBGG/Stackfort', 'retained candidate is not the successful exact source/workflow-dispatch attempt');
  requireValue(artifacts && Array.isArray(artifacts.artifacts) && Number.isSafeInteger(artifacts.total_count) && artifacts.total_count === artifacts.artifacts.length && artifacts.total_count > 0, 'incomplete retained artifact inventory');
  const matches = artifacts.artifacts.filter((artifact) => artifact.name === `stackfort-${version}`);
  requireValue(matches.length === 1, 'missing or duplicate retained candidate artifact');
  const artifact = matches[0];
  requireValue(artifact.id === build.artifactId && artifact.expired === false && Number.isSafeInteger(artifact.size_in_bytes) && artifact.size_in_bytes > 0 && artifact.size_in_bytes <= 2 ** 31 && artifact.digest === `sha256:${build.artifactSHA256}` && artifact.workflow_run?.id === build.runId && artifact.workflow_run?.head_sha === commit, 'retained artifact ID/digest/source mismatch or expired artifact');
  return { schemaVersion: 1, kind: 'verified-candidate-promotion', candidate, sourceWorkflow: run.path, sourceEvent: run.event, publicationAuthorized: false };
}

function read(filename, canonical = false) {
  const stat = lstatSync(filename);
  requireValue(stat.isFile() && stat.size > 0 && stat.size <= 1 << 20, 'invalid promotion input file');
  return parseDocument(readFileSync(filename, 'utf8'), 'promotion input', canonical);
}

export function main(args) {
  const options = new Map();
  for (let index = 0; index < args.length; index += 2) {
    const key = args[index]?.slice(2);
    requireValue(args[index]?.startsWith('--') && ['mode', 'promotion', 'version', 'commit', 'run', 'artifacts'].includes(key) && args[index + 1] && !options.has(key), 'invalid promotion arguments');
    options.set(key, args[index + 1]);
  }
  const mode = options.get('mode');
  const keys = mode === 'inventory' ? ['mode', 'promotion', 'version', 'commit'] : mode === 'verify' ? ['mode', 'promotion', 'version', 'commit', 'run', 'artifacts'] : [];
  requireValue(keys.length > 0 && options.size === keys.length && keys.every((key) => options.has(key)), 'missing promotion arguments');
  const promotion = read(options.get('promotion'), true);
  if (mode === 'inventory') return validatePromotion(promotion, options.get('version'), options.get('commit'));
  return validateRetainedBuild(promotion, options.get('version'), options.get('commit'), read(options.get('run')), read(options.get('artifacts')));
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    process.stdout.write(JSON.stringify(main(process.argv.slice(2)), null, 2) + '\n');
  } catch (error) {
    process.stderr.write(`Candidate promotion blocked: ${error.message}\n`);
    process.exitCode = 1;
  }
}
