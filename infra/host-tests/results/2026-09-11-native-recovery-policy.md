# Native recovery policy and read-only handoff — 2026-09-11

Result: **the current-source recovery advice command passes Windows checks,
Linux isolated state-inspection tests and read-only inspection of the real native
Debian installation**. No restore, repair, reimage, conversion replay, public
activation, package replacement, commit, push or release was performed.

The [policy](../../../docs/native-installer-recovery-policy.md) defines the
fresh-disposable and externally verified backup boundaries. Its pre-preparation
owner-choice/evidence binding is still outstanding; this result does not claim
that a generic backup validator or public consent flow exists.

## Scope

- `stackfort-installer native recovery-plan [--format=text|json]` calls only the
  existing locked `native status` operation. No mutation options are accepted.
- The pure classifier covers 24 state scenarios and rejects 20 malformed or
  contradictory snapshots plus inspection errors. Tests verify that no authority
  is granted and that input state is unchanged.
- Four CLI scenarios in both text and JSON exercise exit codes 0/1/2, limited
  evidence, generic errors and the sole status call. Mutation options, invalid
  formats, help behavior and failed output are tested separately.
- Fourteen Linux mount-namespace fixtures exercise real absence/empty state,
  busy/missing/unsafe locks, incomplete/complete prerequisite receipts, malformed,
  duplicate and unknown JSON, symlinks, hardlinks, FIFO and orphan runtime state.
  Before/after snapshots retain names, ownership, modes, inode identity, links and
  regular file contents. The actual host's `/var/lib` is not changed by fixtures.

All **107 top-level tests** in the Linux `internal/installapply` and installer CLI
binaries passed, with **zero skips/failures**, including the existing isolated
firewall and prerequisite tests. Windows `go test ./...` and `go vet ./...` passed;
Linux-targeted vet for both changed packages passed. Targeted Windows coverage is
100% of statements for both the classifier and its snapshot validator. This is
not 100% branch coverage or a substitute for live-host qualification.

## Real Debian observation

The existing `stackfort-native-quota-debian-13` VM
(`4361f439-15e9-4f9e-a690-9a8e44b6cbd3`, DMI
`6365bd88-5141-4f15-b3f8-2ba9996baad2`) was normally started from its retained
successful state. The restored clone was kept off to avoid duplicate identity.
No checkpoint was restored. Debian reported a normally running system and no
conversion/recovery token on its kernel command line.

The new CLI ran as a separate `/var/tmp` binary, **not** as a replacement for the
sealed runtime. Both formats reported `admission-recorded` for operation
`81b5c629-4a3e-4510-bc2a-a28a9bed2636`, with complete prerequisites and packages.
All backup/live-readiness/public-resume/destructive/automatic-recovery flags
remained false. A stored admission is explicitly historical, not a new live-health
qualification.

Before/after SHA-256 comparisons around the tests and CLI calls passed for:

- `/etc/fstab` and `/boot/grub/grub.cfg`;
- storage, admission and installation package journals;
- the sealed native runtime executable, release manifest and runtime intent.

The normal boot did advance the existing supervisor's admission attempt to 5;
the assertion of unchanged journals applies **after boot, across the new tests
and inspections**, not across an operating-system start. The sealed runtime hash
remained `204503d56dadb60550d70e6fd7d8561cacae822844958d1b55363a9e5bb79bc2`.
The guest was gracefully shut down afterward. Original checkpoints, backups and
the restored clone were retained; all six lab VMs are off.

## Artifact pins and reproduction

Local retained artifacts are under `infra/host-tests/work/` (ignored):

| Artifact | SHA-256 |
| --- | --- |
| `native-recovery-policy-installapply.test` | `0d5b35c46c13e7ea147516f878d380e6249ae342ca29dea5b26caefe76609a35` |
| `native-recovery-policy-cli.test` | `bfc31dac4dd50dd34f17a695d4f82ff2de27744e8c831745ff9b61aab7f4a39c` |
| `native-recovery-policy-installer` | `ecd01ab8b03fa2b1e369ac1506261d5d282599ef728b186eb360da09cf70a29b` |
| `native-recovery-policy-linux.log` | `d963771f23bdb988316c7546ce0e525ddf1ad8af22d9372001dc9d289d7e80c1` |

Build the two test binaries and CLI from current source for Linux/amd64 with
CGO disabled. On the explicitly selected disposable root host, run:

```sh
sudo env STACKFORT_DISPOSABLE_HOST_TEST=1 \
  /var/tmp/native-recovery-policy-installapply.test -test.v -test.timeout=120s
sudo /var/tmp/native-recovery-policy-cli.test -test.v -test.timeout=120s
sudo /var/tmp/native-recovery-policy-installer native recovery-plan --format=json
sudo /var/tmp/native-recovery-policy-installer native recovery-plan
```

The full-disk restore and hard-power-off experiments were not repeated: this
change adds advice and CLI dispatch, not filesystem/boot mutation or new recovery
permissions. Other distributions and a newly signed native candidate remain open.
