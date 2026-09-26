# Large native initramfs inventories

Status: **corrected in published experimental Beta.13**, verified on fresh
MBR/BIOS and GPT/UEFI hosts with real 90,649-byte one-shot inventories.
The public bootstrap selects Beta.13. This is not an upgrade or recovery path;
use only fresh disposable Debian 13 amd64 test hosts within the qualified scope.

## Reported failure

On 2026-09-26 a Debian 13 amd64 operator reported successful prerequisite/runtime
preparation followed by `native one-shot boot arming did not complete` and the
recorded storage failure `arm-failed`. A one-shot image existed, but the custom
GRUB entry did not. Read-only listing succeeded with **1,578 lines / 93,120 bytes**
and all five required Stackfort members present.

Beta.12 routes `lsinitramfs` through the same 64 KiB output buffer used for short
boot-command diagnostics. That rejects this legitimate-size inventory. The
outer arming command also discards its child's stderr, hiding the detailed
reason. The retained report demonstrates a definite rejection in that verifier;
it cannot recover the original discarded stderr or rule out an additional
earlier failure on the affected host.

No customer image, raw installer transcript, setup token, hostname, operation ID
or private state is committed. Regression fixtures contain synthetic filenames
and reproduce the reported dimensions, not the customer's image contents.

## Correction

- Stream the listing into one 4 KiB line buffer and five required-member flags;
  do not retain the complete inventory or reuse the attestation-output buffer.
- Independently limit listing output to 8 MiB, 65,536 lines and 4,096 bytes per
  line. The line length excludes its newline. Control characters and an
  unterminated final line are rejected. All output is checked, including data
  after the required members. Exact required paths remain mandatory and test
  executables remain forbidden.
- Require successful command exit and complete validation before creating the
  custom GRUB entry. Keep the 15-second deadline, process-group cancellation and
  separate 64 KiB stderr cap. All other boot-command and attestation caps remain
  unchanged. No recovery latch, source verification or quota check is bypassed.
- Retain at most 8 KiB of the arm child's stderr while draining the rest. On
  failure, show the bounded diagnostic, exit failure and timeout/cancellation
  status. Escape terminal control characters, redact setup-code-shaped values,
  and disclose truncation. The raw setup code is still never passed to the child.
  Diagnostics can include local paths; review them before sharing.

## An already stopped installation

Do **not** clear journals, rerun internal `native-boot arm`, replace the sealed
runtime, manually select the temporary image or reboot as a workaround. The
recorded `recovery-required` state is terminal for this conversion attempt.
See the [read-only recovery policy](native-installer-recovery-policy.md).
Source fixes do not make an interrupted Beta.12 installation resumable.

Beta.13 passed its exact-candidate release gates and was published unchanged
after RTBGG's explicit fresh-only experimental authorization. Real generic-kernel
inventories exceeding 64 KiB passed on both the MBR/BIOS and GPT/UEFI fixtures.
Future provider reinstallation still requires the owner's explicit decision;
it is not performed by this fix.

See [source regression results](../infra/host-tests/results/2026-09-26-beta13-initrd-regression.md),
[completed exact-candidate qualification](../infra/host-tests/results/2026-09-26-beta13-candidate-qualification.md)
and [public installation status](../infra/host-tests/results/2026-09-26-beta13-public-installation.md).
No independent security review, upgrade, public resume or production claim is implied.
