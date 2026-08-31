# Phase 1 qualification

Status: Passed on 2026-08-25

This report records the exit evidence for backlog item I-003. The same
release-shaped `linux-amd64` archive was installed on a clean checkpoint of
every supported Phase 1 distribution.

## Qualified artifact

- Version: `0.0.0-dev`
- Archive: `stackfort-0.0.0-dev-linux-amd64.tar.gz`
- Archive SHA-256:
  `0c63f3e46bffc4da1f74a09bb17ea9cbe7c125d36bd376470b8fa2dab9250001`
- Installer source digest:
  `dcae4f3a062f2e1a66d10a12e6cc4dcd3dd0c913b9e9fc4576c326d8a2a9f7ae`
- Hyper-V checkpoint: `stackfort-installer-ready-20260824`
- Guest shape: 2 vCPU, 4 GiB startup RAM, system disk plus a dedicated
  project-quota disk

## Distribution matrix

| Evidence | Debian 13 | Ubuntu 26.04 LTS | Rocky Linux 10.2 |
| --- | --- | --- | --- |
| Checksum-verified first install | Pass | Pass | Pass |
| Unchanged idempotent rerun | Pass | Pass | Pass |
| File ownership and modes | Pass | Pass | Pass |
| systemd sandbox and service health | Pass | Pass | Pass |
| Persistent host firewall | nftables: Pass | nftables: Pass | firewalld: Pass |
| Mandatory access control | AppArmor: Pass | AppArmor: Pass | SELinux enforcing: Pass |
| Account identity, quotas, cgroups, and traversal denial | Pass | Pass | Pass |
| Durable account identity/filesystem/resource provisioning saga | Pass | Pass | Pass |
| Managed NGINX reconcile and injected promotion recovery | Pass | Pass | Pass |
| Static/shared/redirect domain lifecycle and removal | Pass | Pass | Pass |
| Native PHP pool isolation and bounded aggregate observability | Pass | Pass | Pass |
| Cross-account domain-operation denial | Pass | Pass | Pass |
| HTTP-01 presentation/bypass/cleanup and TLS staging | Pass | Pass | Pass |
| Private RFC 8555 lifecycle through HTTP API, worker, agent RPC, and NGINX | Pass | Pass | Pass |
| Installed panel HTTPS, SPA, and proxied API health | Pass | Pass | Pass |
| Static and control-API performance records | Pass | Pass | Pass |

The domain suite covers create, edit, shared roots, suspend, resume, remove,
reconcile, canonical redirects, customer 301/302 redirects, non-destructive
document roots, real NGINX responses, and a real local TLS handshake. The
private-ACME suite additionally covers issuance, real NGINX HTTP-01 validation,
challenge cleanup, renewal and predecessor retirement, failed-renewal
retention, non-secret certificate history, and final TLS retirement across the
production API/worker/agent-RPC path. Required `STACKFORT_QUALIFICATION` markers
and both `STACKFORT_PERFORMANCE` records are machine-checked by the installer
harness.

The account-operation bridge is additionally covered by repository and HTTP
tests for authorization, exact account scoping, and response-field omission;
the Vue contract flow follows domain creation through terminal status to the
refreshed domain list. A real Chromium run repeated pending-to-success progress,
automatic resource refresh, active/retired certificate-history expansion, and
English/German presentation against the local synthetic API fixture. Chromium
also loaded the installed release's unmocked bootstrap route and API state and
switched the actual interface from German to English. Because the fresh-host
panel certificate is intentionally self-signed, this final check used a
temporary adapter pinned to its exact SHA-256 fingerprint; no trust-store
change, production credential, or public ACME request was used.

## Defects found and closed

Qualification found six integration defects before the final matrix:

1. A cross-account remove affected zero rows but was classified as a retryable
   database outage. Domain status/removal now maps that case to an opaque,
   non-retryable not-found result, with regression coverage proving that the
   owning domain remains unchanged.
2. The installer made `/var/lib/stackfort-agent` mode `0750`, preventing NGINX
   workers from traversing to the deliberately public HTTP-01 directory. The
   root-owned parent is now `0755`; challenge files remain fixed-path,
   root-owned, bounded, non-listable through NGINX, and protected by the
   existing AppArmor/SELinux policy.
3. An absent account CPU limit was rendered as `CPUQuota=infinity`. Systemd
   accepts infinity for memory/task limits but requires the empty
   `CPUQuota=` assignment to reset a live CPU quota. A no-limit account now
   provisions successfully, with a regression test for both unlimited and
   finite mappings.
4. A successful NGINX reload could return just before the new panel listener
   accepted connections. Installed static and API probes now use a bounded,
   cancellation-aware readiness retry and retain the final diagnostic on
   timeout.
5. Rocky's enforcing SELinux policy correctly denied the NGINX worker's first
   loopback proxy connection. Stackfort now installs a local policy module and
   labels only TCP port `8080` as `stackfort_api_port_t`; the broad
   `httpd_can_network_connect` and `httpd_can_network_relay` booleans remain
   disabled and are checked by the host harness.
6. Certificate activation checkpointed 70% before invoking the standalone
   NGINX activation stages, whose first checkpoint was 10%. The persisted
   runner correctly rejected this decreasing progress. Embedded activation now
   receives a monotonic 75%-90% window, with a regression test for the exact
   composition.

The archive above was rebuilt after all corrections and the entire matrix was
then repeated successfully.

## Reproduction

Restore a clean checkpoint, then run from an elevated PowerShell session:

```powershell
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 debian-13 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 ubuntu-26.04 -VmName stackfort-ubuntu-26-04-v2 -SkipBuild -RunPhase1Suite
.\infra\host-tests\Test-StackfortInstallerHyperVVm.ps1 rocky-10 -SkipBuild -RunPhase1Suite
```

The harness is destructive by design and must run only in the disposable VM
fixtures. See the [host-test guide](../infra/host-tests/README.md), the
[security review](phase1-security-review.md), and the
[performance baseline](phase1-performance-baseline.md).
