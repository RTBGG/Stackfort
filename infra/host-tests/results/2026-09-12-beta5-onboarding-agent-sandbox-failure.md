# Exact beta.5 onboarding: installed agent sandbox failure — 2026-09-12

Result: **failed qualification; do not publish this candidate**. The genuine
tag-bound installation and original setup/login completed, but provisioning the
first hosting account failed in the installed privileged service. No readiness
approval or public release was created. The existing beta.5 tag must not move.

## Retained candidate

- Version `0.1.0-beta.5`, source `04faf03c59c83094796d3184d1cddc97f0649281`.
- Original successful build `34685947218`, attempt 1, artifact `10295762117`.
- Artifact ZIP SHA-256 `4b558b369d35b07c02c286031e148533abc38454dd1149730c1c588bf328ef8e`.
- Release archive SHA-256 `97973a9c7258b33fd49b51a9893bc71c432e776023c93483496a51505bae98c0`.
- Archived/standalone installer SHA-256 `58d852095bcaa4cb62b27abbcf1b78d97f927f45515081f6f23acf1deb2cf4d2`.
- Exact-tag workflow `34686606753`, attempt 1, artifact `10296155516`.
- Tag artifact ZIP SHA-256 `505326f3dbeb445c32af5bba30d71b393b010dda4d1f322f702992e9d63898e6`.
- Tag bundle SHA-256 `af45073d1f228afab810548ef31801af572f8417def5c113dd1ef17e36272359`.
- Bootstrap SHA-256 `321d61f479b7660682b478962b70060383470c41c3d00256b3ffe4cb28d98bec`.
- CI `34685907729` and Security `34685908135`, attempt 1, both successful.

The tag job retained all original payload bytes and generated genuine provenance
before its publication gate correctly stopped on missing readiness evidence.
Green CI/security jobs did not detect this installed-service incompatibility and
are not an independent security review.

## Actual installation and preserved failure

Disposable Hyper-V VM ID `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, DMI
`6365bd88-5141-4f15-b3f8-2ba9996baad2`, Debian 13 amd64, 2 vCPU, 8 GiB RAM,
50 GiB plain GPT/ext4/GRUB. The restored laboratory baseline included build and
Podman packages; it was not a pristine vendor-image package inventory.

The controlling-terminal harness began at `2026-09-12T09:47:52.7223019Z`. It used
the unmodified archived installer, genuine `tag-release` provenance and an
explicit root-owned local asset-transport fixture. It did not bypass provenance,
reboot consent, setup redemption or authentication. This is not evidence of
public GitHub release-asset HTTP transport.

Observed before failure:

- Prerequisites, terminal review, saving the original setup code and explicit
  disposable-server/reboot consent completed.
- One authorized conversion boot enabled ext4 project quotas automatically.
  Storage reached `ready`, `armAttempts=1`; package installation completed and
  admission became `admitted`.
- Original setup redemption returned 201, replay 409, login/session 200.
- Corrected native PHP detection succeeded; the API created the package and
  allocated hosting account `01a09506-7fe1-7926-afa4-446da3ce4ff9`.
- Account operation `01a09506-7fe7-739a-9377-d148fb8a3f9d` failed at 10% after
  three identity-stage attempts, last completed `2026-09-12T09:50:41.654999758Z`.
  Its stored code was `hosting_account.agent_unavailable`; the real agent's
  `hosting.identity.reconcile` returned HTTP 500.
- The allocated UID/GID was 200000, but neither its Linux group/user nor account
  directory existed. No domain/file/database/WAF smoke had run yet.

Native operation: `0e5ae073-b069-42fc-8a18-ed213b0e13bf`.
Previous boot: `2632937d-df36-4200-9f86-556eb19e18df`.
Conversion boot: `3368ec4c-05f1-47e0-87f2-a828a4564815`.
Root UUID `a88eaa57-e875-4855-a3cb-c231758653f8`, partition UUID
`54964bdf-2add-41b7-b41e-6d483962d021`.

The untouched failed installation is preserved in checkpoint
`native-beta5-04faf03-installed-account-failure`,
ID `5a0d9d1e-e710-4144-b270-3a88f8c9bf51`, created 2026-09-12 09:51:59 UTC.
Ignored local evidence under `infra/host-tests/work/candidates/34686606753-attempt1/`:

- `failed-onboard.json`: SHA-256 `0dcb3786b1b1a26f59523e1c855dd5feaea569b53961dc82ca98ead1b36f34b7`.
- `account-failure-readonly.json`: SHA-256 `2675c40df65c382c0b1f9e536113606cc3eb12ea59e6e1ee640a75e1d6173f00`.
- `failed-native-status.json`: SHA-256 `82c17be0c041a387f57ce50878988113eb9d6812c4a2c271c481b32eef191729`.
- `partial-host-security.json`: SHA-256 `ced14f51f8602ae0822b5d1271c2d7bf97da423ee9086bb43ee52c1855d5313c`.

No passwords, setup tokens, session cookies, private keys or raw terminal
transcript were retained. The targeted read-only SQLite diagnostic queried only
the generated fixture's operation and Linux identity metadata, never credential
tables or database writes.

## Root cause and subsequent diagnostic probes

The installed agent had `ProtectSystem=full`, no writable path exceptions, and
an actual read-only `/etc` mount. Its closed `groupadd`/`useradd` profiles inherit
that namespace, conflicting with their required Linux account-database updates.
[Debian's groupadd documentation](https://manpages.debian.org/trixie/passwd/groupadd.8.en.html)
identifies the affected account files; the
[upstream systemd execution contract](https://github.com/systemd/systemd/blob/v257/man/systemd.exec.xml)
documents the filesystem and inherited process restrictions.

After preserving the checkpoint, main performed explicitly diagnostic changes
to the failed host's agent unit, never its installed binary. A root-owned named
qualification drop-in was added/replaced only after exact metadata/hash guards.
The first `/etc`-only exception (`ProtectSystem=full`, `ReadWritePaths=/etc`)
still left `/etc` read-only on observed systemd `257.13-1~deb13u1`; both the actual
service and a minimal read-only transient mount probe confirmed this. The base
RPC still returned 500. No successful correction is claimed for that variant.

A coherent privileged-broker variant then used `ProtectSystem=yes`, preserving
read-only `/usr` and `/boot`, allowed `/etc` updates, retained visible `/run/user`
while hiding `/home` and `/root`, and allowed the device access, setuid mapping
helpers and Netlink needed by quotas/rootless OCI. The API service was unchanged.
`ProtectControlGroups=yes` remained enabled. These are explicit host-provisioning
permissions, not confinement for untrusted application code.

With that diagnostic unit, **the same installed beta.5 agent binary** returned
200 for base identity, filesystem/project quota, resource limits and runtime
preparation through its existing Unix socket as the authorized control UID.
The probe used only the original generated failed fixture and did not rewrite
its failed operation or mark the database account host-ready. Results are stored
as `probe-broker-{base,filesystem,resources,runtime}.json` in the same ignored
directory. These demonstrate the correction direction, **not exact-candidate
qualification**. Container build/scanning/deployment needs its own real
installed-service test; merely invoking production code outside this service
would miss the inherited sandbox defect.

## Collector limitation found during diagnosis

The version-1 read-only collector observed matching installed artifact hashes
and service properties, but incorrectly required UUIDv7 for the native admission
table comment. Native installation generates UUIDv4 and validates canonical
nonzero UUIDs. Its one reported firewall failure was a **collector defect**, not
an unexpected live firewall rule: the actual table had the correctly bound
comment and an empty admitted input chain. Version 2 preserves the old evidence,
fixes that parser with regression tests and additionally observes actual broker
mount flags instead of trusting declared systemd properties alone. Neither
collector replaces the external reachability or lifecycle qualification tests.

The completed rerun, normal reboot persistence, full installed API product smoke,
real rootless build/scan/deploy, process-loss recovery and same-target full OS
reprovision removal have **not passed for beta.5**. A new source/version/build and
complete exact-candidate qualification are required; no passing gate evidence
may be derived from these diagnostic observations.
