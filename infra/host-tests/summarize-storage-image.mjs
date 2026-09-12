// SPDX-License-Identifier: AGPL-3.0-or-later
// Read one extracted, complete fio benchmark result directory; emit derived
// evidence as JSON. Never substitute zeros for missing/error measurements.
import { readFileSync } from 'node:fs';
import { resolve, join } from 'node:path';

const xfsComparison = process.argv.length === 4 && process.argv[3] === '--xfs';
if (process.argv.length !== 3 && !xfsComparison) throw new Error('Expected one benchmark result directory and optional --xfs');
const root = resolve(process.argv[2]);
const labels = xfsComparison ? ['native', 'ext4', 'xfs'] : ['native', 'image'];
const read = name => JSON.parse(readFileSync(join(root, name), 'utf8'));
const stats = values => {
  if (values.length !== 3 || values.some(value => !Number.isFinite(value))) {
    throw new Error('Expected three finite measurements');
  }
  const sorted = [...values].sort((a, b) => a - b);
  return { median: sorted[1], min: sorted[0], max: sorted[2], runs: values };
};
const workloads = [];
for (const workload of ['seqread', 'seqwrite', 'randread', 'randwrite', 'syncwrite']) {
  const direction = workload.endsWith('read') ? 'read' : 'write';
  const backends = {};
  for (const backend of labels) {
    const rows = [1, 2, 3].map(repeat => {
      const name = `${backend}-${workload}-${repeat}`;
      const raw = read(`${name}.json`);
      if (raw.jobs.length !== 1 || raw.jobs[0].error !== 0) throw new Error(`Invalid fio job: ${name}`);
      const job = raw.jobs[0];
      const latency = workload === 'syncwrite' ? job.sync.lat_ns : job[direction].clat_ns;
      return {
        iops: job[direction].iops,
        mibPerSecond: job[direction].bw_bytes / (1024 * 1024),
        p99Milliseconds: latency.percentile['99.000000'] / 1e6,
        guestBusyPercent: read(`${name}-cpu.json`).guestBusyPercent,
      };
    });
    backends[backend] = Object.fromEntries(Object.keys(rows[0]).map(key => [key, stats(rows.map(row => row[key]))]));
  }
  const changes = (candidate, baseline) => ({
    medianIOPSChangePercent: 100 * (backends[candidate].iops.median / backends[baseline].iops.median - 1),
    pairedIOPSChangePercent: stats(backends[candidate].iops.runs.map((value, index) => 100 * (value / backends[baseline].iops.runs[index] - 1))),
  });
  const legacyChanges = xfsComparison ? null : changes('image', 'native');
  workloads.push({
    workload,
    latencyMetric: workload === 'syncwrite' ? 'fio sync.lat_ns p99 (fsync only)' : 'fio clat_ns p99',
    ...backends,
    ...(xfsComparison ? {
      ext4VsNative: changes('ext4', 'native'),
      xfsVsNative: changes('xfs', 'native'),
      xfsVsExt4: changes('xfs', 'ext4'),
    } : {
      imageMedianIOPSChangePercent: legacyChanges.medianIOPSChangePercent,
      pairedIOPSChangePercent: legacyChanges.pairedIOPSChangePercent,
    }),
  });
}
process.stdout.write(JSON.stringify({
  schemaVersion: 1,
  kind: xfsComparison ? 'single-disk-xfs-ext4-image-comparison' : 'single-disk-storage-image-experiment',
  baseline: 'native root ext4 WITHOUT project quotas',
  candidate: xfsComparison ? 'preallocated loop XFS and ext4 WITH project quotas and backing-file direct I/O' : 'preallocated loop ext4 WITH project quotas and backing-file direct I/O',
  performanceAcceptance: 'not determined by this measurement script',
  workloads,
}, null, 2) + '\n');
