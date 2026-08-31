# Integration tests

Integration tests exercise the API/agent protocol, desired-state reconciliation,
configuration rendering, service validation, rollback, quotas, and cross-account
isolation on disposable supported distribution images.

The managed-NGINX test also renders typed static and redirect-only domains into
an isolated temporary `http` context and requires the distribution's real
`nginx -t` to accept it. Its redirect fixture contains unknown dollar-prefixed
markers: successful parsing proves user URL literals were encoded instead of
being compiled as NGINX variables, while renderer-owned variables remain live.
The activated lifecycle additionally sends real apex and `www` requests for
canonical 301, customer 301/302, inactive-host 404, and every selected
path/query preservation behavior, checking the exact `Location` value.

The same disposable-host test activates a complete global sites revision with
the production agent implementation, repeats it through a fresh activator to
model an agent restart, then constructs a synced `promoted` journal/pointer
crash state. A second fresh activator must restore and reload the prior valid
revision, remove only the interrupted tree, activate the final revision,
health-check it over loopback, and leave the journal absent and current symlink
exact on every supported distribution.

It also stages a generated P-256 certificate bundle through the production
fixed-path host stager, verifies idempotency and root-only modes, activates an
HTTPS redirect host, and completes a real SNI/SAN-checked TLS handshake. On
Rocky this runs with SELinux enforcing and the inherited `httpd_config_t`
certificate context.

The first Linux-only protocol tests use a real filesystem Unix socket to verify
`SO_PEERCRED` admission, unexpected-UID rejection before handler entry, the
64-KiB request boundary, a typed version handshake, and a typed capability
report. The repository smoke script also checks socket mode, capability fields,
and the packaged agent process.

The project-quota and account-boundary test is deliberately excluded from the
default suite. Build it for Linux and run it as root only in a disposable
host-test VM:

```sh
go test -tags=integration -c -o /tmp/stackfort-host-integration.test ./tests/integration
sudo STACKFORT_DISPOSABLE_HOST_TEST=1 /tmp/stackfort-host-integration.test -test.v
```

It creates temporary reserved-range Unix identities below `/srv/hosting`,
proves hard byte and inode enforcement, checks that a second account cannot
traverse the first account root, and verifies that document-root creation does
not follow a symlink. It also reconciles a real account systemd slice, verifies
limit changes, proves the aggregate task limit through `pids.events`, and
observes a contained account OOM kill through `memory.events`. Cleanup is
limited to the randomly generated account paths, identities, and exact
disposable account slice.

K-001 adds `TestDisposableHostFileManagerNavigation` to the same disposable
binary. It verifies the fixed managed root, bounded resumable pages across more
than 100 entries, safe omission of a control-character filename, symlink
visibility without traversal, rejected `..`, and rejection of a forged
redundant hosting identity. The focused Hyper-V wrapper runs only that test and
returns a VM to its prior powered-off state:

```powershell
.\infra\host-tests\Test-StackfortFileManagerHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

K-002 adds `TestDisposableHostFileManagerDownload`. The focused wrapper builds
both the integration binary and the real production agent binary, then proves
full/range reads, account permission enforcement, symlink rejection, the 4-GiB
response ceiling, a bounded range from a larger sparse file, and cancellation:

```powershell
.\infra\host-tests\Test-StackfortFileDownloadHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

K-003 adds `TestDisposableHostFileManagerWrite`. The focused wrapper builds
the integration binary and the real production agent/helper once. It verifies
hidden staging, exact-offset chunking, resume through a fresh helper process,
SHA-256 completion, atomic no-replace activation, safe empty-file/directory
creation with expected modes, conflict preservation, and cancellation:

```powershell
.\infra\host-tests\Test-StackfortFileWriteHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

K-004 adds `TestDisposableHostFileManagerOperations`. It exercises atomic
rename/move, recursive staged copy, no-replace conflicts, symlink rejection,
hidden internal roots, bounded trash list/restore/purge, and a real project
quota failure without a visible partial destination:

```powershell
.\infra\host-tests\Test-StackfortFileOperationsHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

K-005 adds `TestDisposableHostFileArchiveOperations` for bounded ZIP/tar.gz
creation and hostile-input-safe extraction through the production helper:

```powershell
.\infra\host-tests\Test-StackfortFileArchivesHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

K-006 adds `TestDisposableHostLocalBackupRestore`. It verifies root-only
repository modes, authenticated manifests, complete payload verification,
document-root and account-file replacement, internal-root preservation,
modified-payload/symlink rejection, account isolation, and staging cleanup:

```powershell
.\infra\host-tests\Test-StackfortLocalBackupsHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```

K-010 adds `TestDisposableHostWAFRuntimeAndPerformance`. It sends one fixed
benign/hostile corpus through static, PHP, redirect, ACME, and TLS paths with
WAF off, detection-only, and blocking PL1. The test also rejects an invalid
candidate profile without changing the live revision, checks mandatory access
control denials, and records off/detection/blocking throughput plus p99 latency:

```powershell
.\infra\host-tests\Test-StackfortWAFHyperVVm.ps1 `
  -ImageId debian-13 -VmName stackfort-debian-13
```
