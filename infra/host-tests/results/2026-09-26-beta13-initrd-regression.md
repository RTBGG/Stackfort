# Beta.13 preparation: initramfs inventory regression

Date: 2026-09-26. Status: **source preparation, not release qualification**.
RTBGG authorized implementing the correction/tests and preparing Beta.13.
This is not candidate publication approval. No previous immutable release,
public bootstrap default, customer VPS or native recovery journal was changed.

## Evidence and implementation

The operator's read-only check returned 1,578 lines / 93,120 bytes, exit 0, with
all five required Stackfort paths present. Beta.12's inventory runner is bounded
to 65,536 bytes. Its outer arming wrapper hides stderr. See the
[diagnosis, limits and recovery boundary](../../../docs/native-installer-initrd-inventory.md).
The synthetic regression matches those dimensions without retaining customer
paths, tokens or other private state.

Source tests cover:

- The reported-size listing across one-byte, small, boundary and whole-output
  writes, including required members beyond the former 64 KiB boundary.
- Exact required-member matching, missing members, forbidden test executables
  after the required paths, truncated lines and fail-closed reuse prevention.
- Inclusive byte/line/entry limits and rejection immediately above them;
  control-character rejection and `io.Copy`/`io.WriteString` limit enforcement.
- Linux subprocess failure after emitting all members, stderr overflow,
  forbidden tail, cancellation and deadline; the short-output runner no longer
  accepts `lsinitramfs`.
- Linux reproduction of the old 64 KiB failure with the same synthetic fixture.
- Bounded arming-error capture, complete pipe draining, setup-pattern redaction
  across writes/truncation, escaped terminal controls and visible timeout errors.
- Real Linux helper-process arming failures and verbose success, without calling
  the privileged boot backend, modifying GRUB or rebooting.

## Local verification and environment limits

- Focused platform-independent regression/controller tests passed on Windows
  with Go 1.26.6.
- `go vet ./cmd/... ./internal/...` passed.
- Both affected packages' Linux amd64 test executables and the development
  installer cross-compiled successfully. Cross-compilation is **not** Linux test
  execution or a distributable release archive.
- Broad Windows source-package tests passed except that Windows denied launching
  `internal/ociimage`'s test executable. An earlier `go test ./...` also failed
  while traversing an inaccessible historical artifact directory. Neither is
  reported as a full-suite pass; no filesystem permissions were relaxed.
- Hyper-V inspection was denied in this session; no host was rebooted, restored,
  reprovisioned or used for a new candidate installation. WSL is not installed.
- Native Git HTTPS access failed at Windows Schannel credential initialization.
  The connected GitHub API remains available for a separate candidate review/CI
  branch; GitHub results must be checked independently, not inferred here.

## Remaining release prerequisites

1. Review the exact candidate source and complete Linux CI/security, including
   the new subprocess regressions and existing root-only checks.
2. Build and retain an unpublished `0.1.0-beta.13` candidate with genuine build
   provenance, then bind its exact hashes to tag qualification without rebuild.
3. Run actual large-inventory/generic-kernel fresh-host installation and the
   required MBR/GPT onboarding, setup, hosting, resource/isolation, WAF/cache,
   OCI, rerun/reboot, failure-containment and full-OS-removal checks.
4. Obtain candidate-specific publication/scope approval and record only genuine
   results. Do not assume Beta.12's fresh-only exception automatically authorizes
   a new release or claim upgrade support.
5. Publish only after the applicable gates pass; verify public asset bytes
   before advancing the one-line default and testing the real public pipeline.

There is no new release asset, installer override or supported repair procedure
for the operator's stopped Beta.12 host in this preparation record.
