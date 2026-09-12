# Native installation web admission and reviewed recovery — 2026-09-09

Scope: internal coordinator and opt-in Debian Hyper-V lab. No public installer
activation, release publication, commit/push or customer-server changes.
See the [design and reproduction guide](../../../docs/native-quota-service-admission.md).

## Inputs

The only started VM was `stackfort-native-quota-debian-13`,
ID `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`. Debian 13 used kernel
`6.12.107+deb13-cloud-amd64`, 2 vCPUs, 8 GiB RAM and its existing 50 GiB
system disk plus seed disk. Tests began by restoring the verified offline
`native-quota-before-conversion` checkpoint
`9e62e0ac-89ad-4851-8ae2-e6b936e40eae`.

The unchanged authenticated beta.3 fixture is the same one documented by the
[previous continuation qualification](2026-09-09-native-install-continuation.md).
Current internal code drives its payload; the old candidate executables do not
gain native-storage functionality. nftables was already installed on the
baseline; the prerequisite apt invocation installed/upgraded no packages.
After source retention, the bootstrap fixture directory was renamed to
`/var/tmp/stackfort-origin-retired`. No fallback download/extraction was used.

| Identity | Value |
| --- | --- |
| Workspace base HEAD, with local changes | `ef74732bdcb18caf9ddd3d5aa3024b4381c6c3f2` |
| Integration helper SHA-256, unchanged throughout boots | `f94d9a9971c1be65389b6682114faf683b2a3f0687b70f623d94324363508b68` |
| Linux installapply test executable SHA-256 | `83940ed8ef93f2e4da60e46ac51f28f3cda73a9fd9419e944b7bcd32a0d58162` |
| Candidate archive SHA-256 | `3bf0987612d902df1e5fd2f159235d65f1f13a2f19a4f673e84befa170bd0df0` |
| Attestation bundle SHA-256 | `b929c9d6a8e15726f53381bb35e2bc1912ab13908b2fadffe5c73e45178d8344` |
| Retained source digest | `48babc7785a3025828ea89735959a107d2a95bbec0e66486a3dee3e1ce30caa8` |
| Native operation | `449c784e-a369-4151-91e9-5d8242ccf382` |
| Native manifest digest | `6376592062fa3eeb91b524349a850b1caad7b3fc1a67d1fefe44795a09dbceca` |

## Verified scenarios

1. **Early gate and actual conversion.** The gate is closed before the storage
   resumer. The authenticated one-shot conversion finishes, storage becomes
   `ready`, and all package transactions complete behind the closed web gate.
   GRUB consumes the one-shot entry; normal initrd remains unchanged.
2. **Actual process loss.** The sealed `pause-services-once` fault pauses before
   applying the service stage, after the first eight stages complete. The test
   sends SIGKILL only to the coordinator main process. `ExecStopPost` closes
   and verifies the gate and stops the checked fixed consumers. Admission
   remains `checking`, attempt 1, with the original partial package journal.
   No apt or filesystem metadata write is deliberately interrupted.
3. **No implicit recovery.** A direct service-start retry fails with the
   explicit-recovery requirement. A request with an all-zero admission digest
   is rejected. Both journal hashes remain exactly unchanged. SSH still works;
   Windows HTTPS times out with curl exit 28 and HTTP code 000.
4. **Reviewed continuation.** Matching both actual digests permits attempt 2.
   The service stage completes on attempt 2; the other eight stages retain
   attempt 1. Package journal becomes complete, admission becomes admitted,
   and Windows HTTPS health returns 200. Quota/account isolation, OCI resources,
   real container deployment lifecycle and container subordinate-UID quota
   tests all pass.
5. **Normal reboot.** A fresh early gate is closed and all installed-state and
   health checks repeat. Result is `alreadyInstalled=true`,
   `resumed=false`, `changed=false`, with no package-stage replay.
   Quota/OCI checks pass again and the conversion helper does not rerun.
6. **Real external network gate.** With NGINX still active and loopback HTTPS
   returning `status=ok`, explicitly closing the gate causes Windows public
   IPv4 HTTPS to time out. SSH remains available. This distinguishes an actual
   network barrier from an inactive listener. The inet TCP/UDP policy is
   verified; this is not an external IPv6 reachability test.
7. **Complete journal is insufficient.** Only the installed panel
   `/usr/share/stackfort/web/index.html` mode is changed from 0644 to 0600.
   Its content hash remains unchanged. On a real normal boot, immutable
   payload verification rejects the mode. Admission becomes
   `recovery-required`, attempt 4; package journal stays complete and
   unchanged. Gate remains closed, checked consumers are inactive, and the
   verified hosting mount remains available for recovery.
8. **Repair alone does not bypass recovery.** With the original 0644 mode
   restored, another normal boot still refuses automatic continuation. The
   admission record remains attempt 4 and the package journal remains
   unchanged. Only the explicitly reviewed pair of digests permits attempt 5.
9. **Final healthy reboot.** After reviewed recovery, another normal boot
   passes all four quota/OCI subtests, preserves the complete package journal,
   and returns `alreadyInstalled=true` without stage replay. Admission is
   `admitted`, attempt 6; Windows HTTPS health returns 200 again.

The metadata test changes an installed file, not the retained authenticated
source. The original mode is restored explicitly after the rejection; no
source/journal reset is used.

## Record digests

These are reviewable state identities, not secrets or reusable recovery flags:

| Snapshot | SHA-256 |
| --- | --- |
| Admission after actual interruption | `4426ac269b20fb3d072d4eb7f986291ded991fdce6b439d41d4b7465498e00a8` |
| Partial package journal | `11244c466ca244f3b9240b84c7b5c0a41f2221804b185bd1905c0f394ea5e4f0` |
| Complete package journal after recovery | `797b51aba4c0a456e54da3b87404ac8e899212fcc38eff0e6ef9feec7cbe5e9f` |
| Admission after installed-metadata rejection | `c28f60c9bf14d55f6ff6203fa58db97d634487c79ab22249351394645d9b688f` |
| Final admission after healthy reboot | `fa9aa949b810d06c8b32e70ed6b01db151ad9106e8be6d2550ded194659e17da` |
| Final storage journal | `ccd2fcf5c4694d23e044f8c67b46003a991b78672395c7cb7482721ad62e70ce` |
| Installed index content before/after mode fault | `10aa64b61bd2b6c5b90b6471ebdd8937e8d6bdf8a08a438c77df4b13f98d89b4` |

## Automated checks and retained evidence

- Windows `go test ./...` and `go vet ./...`: pass.
- Linux cross-target `go vet -tags=integration ./...`: pass.
- Actual Debian `internal/installapply` executable: 61 top-level tests pass,
  including new admission state/recovery and strict record tests.
- Real nftables private network namespace: create, close/open, idempotence and
  refusing an altered owned table without modifying it all pass. The child
  verifies it is not in the host network namespace.
- Updated PowerShell wrapper parses; `git diff --check` passes.

Ignored local evidence is retained in `infra/host-tests/work/`:

- `admission-unit-linux.log`, `admission-fixture.log`.
- `native-journal-Prepare-20260909T141613Z`.
- `native-journal-Arm-20260909T141710Z`.
- `native-journal-InterruptInstall-20260909T141728Z`.
- `native-journal-RecoverInstallation-20260909T142008Z`.
- `native-journal-ValidateCurrent-20260909T142046Z`.
- `native-journal-NormalBoot-20260909T142118Z`.
- `native-journal-WebGateProbe-20260909T142207Z`.
- `native-journal-BlockedInstallBoot-20260909T142258Z`.
- `native-journal-BlockedInstallBoot-20260909T142403Z`.
- `native-journal-RecoverInstallation-20260909T142559Z`.
- `native-journal-NormalBoot-20260909T142640Z`.

Each wrapper directory retains the helper, test log, boot log and bounded-scope
evidence archive. Raw archives and binaries are not added to Git. Expected
failure logs from deliberate rejection must not be read as successful installs.

After the final health check, the VM was gracefully powered off and checkpoint
`native-admission-qualified-success`
(`ba8282d5-1ae4-4336-9ce7-d6f760f552b9`) was created while offline. Existing
checkpoints, including the prior continuation success, were retained. All five
Stackfort VMs are off. On a future boot, the historical admitted record still
requires a fresh closed-gate verification cycle.

## Qualification limits

The gate covers fixed local web ports, not arbitrary tenant services, container
forwarding or every possible firewall service. It does not continuously
monitor quota enforcement or defend against hostile root. The cleanup list is
fixed and does not enumerate all dynamic account/OCI units.

Production boot backend/coordinator/recovery UX, bootstrap prerequisite order,
listener/firewall integration, external package/kernel serialization, capacity
reserve and ordinary Debian/Ubuntu/Rocky provider onboarding remain open. A new
signed native-runtime candidate is still required. These tests do not qualify
power loss during ext4 conversion or forcefully interrupted package writes.
