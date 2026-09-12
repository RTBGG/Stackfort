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

## Actual installed-broker OCI diagnostic

After the unit-only correction, a separate scratch-image fixture used the real
installed agent socket, authenticated with the fixed control service's kernel
UID. Its Containerfile includes a real exec-form `RUN` as UID/GID 1000, not just
file copies; no scanner or build-resource bypass was introduced. Four pure
helper test groups passed first. An earlier helper version's output-bound test
had failed before any OCI mutation; that failed helper and its log were retained.

The real v2 probe created fresh account
`01a0951d-2302-74c7-b99b-31089921435c`, UID/GID 249940, and successfully reconciled
identity, filesystem/quota, resources and rootless runtime. Image preparation
operation `01a0951d-2501-7640-b916-214e21c82bd4` failed at
`2026-09-12T10:15:10.419321925Z`, with agent audit HTTP 503. The existing protocol
validator rejected this error because the server's generic `oci_image_unavailable`
response lacked its mandatory capability detail. The helper treated the outcome
as uncertain and deliberately retained all remaining fixture state.

Read-only path inspection found `/srv/hosting` and its actual bind source
`/srv/stackfort-native-hosting` were the same root-owned inode with mode **0700**.
The account parent itself was correctly 0711. A fixed rootless `podman info`
diagnostic in the actual broker mount namespace failed immediately with a
permission error traversing that native hosting parent. After preserving
checkpoint `native-beta5-broker-rpc-v2-failure` (ID
`c236b35f-ee9c-422a-a7a0-e1dd4afb75bb`), a guarded diagnostic changed only that
verified bind-source inode to root-owned **0711**: traverse, but no directory
listing or writes for other users. The same `podman info` command then succeeded
and reported `overlay`. This is not a successful image build or release test.

Source inspection also found that creating an OCI transaction with requested
mode 0711 under the actual agent's `UMask=0027` leaves mode 0710 without an
explicit final chmod. Both umask-sensitive directory contracts require product
fixes and real regression tests. The service umask must not be relaxed to hide
these faults. No finding here justifies removing tenant isolation or disabling
scanner checks.

Ignored v2 evidence under `infra/host-tests/work/candidate-broker-rpc-probe-v2/`:

- Probe binary SHA-256 `58cb0b0fa4609d060adce7f6b43bbffc6e96f9cbe393388f80c536838a8bfc25`.
- `actual-helper-tests.log`: SHA-256 `57e715b455283fc6b0ec3e3d1db6185b7a574a7e4997616d01f627bfd6e9c0af`.
- `actual-rpc-probe.log`: SHA-256 `25184d60ab5aa1ecfaa06bc9d8b07102ccd12c73b0670cb62dadf9c5a57fee37`.
- `mount-namespace-podman-info.log`: SHA-256 `f908c9496bc0c5638790cacdc8395f99889e3dd9716e4b4a32885f2704f59d69`.
- `mount-namespace-podman-info-hosting-fixed.log`: SHA-256 `9dccdd68ed4bb1b0182f01b354d7f3ddfa397e76b1be057c8cea2255e71ead6f`.

## Real Containerfile RUN and inherited cgroup namespace

The diagnostic host next received an explicitly non-release agent built from
`139e5a97150b72d2f17fb294e00e2e152d2621d3` (version
`dev-beta6-diagnostic`, binary SHA-256
`760ac45bdddfb6737c84d7f2b1c27bf6d94c600c8d6a93e32ee52a35a82903c6`).
The original beta.5 binary remains preserved. This mixed installation must not
be treated as an exact candidate, nor used for a completed installer recheck.

Five pure v3 helper groups passed. The actual installed-socket probe used new
account `01a0952a-aaf3-7a84-aec9-9adeac9ab0b3`, UID/GID 249941. Its base,
filesystem/quota, resource and runtime operations returned 200. Image operation
`01a0952a-aced-7df3-ad83-84204341a864` returned a correctly typed HTTP 503 with
`oci-image-build-failed`; the stricter protocol validator was not relaxed.

A fixed, synthetic-source build with the production limits reached its real
Containerfile `RUN`, but crun could not create the build cgroup. The same build
failed both inside and outside the agent mount namespace: Podman's persistent
pause process had inherited and retained a read-only `/sys/fs/cgroup` from the
broker. The account's systemd manager was delegated and its account subtree
existed. Restarting the broker alone does not replace that pause namespace.

Checkpoint `native-beta6-139e5a9-rpc-v3-failure`, ID
`317e22af-c82a-4d47-8daf-309aae7b086f`, preserves this failure. A second guarded
diagnostic drop-in then changed only the broker to `ProtectControlGroups=no`.
Its actual cgroup mount became writable. A fresh account and a complete actual
build/scan/deploy plus account-isolation test are still required; this mount
observation alone is not a successful OCI qualification. Stopping the agent also
stopped the dependent API; the API has not yet been restarted on this mixed host.

The v3 helper additionally exposed a cleanup defect: installed Podman does not
support `network rm --ignore`. After removing its own image it preserved the
remaining fixture when that command failed. The replacement helper must probe
`network exists`, distinguish absence from runtime errors, and remove only its
own network without `--force`; old failed fixtures are not reused or erased.

Ignored v3 evidence under `infra/host-tests/work/candidate-broker-rpc-probe-v3/`:

- Probe binary SHA-256 `10f1e94c463bdc8ec5e5392b22ac0ee3493a37b0d33265ab6bd1e992158e25ca`.
- `actual-helper-tests.log`: SHA-256 `e7aabde72b89225f8545c94f25cb9a6990a337bf7724ea00e5bc0698c143ce34`.
- `actual-rpc-probe.log`: SHA-256 `ed96daf306339cc67267be26d2c38c371284c5db5d210659a141d7f0a4a3af6f`.
- `mount-namespace-synthetic-build.log`: SHA-256 `d0a7caa7f947e072244d76fdd0b6687ea0eb7da07482ea30f3da2d84bcbdb97b`.
- `global-namespace-synthetic-build.log`: SHA-256 `f379cbc245e38a34d71c4a24884a95fc15a8c34a7b3596b3354f72413db78272`.

## Follow-up procfs A/B, still not release qualification

The final v4 helper (`47bab7ce75fb43b70f922993c93bd5a6bd2e44cf16797b8c3462a2a070a5eb77`)
passed six Linux helper test groups, including clean EOF versus a descendant-held
output pipe after exit 1. Its new real-socket account was
`01a0953a-3465-7c41-a54d-e32666b75848`, UID/GID 249942, application
`01a0953a-3465-7c4a-8852-6532c44705a6`. Identity, filesystem, resources and
runtime returned 200, but preparation `01a0953a-3659-78bf-846d-c30be49d8935`
still returned `oci-image-build-failed`. Cleanup removed its runtime, files and
identity, then failed the quota reset: the helper used the subtree bind mount
`/srv/hosting`, whereas the production quota profile verifies and selects `/`
for this native-root layout. A residual test project-quota record may remain;
this cleanup failure is not a successful removal test.

Checkpoint `native-beta6-v4-before-v3-runtime-reprobe`, ID
`d9d0bdbb-523c-48a0-b422-74f56d324c8d`, preserves that state. Main then deliberately
reused only the already identified v3 synthetic fixture, after terminating and
restarting its exact user manager to remove the old pause namespace. The original
v3 failure remains in earlier checkpoints; its live fixture is now diagnostic
reprobe state, not untouched evidence.

With writable broker cgroups, the same limited scratch-image build progressed
to a different crun error: mounting `proc` was not permitted. Main independently
tested the broker's two proc-masking settings, resetting the fixture's pause
namespace for each case. These are direct synthetic builds in the actual agent
mount namespace, not the final RPC build/scan/deploy qualification:

| ProtectKernelLogs | ProtectKernelTunables | Actual Containerfile RUN |
| --- | --- | --- |
| yes | yes | fail: proc mount EPERM |
| yes | no | fail: proc mount EPERM |
| no | yes | fail: proc mount EPERM |
| no | no | pass as UID/GID 1000, confirmed twice |

The rapid diagnostic restart loop reached the synthetic user manager's start
burst limit once. Main reset only `user@249941.service`'s failed state and
continued; no production restart limit was changed. The broker currently uses
the two diagnostic exceptions, remains the source-139 diagnostic binary, and
the API remains inactive. The manually built image is unscanned and must not be
published or counted as a deployable candidate. A new complete actual-socket
probe, then a fresh exact release installation, are still required.

The primary
[kernel visibility check](https://github.com/torvalds/linux/blob/v6.12/fs/namespace.c)
and [systemd proc masking implementation](https://github.com/systemd/systemd/blob/v257/src/core/namespace.c)
explain the observed conflict: locked child mounts hide nonempty parts of procfs,
so a new procfs mount in the rootless user namespace is rejected. Disabling these
two broker restrictions is a documented compatibility/security tradeoff, not a
claim that the root service retains those defense-in-depth protections. Other
service sandboxes and container/account limits were not relaxed by these probes.

Ignored v4 evidence under `infra/host-tests/work/candidate-broker-rpc-probe-v4/`:

- `actual-helper-tests.log`: SHA-256 `4ce587a60abe8a822902b4086554c120b4a813a610599dd4b454314fc69a4c1c`.
- `actual-rpc-probe.log`: SHA-256 `9a5addbd18a9cae6f3c331ce18abaca0bcc3753c344a1f7c00921f5de989f48e`.
- `v3-reset-runtime-synthetic-build.log`, `proc-ab-logs-on.log`, `proc-ab-tunables-on.log`: identical SHA-256 `f96fc3e17afe60869fe6b5010fdf1a1ffca6b17fab9b0b4f86893d631e730479`.
- `v3-unmasked-proc-synthetic-build.log`: SHA-256 `420eae96074eccdb8f2fdf5445b7a73b08c97f76b5d2d3118e28063621a6b6f7`.
- `proc-ab-both-off-restored.log`: SHA-256 `f3283051199b699aea248b84a51e44274d5bd753681c748b12393780ea63a8e7`.

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
