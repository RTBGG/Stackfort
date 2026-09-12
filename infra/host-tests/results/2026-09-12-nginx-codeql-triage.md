# NGINX path and integer-boundary review — 2026-09-12

Scope: the repository UI's CodeQL path alerts #15–27 and cache ownership
conversion alerts #69–70, using the file/line mapping reported by the maintainer.
This is a bounded internal source review, not an independent security audit,
remote alert dismissal, or approval of a final release candidate. The complete
remote CodeQL traces were not exported for this review. A green scanner job
does not mean that existing alerts have been dismissed or independently resolved.

## Activation paths (#15–17)

The reviewed sinks are the account file and new revision/transaction directory
checks in [`activation_store_linux.go`](../../../internal/hostnginx/activation_store_linux.go).
No tenant-selected filesystem path was demonstrated through the production
activation entry point:

- [`Activator.Activate`](../../../internal/hostnginx/activation.go) constructs its
  private candidate from `nginxconfig.RenderSpecs` / `RenderAccount`, not a
  caller-supplied filename. [`RenderAccount`](../../../internal/nginxconfig/renderer.go)
  validates the full hosting identity and canonical activation revision, then
  derives exactly `account-<account UUID>.conf`.
- [`hostingidentity.Validate`](../../../internal/hostingidentity/spec.go)
  requires a canonical lowercase UUIDv7 and its derived username/home, the
  reserved UID range, and matching GID. The renderer also validates the revision
  through `core.ParseID`; slashes and traversal components are not accepted IDs.
- Before staging paths, `linuxActivationWorkspace.Stage` independently checks
  all three UUIDs and the exact derived filename. `Active` uses the renderer's
  candidate and requires equality with the current revision and its validated
  manifest before reading the account path.
- `currentRevision` permits only the exact relative symlink target
  `site-revisions/<canonical UUIDv7>`, checks the target root-owned mode-0750
  directory, and rejects noncanonical targets. Manifest/journal readers reject
  unknown/trailing JSON and invalid identities or phases. `copyActiveRevision`
  validates each inherited `account-<canonical UUIDv7>.conf` name.

The store and candidate structs are private implementation details. They are
not a general safe-path API: a future caller that bypasses renderer validation
must not assume that `filepath.Join` itself rejects traversal.

## Configuration helpers (#18–27)

The creation, ownership, rename, rollback and read sinks in
[`configuration_linux.go`](../../../internal/hostnginx/configuration_linux.go)
receive fixed baseline intents or the constrained activation paths above.
The production manager root is the literal `/`; alternate roots are private
test fixtures, not an API field or installer argument.

`Prepare` enumerates constant configuration/directory intents from
[`nginxbaseline`](../../../internal/nginxbaseline/spec.go) and WAF configuration.
`validateAnchors` checks `/etc`, `/etc/nginx`, `/etc/systemd` and
`/etc/systemd/system`. `safeRootDirectory` requires a real nonsymlink directory,
UID/GID zero and no group/other write permission. Existing unsafe directories
are rejected, not repaired into acceptance. Rollback snapshots are generated
from the same intent list; arbitrary serialized rollback paths are not accepted.

`readSafeRootFile` requires a root-owned regular file with one hard link;
callers additionally check expected content/mode where appropriate.
`atomicWrite` creates a unique temporary in the selected parent, sets ownership
and mode, writes and syncs it, renames it and syncs the parent directory.
`snapshotPath` separately rejects symlinks and nonregular nondirectory inputs;
its snapshot read is not itself the hard-link check used by `readSafeRootFile`.

The helper also serves panel configuration. Those callers use fixed panel paths,
validated content-derived bundle paths or validated ACME tokens, not arbitrary
write destinations. [`panel_linux.go`](../../../internal/hostnginx/panel_linux.go)
checks the destination parent and existing file; its input reader walks path
components with `openat` / `O_NOFOLLOW`, checking root-owned nonwritable ancestors,
regular single-link files and bounded sizes. User-selected certificate/key paths
are read-only inputs, not write targets.

### Trusted-ancestor limitation

The configuration helpers use path-based operations after `Lstat` checks; they
are **not** a descriptor-pinned, race-proof filesystem sandbox. This finding
depends on the fresh managed host's root-owned nonwritable ancestors preventing
unprivileged replacement. It does not prove safety against concurrent privileged
ancestor replacement or arbitrary root edits. Some file-mode checks are caller
specific; `readSafeRootFile` alone is not a universal immutable-file policy.
No tenant-controlled traversal into these sinks was found within that stated
boundary. Keep the boundary when adding callers; do not generalize this review
to other filesystem helpers or preexisting unmanaged host state.

## Cache ownership conversions (#69–70)

[`cacheRuntime`](../../../internal/hostnginx/cache_runtime_linux.go) looks up the
distribution's fixed NGINX worker (`www-data` or `nginx`) after checking that the
specification matches the supported baseline. UID/GID are parsed using
`strconv.ParseUint(..., 10, 32)`; parse errors and zero are rejected before
conversion to `int` for `os.Chown`.

For the public native **amd64** target, `int` is signed 64-bit, so every parsed
32-bit unsigned value fits. The architecture restriction is explicit in
[`native_host_linux.go`](../../../internal/installapply/native_host_linux.go),
[`runner_linux.go`](../../../internal/installapply/runner_linux.go) and the
[bootstrap](../../../packaging/installer/install.sh). This is not a claim that
the casts are safe on a future 32-bit port; such a port needs checked conversions
and new qualification. The host account database is trusted configuration, not
a tenant-selectable numeric owner.

## Verification and limits

Focused Windows tests for `internal/hostingidentity`, `internal/nginxconfig` and
`internal/hostnginx` passed. Windows execution does not run Linux-only tests.
Existing relevant coverage includes:

- [`renderer_test.go`](../../../internal/nginxconfig/renderer_test.go):
  `TestRenderAccountScopesActivationRevisionHeaderToProbeURI` rejects a
  traversal-shaped activation revision.
- [`spec_test.go`](../../../internal/hostingidentity/spec_test.go):
  substituted identity fields are rejected.
- [`cache_runtime_linux_test.go`](../../../internal/hostnginx/cache_runtime_linux_test.go):
  root-only ownership/idempotency, writable-directory and symlink rejection.
- [`panel_linux_test.go`](../../../internal/hostnginx/panel_linux_test.go):
  unsafe key permissions, leaf/ancestor symlinks, hard links, writable ancestors,
  oversized inputs and FIFO rejection.
- [`activation_test.go`](../../../internal/hostnginx/activation_test.go) and
  [`host_nginx_linux_test.go`](../../../tests/integration/host_nginx_linux_test.go):
  activation/recovery and transactional revision behavior.

No new real-host Linux execution was performed as part of this source review.
Existing lifecycle tests are not a substitute for hostile-input coverage at
every private store method or qualification of the final tagged archive.
The separate [SQL review](2026-09-12-sql-codeql-triage.md) records a real adjacent
authorization defect; this NGINX conclusion must not be used to dismiss unrelated
scanner findings in bulk.
