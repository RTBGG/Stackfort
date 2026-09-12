// SPDX-License-Identifier: AGPL-3.0-or-later
import { readFileSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';

function stats(values) {
  if (values.length !== 3 || values.some(value => !Number.isFinite(value))) throw new Error('Expected three finite measurements');
  const sorted = [...values].sort((a, b) => a - b);
  return { median: sorted[1], min: sorted[0], max: sorted[2], runs: values };
}

export function summarizeNativeQuota(read) {
  const workloads = ['randread', 'randwrite', 'syncwrite'].map(workload => {
    const direction = workload === 'randread' ? 'read' : 'write';
    const phases = {};
    for (const phase of ['before', 'after']) {
      const samples = [1, 2, 3].map(repeat => {
        const prefix = `benchmark-${phase}/${workload}-${repeat}`;
        const raw = read(`${prefix}.json`);
        if (raw.jobs?.length !== 1 || raw.jobs[0].error !== 0) throw new Error(`Failed or missing fio job: ${prefix}`);
        const job = raw.jobs[0];
        const latency = workload === 'syncwrite' ? job.sync.lat_ns : job[direction].clat_ns;
        const values = {
          iops: job[direction].iops,
          mibPerSecond: job[direction].bw_bytes / 1048576,
          p99Milliseconds: latency.percentile['99.000000'] / 1e6,
          guestBusyPercent: read(`${prefix}-cpu.json`).guestBusyPercent,
        };
        if (Object.values(values).some(value => !Number.isFinite(value) || value < 0) || values.iops === 0 || values.guestBusyPercent > 100) throw new Error(`Invalid measurement: ${prefix}`);
        return values;
      });
      phases[phase] = Object.fromEntries(Object.keys(samples[0]).map(key => [key, stats(samples.map(row => row[key]))]));
    }
    return {
      workload,
      latencyMetric: workload === 'syncwrite' ? 'fio sync.lat_ns p99 (fsync only)' : 'fio clat_ns p99',
      ...phases,
      medianIOPSChangePercent: 100 * (phases.after.iops.median / phases.before.iops.median - 1),
    };
  });
  return {
    schemaVersion: 1,
    kind: 'native-ext4-project-quota-before-after-lab',
    baseline: 'native root ext4 before quota/project conversion; project 0',
    candidate: 'same native ext4 after offline quota/project conversion; inherited project with 1 GiB hard limit',
    comparisonBoundary: 'Sequential before/after phases separated by reboot and validation; not randomized or simultaneous paired runs. No statistical significance or application-performance claim.',
    workloads,
  };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  if (process.argv.length !== 3) throw new Error('Expected one extracted native quota evidence directory');
  const root = resolve(process.argv[2]);
  process.stdout.write(JSON.stringify(summarizeNativeQuota(name => JSON.parse(readFileSync(join(root, name), 'utf8'))), null, 2) + '\n');
}
