# Native host eligibility and prerequisites — 2026-09-11

Result: **passed for the internal Debian 13 plain-GPT/ext4 profile**.
Public native installation remains disabled. No release, commit or push was
performed. See the [implementation contract](../../../docs/native-installer-host-eligibility.md).

## Scope and fixture

- Only `stackfort-native-quota-debian-13` was operated:
  VM ID `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`.
- Debian 13, kernel `6.12.107+deb13-cloud-amd64`, 2 vCPU, 8 GiB assigned RAM,
  approximately 50 GiB GPT/ext4 root. Hyper-V Secure Boot remained enabled.
- Root UUID `a88eaa57-e875-4855-a3cb-c231758653f8`;
  PARTUUID `54964bdf-2add-41b7-b41e-6d483962d021`.
- The prior successful Point-1 snapshot was preserved. The original offline
  `native-quota-before-conversion` baseline was restored for the new test.
- A reviewed APT purge simulation listed exactly two removals. Only `quota` and
  `nftables` were then purged from this disposable fixture; no autoremove or
  customer-host changes occurred. Existing firewall tables were empty.
- The resulting offline `native-host-missing-prerequisites` snapshot is
  `dd956c2c-b555-4d74-8812-06e85a149d39`. It retains the fixture's preinstalled
  boot, build and unused Podman packages; this is not a claim that every minimal
  provider image has been exercised.

The authenticated beta.3 archive and attestation fixture are unchanged from the
[Point-1 evidence](2026-09-11-native-installer-preparation.md).

## Verified cases

1. The new read-only CLI accepted the unconverted baseline with both prerequisites
   already installed. It created no installer state.
2. Starting from the missing-prerequisites checkpoint, the **real**
   `SourceStage.PrepareNativeBoot` authenticated the retained release, inspected
   eligibility and installed exactly `nftables=1.1.3-1` and `quota=4.09-1+b1`.
   The APT protocol-v2 hook accepted the exact new-package transaction. The
   completed receipt records matching before/after boot hashes and host geometry;
   the package hash returned to the original inventory. No normal kernel, initrd,
   fstab or GRUB configuration changed during this prerequisite transaction.
3. The temporary service-start policy was absent after completion. The receipt
   hash was sealed into the offline intent before arming.
4. Real arming built the separate initrd and selected one GRUB entry. A second
   arming request did not repeat the journal transition.
5. The one-shot boot verified the new receipt before offline writes, converted
   ext4 project quota, finalized storage and completed all nine installation
   stages with the normal dispatcher, not a test executable in the boot path.
6. Real account isolation, project-quota enforcement, private OCI resources,
   deployment lifecycle and rootless-container sub-UID quota tests passed.
7. A normal reboot passed the same runtime/quota/OCI checks with
   `alreadyInstalled=true`, `changed=false`, no conversion argument and no
   current-boot offline-conversion proof. The admission attempt advanced from
   1 to 2 without reinstalling packages.
8. The read-only CLI rejected the now-installed server (exit 2), identifying
   existing packages, data, identities, listeners and converted storage. The
   native service remained active. `native status` reported the completed
   prerequisite receipt while keeping public resume and live-readiness claims off.

All **116 top-level Linux tests** across `internal/installapply`,
`internal/storageprep` and `cmd/stackfort-installer` passed without skips, including
the explicit disposable namespace checks. New tests cover:

- malformed/held/broken/foreign-architecture package inventories and conflicts;
- reserved TCP/UDP ports, including an actual UDP listener in an isolated namespace;
- upgrade/removal/reinstall/version-drift/unapproved-dependency APT actions;
- receipt identity, canonical encoding, modes, orphan files and bound boot digest;
- real shared-lock journal transitions to complete/recovery-required, rejected
  plan changes and resets, unchanged bytes after rejected transitions, and valid
  prerequisite-only operator status;
- read-only inspection creating no installer directory, isolated-host rejection,
  and public-installation blocking even before a storage journal exists.

Windows `go test ./...` and `go vet ./...` passed. Linux-targeted
`go vet -tags=integration` passed for the three affected packages and integration
tests. `git diff --check` passed.

## Artifact identities

| Artifact | SHA-256 / identity |
| --- | --- |
| Real installer, both runtime/prerequisite copies | `16be3acb2e6038f8aa2b327bd9cfc407f8055581a4b2ecad29abae6d63a1540f` |
| Boot qualification helper | `396254bc85d0648af3a5ae5dea9b3f0d1ae2ede661dd338f1537811a6f66ebd1` |
| Linux installapply test executable | `e89d3dd9cabdea31183b25ae2210fd902ebb01a717bb34f0a4f12be9b81db969` |
| Linux storageprep test executable | `53d69138e603da93af5673293a0662f30889ad28e106002086a751d1ce200cd5` |
| Linux installer CLI test executable | `593dc61909d2f09b2501b34299ff214aa019bf7eed1c04c86dffaeac69ed73e9` |
| Completed prerequisite receipt | `be389d9bf7cb6271c56cae2d60d1078e11c41d808112a5244c37909f8ad21992` |
| Runtime intent | `82076fb29b665e6acde5792c08c814a787be38ad95ca208b1086b6cb5bbb7ad1` |
| Release manifest | `f2425db4438cf2c977a4926d1b2c0dbfc452ecd365e3dca193d8a1c198ee18ca` |
| Completed package journal | `48809c1f0b38a624ff623bb34c1ffecf53ad3bdfcf4b12e1bef4aca33803aaaf` |
| Operation | `40020cd9-e4e8-4214-ba49-ef9dffeac608` |

Ignored local evidence directories under `infra/host-tests/work/`:

- `native-boot-Prepare-20260911T075737Z`
- `native-boot-Arm-20260911T075858Z`
- `native-boot-Validate-20260911T080002Z`
- `native-boot-NormalBoot-20260911T080317Z`

The final `native-host-linux-tests.log` hash is
`ae31b75c69d83f034f82f0887fe3b33ee33726e97d35b4bd9c25459adddff689`.
The CLI reports and final receipt/status capture are also retained locally.
The successful VM was shut down normally and saved as the offline
`native-host-qualified-success` checkpoint:
`9602bfb4-ba39-41f8-a308-70f9b56a2e6e`. All test VMs were left off.

## Deliberate limits

This does not qualify power loss during APT/dpkg or filesystem metadata writes,
nor offer automatic recovery from either. Rejected transaction plans and durable
interrupted-state behavior are tested; no power cut was injected into dpkg.
No live post-arm receipt-tampering reboot was added in this step; binding rejection
is covered by unit tests and the actual positive initramfs/runtime path.

Provider-wide eligibility, external package/kernel/initrd coordination across
boots, capacity reserves, complete listener/firewall boundaries, Ubuntu/Rocky
native boot profiles and signed public release qualification remain open.
