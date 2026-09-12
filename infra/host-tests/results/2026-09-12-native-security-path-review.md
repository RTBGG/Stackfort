# Native security review: identity, log and cache paths

Date: 2026-09-12. Scope: the reported uncontrolled-path flows in
`internal/hostidentity/runtime_linux.go` (CodeQL alert #29), `internal/hostlogs`
and `internal/hostcache`, their public wrappers, and the two focused fixes below.
This is not a project-wide security assessment or a claim of zero findings.

## Guard review

- **Identity / linger:** `Reconciler.ReconcileRuntime` and `Delete` validate the
  complete `hostingidentity.Spec`; the runtime manager independently derives
  `hostingoci.Spec` through `ForIdentity`. Canonical UUIDv7 validation binds the
  username to `sf_<26 hexadecimal characters>` and the home to the fixed account
  root. The production linger directory is privately configured as
  `/var/lib/systemd/linger`, not supplied by RPC. The reported `Lstat` at the
  original line 326 rejects a symlink/non-regular leaf, non-root ownership and
  multiple hardlinks. No caller-selected path escape was found in this flow.
- **Log paths:** public `Manager.Ensure` validates identities and normalized
  domains before the private Linux implementation creates anything. Linux
  `Read`/`ReadWAFEvents` repeat identity/domain validation and constrain log kind
  to access/error. `hostinglogs.DomainFile` combines a canonical account UUID,
  SHA-256-derived domain filename and fixed suffix. The account-facing workspace
  checks authorization, host readiness and persisted domain ownership first.
  Managed directories are checked for root ownership and exact modes; opened
  files require regular type, mode `0640`, root ownership and one link.
- **Cache metrics:** protocol and Linux entry points validate the complete
  identity and canonical domain; the same closed log filename derivation is
  used. `cacheworkspace` checks account authorization/readiness and obtains the
  domain within that account. No request-controlled traversal was found.

These conclusions assume trusted operating-system ancestors and the installed
root-owned managed log tree (`/var/log/stackfort` mode `0750`, `accounts` and each
account directory mode `0700`). Leaf `O_NOFOLLOW` does not itself prohibit
symlinks in every ancestor. Cache metrics do not yet repeat hostlogs' directory,
file-owner/group and link-count checks. Making those checks uniform and walking
ancestors with directory descriptors would strengthen malformed-host handling;
this review did not demonstrate a tenant escape through the installed tree.
No remote CodeQL alert state was changed by this audit.

## Focused findings and fixes

1. **Blocking cache FIFO open:** `countCacheLog` previously opened its candidate
   path before checking regular-file type, without `O_NONBLOCK`. An unexpected
   FIFO could therefore wait for a writer before reaching either the type or
   context check. The fix adds `O_NONBLOCK`; regular logs retain their existing
   bounded scanner behavior. Placing that FIFO requires a root-capable change
   or an incorrectly writable managed tree, not ordinary tenant access.
2. **Runtime descriptor leaks on failure:** `ensureDirectoryChain` previously
   closed its current owned directory only on successful completion. Mode,
   ownership or later-component failures leaked it. An account can alter its
   own `.local` directory; subsequent failed runtime reconciliation attempts
   could each consume another agent descriptor. `ensureAbsoluteDirectoryChain`
   had analogous cleanup gaps for malformed root-managed parents. Both helpers
   now defer closure of only descriptors they own on every return. The caller's
   parent descriptor and the separate ownership of `/` are preserved.

Files changed: `internal/hostcache/manager_linux.go`, its new
`manager_linux_test.go`, `internal/hostidentity/runtime_linux.go`, and new
`runtime_descriptors_linux_test.go`. No broader path-policy rewrite was made.

## Verification

The coordinating operator hash-verified and ran **all tests** in both Linux
amd64 test binaries as root on the existing disposable Debian 13 native VM.
Retained logs confirm **16 hostidentity tests and 2 hostcache tests passed, with
no skips** (top-level counts; subtests also passed).

- `work/hostidentity-descriptors-linux.test.log`: includes 32 repeated failures
  per invalid-mode, invalid-owner and mid-chain-symlink case; exact fixture FD
  counts remain unchanged and the caller FD retains its device/inode. The
  absolute-parent test rejects writable `/tmp` before creating any directory.
- `work/hostcache-nonblock-linux.test.log`: rejects a FIFO with no writer promptly
  and still counts the expected domain's regular-file HIT/MISS/BYPASS records.
- Linux test compilation, focused Linux `go vet`, and shared Windows tests for
  `hostingidentity`, `hostingoci`, `hostinglogs`, `agentprotocol`, `logworkspace`
  and `cacheworkspace` passed. Windows tests do not substitute for Linux tests.

Retained evidence SHA-256:

| Artifact under `infra/host-tests/work` | SHA-256 |
| --- | --- |
| `hostidentity-descriptors-linux.test` | `1f88df13db813e2894bb76c4b1fe256be5c9867ced4d7c5a27b2e4ebfa1ff2aa` |
| `hostidentity-descriptors-linux.test.log` | `4ac255def2f53514d42985605b6bae702402cae1e67162145754d1581c5042de` |
| `hostcache-nonblock-linux.test` | `e480b956bb5dcc40dfa97fd17146a378039c28d659b5e6e6b5949c56bdaa7b75` |
| `hostcache-nonblock-linux.test.log` | `7ed20da144c4be48978eaaa4711a30038986ce8bf1498ba8131115d087ddc94a` |

Limits: the path conclusions are source/guard review, not exhaustive race or
host-compromise testing. Existing hostlogs unit tests cover parsing, redaction
and pagination, not a complete filesystem-adversary matrix. The new regressions
test the focused failure paths, not every lifecycle operation, OS or kernel.

## Separate preparation evidence

The operator also successfully ran the independent vendor-OS preparation helper.
`work/native-reprovision-5f948072f3374e2cb425a5966605dd88/reprovision.json` records
stage `prepared-not-attached`, a fresh instance/seed and no VM or `known_hosts`
modification. The 50-GiB standalone system VHDX has disk ID
`6790b3ca-d3ab-414e-bb36-2cbcee750d90`; the 64-MiB seed has disk ID
`2a724d2f-e00a-4f4e-8cfc-637aab1db352`.

Source QCOW2 SHA-256:
`85a969b7e99d7c817414136033df18c58d5c45ac8d27bb36e8ccb67173d2d4e3`.
Provenance is official Debian HTTPS plus published SHA512SUMS, **not** verified
detached signatures. Preparation alone proves neither first boot nor real KVP
public-host-key retrieval nor successful installation on the new disk.
