# Read-only installer preflight

I-001 turns Stackfort's typed Linux host-capability detector into an installer
gate and an explicit installation plan. It is safe to run before granting an
installer permission to mutate a host: the command exposes no apply operation.

## Usage and result contract

Build from a reviewed checkout during development:

```sh
go build -o stackfort-installer ./cmd/stackfort-installer
./stackfort-installer preflight
./stackfort-installer preflight --format=json
```

The release build also emits a standalone versioned Linux binary for each
architecture and includes the binary in the complete release archive. I-002
adds the separately inspectable, checksum-verified download and journaled apply
workflow; unreleased binaries must stay on disposable hosts.

The command has three stable exit classes:

| Exit | Meaning |
| --- | --- |
| `0` | Every required check passed; the fresh host is ready for the future apply stage. |
| `1` | Invocation, report generation, or output failed. |
| `2` | Inspection completed, but at least one actionable host check blocked installation. |

Text is the human default. `--format=json` emits schema version `1`, the raw
typed capability/resource observations, the normalized checks, and the same
plan. `readOnly` is always true. A failure includes a stable `reasonCode` and a
concrete remediation; automation must use IDs/statuses rather than English
text.

## Required fresh-host shape

Preflight requires:

- Debian 13, Ubuntu 26.04 LTS, or Rocky Linux 10 on `amd64`;
- systemd as PID 1 and unified cgroup v2 with CPU, memory, I/O, and pids
  controllers;
- at least 2 logical CPUs and a nominal 4 GiB physical-memory allocation
  (`MemTotal` may be as low as 3.5 GiB after firmware/kernel reservations);
- `/srv/hosting` as a real directory on ext4 with `prjquota`, or XFS with
  `prjquota`/`pquota`, with at least 5 GiB free;
- AppArmor enabled on Debian/Ubuntu or SELinux enforcing on Rocky;
- TCP ports 80, 443, and 8443 available; and
- no active NGINX, PHP-FPM, MariaDB, Vinyl, or Podman service and no previous
  Stackfort systemd units.

The firewall service is inspected but is not treated as foreign: later install
stages must preserve unrelated nftables/firewalld state and own only
Stackfort-specific TCP 80/443/8443 rules. Missing packages are expected on a fresh
host and are listed as planned package-manager actions rather than blockers.

Project quotas are an explicit host prerequisite, not a silent root-filesystem
rewrite. This keeps preflight read-only and prevents the installer from adding
mount flags or repartitioning an administrator's unknown storage. The
disposable Hyper-V harness already creates the dedicated quota filesystem used
by host qualification.

## Explicit plan

The plan is generated even when checks fail. It enumerates:

- distribution-specific prerequisite packages, including Podman, netavark,
  aardvark-dns, passt/pasta, slirp4netns, fuse-overlayfs, and subordinate-ID helpers;
- release binaries, web assets, state/config/runtime paths, systemd units and
  slices, and the Stackfort-owned NGINX paths with intended owner and mode;
- the locked `stackfort` system service account;
- API, agent, NGINX, and firewall service actions;
- system and global-user masking of Podman's API socket/service without
  starting a container workload;
- public TCP 80/443, dedicated HTTPS management port 8443, loopback-only API port 8080, and the authenticated local
  agent socket; and
- AppArmor/nftables changes or SELinux/firewalld changes without disabling
  enforcement or replacing unrelated firewall policy.

The plan is declarative output, not a shell transcript. I-002 turns its entries
into fixed idempotent journal stages and verifies each stage before recording
completion; see [Fresh-host installation](installer-installation.md).

## Read-only boundary

Host facts come from bounded reads of fixed `/etc`, `/proc`, and `/sys` paths,
one `statfs` call for `/srv/hosting`, and the existing allowlisted
`dpkg-query`/`rpm` and `systemctl show` probes. No shell is invoked. The command
does not call package managers in mutation mode, create paths or identities,
write configuration, change mounts/security policy/firewall state, or start,
stop, enable, or reload services.

Pure tests cover every acceptance blocker and plan section. The existing
supported-distribution fixtures continue to exercise the bounded host parsers.
The compiled preflight also returns ready on clean Debian 13, Ubuntu 26.04, and
Rocky Linux 10 Hyper-V guests before their package-preparation and destructive
host test phases. The I-003 qualification retained this gate before the
complete fresh-host installer and destructive Phase 1 suite on all three
guests; upgrade matrices remain a later release-phase gate.
