// SPDX-License-Identifier: AGPL-3.0-or-later
import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

const script = fileURLToPath(new URL('./summarize-storage-image.mjs', import.meta.url));
function fixture(t, labels) {
  const root = mkdtempSync(join(tmpdir(), 'stackfort-storage-summary-'));
  t.after(() => rmSync(root, { recursive: true }));
  for (const backend of labels) {
    for (const workload of ['seqread', 'seqwrite', 'randread', 'randwrite', 'syncwrite']) {
      for (const repeat of [1, 2, 3]) {
        const iops = ({ native: 100, image: 50, ext4: 50, xfs: 75 }[backend]) * repeat;
        const direction = { iops, bw_bytes: iops * 4096, clat_ns: { percentile: { '99.000000': 1e6 } } };
        const raw = { jobs: [{ error: 0, read: direction, write: direction, sync: { lat_ns: { percentile: { '99.000000': 9e6 } } } }] };
        const name = `${backend}-${workload}-${repeat}`;
        writeFileSync(join(root, `${name}.json`), JSON.stringify(raw));
        writeFileSync(join(root, `${name}-cpu.json`), JSON.stringify({ guestBusyPercent: repeat * 10 }));
      }
    }
  }
  return root;
}
function run(root, ...args) {
  return spawnSync(process.execPath, [script, root, ...args], { encoding: 'utf8' });
}

test('legacy two-backend result remains compatible and uses separate fsync latency', t => {
  const result = run(fixture(t, ['native', 'image']));
  assert.equal(result.status, 0, result.stderr);
  const parsed = JSON.parse(result.stdout);
  assert.equal(parsed.workloads[0].imageMedianIOPSChangePercent, -50);
  assert.deepEqual(parsed.workloads[0].native.iops.runs, [100, 200, 300]);
  assert.equal(parsed.workloads[0].native.p99Milliseconds.median, 1);
  assert.equal(parsed.workloads[4].native.p99Milliseconds.median, 9);
});
test('three-backend comparison exposes both native and ext4 comparisons', t => {
  const result = run(fixture(t, ['native', 'ext4', 'xfs']), '--xfs');
  assert.equal(result.status, 0, result.stderr);
  const parsed = JSON.parse(result.stdout);
  assert.equal(parsed.workloads[0].ext4VsNative.medianIOPSChangePercent, -50);
  assert.equal(parsed.workloads[0].xfsVsNative.medianIOPSChangePercent, -25);
  assert.equal(parsed.workloads[0].xfsVsExt4.medianIOPSChangePercent, 50);
  assert.equal(parsed.workloads[0].xfsVsExt4.pairedIOPSChangePercent.median, 50);
});
test('missing measurement, non-finite metric and failed fio job cannot become success', t => {
  const root = fixture(t, ['native', 'ext4', 'xfs']);
  const path = join(root, 'xfs-seqread-1.json');
  writeFileSync(path, JSON.stringify({ jobs: [{ error: 28 }] }));
  assert.notEqual(run(root, '--xfs').status, 0);
  writeFileSync(path, JSON.stringify({ jobs: [{ error: 0, read: { iops: null, clat_ns: { percentile: {} } } }] }));
  assert.notEqual(run(root, '--xfs').status, 0);
  rmSync(path);
  assert.notEqual(run(root, '--xfs').status, 0);
});
