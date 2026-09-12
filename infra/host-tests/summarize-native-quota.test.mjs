// SPDX-License-Identifier: AGPL-3.0-or-later
import test from 'node:test';
import assert from 'node:assert/strict';
import { summarizeNativeQuota } from './summarize-native-quota.mjs';

function fixture(name) {
  const repeat = Number(name.match(/-(\d)(?:-cpu)?\.json$/)[1]);
  if (name.endsWith('-cpu.json')) return { guestBusyPercent: 10 * repeat };
  const iops = repeat * (name.startsWith('benchmark-before/') ? 100 : 90);
  const direction = { iops, bw_bytes: iops * 4096, clat_ns: { percentile: { '99.000000': 1e6 } } };
  return { jobs: [{ error: 0, read: direction, write: direction, sync: { lat_ns: { percentile: { '99.000000': 9e6 } } } }] };
}

test('native before/after retains samples and uses separate fsync latency', () => {
  const result = summarizeNativeQuota(fixture);
  assert.deepEqual(result.workloads[0].before.iops.runs, [100, 200, 300]);
  assert.deepEqual(result.workloads[0].after.iops.runs, [90, 180, 270]);
  assert.ok(Math.abs(result.workloads[0].medianIOPSChangePercent + 10) < 1e-10);
  assert.equal(result.workloads[0].after.p99Milliseconds.median, 1);
  assert.equal(result.workloads[2].after.p99Milliseconds.median, 9);
});

test('failed, missing, zero or non-finite measurements never become successful results', () => {
  for (const invalid of [{ jobs: [{ error: 28 }] }, {}, { jobs: [] }]) {
    assert.throws(() => summarizeNativeQuota(name => name.endsWith('-cpu.json') ? fixture(name) : invalid));
  }
  for (const iops of [0, null, NaN, Infinity, -1]) {
    assert.throws(() => summarizeNativeQuota(name => {
      const raw = fixture(name);
      if (raw.jobs) raw.jobs[0].read.iops = iops;
      return raw;
    }));
  }
  assert.throws(() => summarizeNativeQuota(name => name.endsWith('-cpu.json') ? { guestBusyPercent: 101 } : fixture(name)));
});
