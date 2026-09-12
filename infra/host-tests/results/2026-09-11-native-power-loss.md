# Native power-loss containment — 2026-09-11

Result: **the internal Debian boot safety stop passed real VM hard-power-off
tests; complete interrupted/torn-write recovery remains unqualified**.
No public activation, release, commit or push was performed.

See the [design and operator boundaries](../../../docs/native-installer-power-loss.md).

## Fixture and artifact identity

Only `stackfort-native-quota-debian-13`,
VM ID `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, was operated.
Other VMs remained off. The existing Point-2 successful checkpoint was preserved.
Preparation started from the reviewed offline missing-prerequisites checkpoint,
installed the same two approved prerequisites and sealed a guarded boot intent.

The fixture remains Debian 13, kernel `6.12.107+deb13-cloud-amd64`, plain GPT/ext4,
2 vCPU, 8 GiB assigned RAM and approximately 50 GiB root. Secure Boot stayed on.
No filesystem undo log, automatic repair, altered e2fsprogs executable or
test/pause switch was introduced into the initramfs.

| Artifact | SHA-256 / identity |
| --- | --- |
| Real runtime/prerequisite installer | `204503d56dadb60550d70e6fd7d8561cacae822844958d1b55363a9e5bb79bc2` |
| Runtime intent | `f4b95a51aadfcda0401bcd1ae701e958b0f5e96878b09ef95d15ef2a17d16a41` |
| Prerequisite receipt | `ad1144ddd4c66b32cfb57d8ae675cc9501db46a078fcd78c103a7ea1d3b68f3b` |
| Release manifest | `ab31e7b04307c3643750bc17a1f75065bcaa77fe7a5e64844624380692b25654` |
| Completed package journal | `e728db7658b844bff09f8cb5e3ae60a3dc2adaf743e256e145d2a1b7e305ec68` |
| Initial boot qualification helper | `5c888d014777ed1c432f2fab68de520966a1049c245c19d4f413d6213fef9346` |
| Final helper, including queued-job wait fix | `cd36fdf8a583be7ea6b7907b58d2502cf6f6e9b470e43c937f7ef865888125ff` |
| Operation | `81b5c629-4a3e-4510-bc2a-a28a9bed2636` |

The authenticated beta.3 source archive is unchanged. The runtime hash stayed
identical throughout all cuts and successful boot tests; only the external
qualification helper's wait policy changed.

## Real power-off and recovery cases

The armed, normally powered-off fixture was saved as
`native-powerguard-armed` (`8f58a789-31d9-469c-8738-745bad410961`).
Each case explicitly restored that snapshot while the VM was off.
The driver observed real kernel/serial progress and requested Hyper-V
`Stop-VM -TurnOff`, not a guest reboot or SIGKILL.

| Power-off request followed this observed event | Cut evidence suffix | Verified recovery suffix |
| --- | --- | --- |
| `quota-start` | `Cut-quota-start-20260911T082610Z` | `Recovery-quota-start-20260911T082727Z` |
| `recovery-latch-flushed` | `Cut-recovery-latch-flushed-20260911T082823Z` | `Recovery-recovery-latch-flushed-20260911T082836Z` |
| `postcheck-flushed` | `Cut-postcheck-flushed-20260911T082910Z` | `Recovery-postcheck-flushed-20260911T082950Z` |
| `quota-start`, repeat with temporary CPU maximum 5 | `Cut-quota-start-20260911T083025Z` | `Recovery-quota-start-20260911T083046Z` |

Evidence paths are `infra/host-tests/work/native-powercut-<suffix>/serial.log`
and are intentionally ignored local artifacts. Each contains the trigger,
host power-off timestamps and/or read-only recovery console output.

All verified recovery runs:

- Selected the operation-bound recovery entry without operator menu selection.
- Reached the initramfs recovery stop with PID 1 still `init`.
- Reported an unmounted target; `/proc/mounts` contained only RAM/kernel
  filesystems, no mounted ext4 root.
- Retained the exact consumed operation in GRUB and the recovery kernel token.
- Did not emit a new precheck/quota/postcheck start event or invoke automatic
  filesystem repair, conversion, root mounting or hosting installation.
- Left the root superblock's last-mount timestamp at its pre-conversion value.

The first recovery diagnostic parser expected a newline before its end marker,
but `debugfs` returned the GRUB block without one. The safe recovery screen and
diagnostics were already present. The parser was corrected and the same
interrupted state was booted again: the second recovery also remained blocked.
Both logs are retained. The optional `ps` diagnostic was unavailable in this
minimal initramfs; no process-list claim relies on it.

The first quota-cut state is additionally retained as
`native-powerguard-quota-cut` (`61973878-ae47-470b-b074-e4f05a84e2b3`).

### What these cuts do not prove

The first power-off request completed approximately 284 ms after observation;
event delivery and VM termination are not synchronous with a disk write syscall.
Quota/project features and a project quota inode were present in the observed
recovery cases, and the superblock reported clean. Even the CPU-limited repeat
does not establish that a particular quota-metadata write was interrupted.

Therefore this is **actual VM power-cut containment**, not deterministic
sector-tear or partial-quota-inode qualification. It also does not emulate power
loss of the Windows host/controller or prove cache flushes on arbitrary hardware.
The checkpoints are lab fixtures, not a portable recovery feature.

## Successful-path regression

After restoring the original armed checkpoint again:

- The all-real one-shot path passed prerequisite binding, raw-device flushes,
  offline conversion, current-boot finalization and all nine installer stages.
- Project-quota/account isolation, OCI private resources, OCI lifecycle and
  rootless-container sub-UID quota tests passed.
- Normal boot passed with `alreadyInstalled=true`, `changed=false`, no conversion
  token/proof and the same completed package journal. Temporary boot artifacts
  were retired; the normal initrd/GRUB configuration remained pinned.
- A test-only startup race was corrected: SSH can be available while the native
  install service is inactive with a queued start job waiting on dependencies.
  The original run's systemd journal independently showed successful completion.
  The helper now accepts only an actual queued job or `activating`, not arbitrary
  inactive/failed state. The policy unit test and a repeated full normal boot pass.

Successful stage directories under `infra/host-tests/work/`:

- `native-boot-Prepare-20260911T082122Z`
- `native-boot-Arm-20260911T082522Z`
- `native-boot-ValidateCurrent-20260911T083112Z`
- `native-boot-NormalBoot-20260911T083540Z`

The earlier wait-race evidence remains in
`native-boot-NormalBoot-20260911T083348Z`.

All **119 top-level Linux package/CLI tests** passed without skips, including
the isolated namespace tests and new recovery-default/flush-target/intent tests.
The additional integration helper policy test passed separately. Windows
`go test ./...` and `go vet ./...`, Linux-targeted integration/package vet,
PowerShell parsing and `git diff --check` passed.

| Test artifact | SHA-256 |
| --- | --- |
| Linux installapply tests | `57b2cf5b2765b6027b7b64f3b7fe9eb8bea410c486c74eb2064d8313972e1fb0` |
| Linux storageprep tests | `cb1dee9716556502ba2597002e8eac636926102889c20c5b2d2e140cb7b9577b` |
| Linux CLI tests | `b977057d40b5e9dff8e5d99823519eaf879855be3d12aa26fe5f61f1d3a09e3a` |
| Combined Linux log | `91a4a9bbb9c72954f0fec15dbbd0313dcf8a7bd87e5f1172f78777931c6d8bf9` |

## Final state and remaining gate

The successful VM was shut down normally and preserved as
`native-powerguard-qualified-success` (`d7ca0acd-8071-41bf-8d0e-57660344b694`).
All VMs are off. The CPU maximum is back to 100 and the temporary serial pipe
was disconnected, restoring its original empty path; Secure Boot was unchanged.

Point 3 is **not completely closed**: deterministic interrupted/torn-write
testing and a verified portable external backup/recovery/restore procedure remain
before public activation. External package/kernel coordination, capacity policy,
broader firewall/listener qualification, other OS boot variants and signed native
release qualification also remain open. No unsafe automatic repair or state
reset was added to make the happy path pass.
