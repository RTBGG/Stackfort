# Large native initramfs inventories

Status: **source correction prepared for unpublished Beta.13**. This is not an
upgrade, recovery procedure, qualified release or publication approval. The
public bootstrap and immutable Beta.12 assets remain unchanged.

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

Beta.13 preparation is authorized, but publication is a separate decision after
its exact candidate passes release gates. A fresh disposable Debian 13 host with
a generic kernel and a verified inventory over 64 KiB must be included in those
tests, alongside the existing MBR/BIOS and GPT/UEFI scope. Do not reuse older
candidate pass reports. Future provider reinstallation requires the owner's
explicit decision; it is not performed by this fix.

See [regression results and remaining candidate checks](../infra/host-tests/results/2026-09-26-beta13-initrd-regression.md).
