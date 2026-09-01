# Clean-host installer matrix — Hyper-V — 2026-09-01

Phase 6's GitHub bootstrap, manual release archive, and passive native package
routes passed the same closed clean-host qualification on all three supported
`amd64` distributions.

## Matrix

Each cell started by restoring the immutable
`stackfort-installer-ready-20260824` checkpoint. No installed state was reused
between methods.

| Guest | Manual archive | GitHub bootstrap | Native package |
| --- | --- | --- | --- |
| Debian 13 | passed | passed | DEB passed |
| Ubuntu 26.04 LTS | passed | passed | DEB passed |
| Rocky Linux 10 | passed | passed | RPM passed |

All nine cells proved:

- a successful read-only JSON preflight before mutation;
- a complete initial journaled installation and exact release metadata;
- an unchanged journal plus `alreadyInstalled=true` on the second invocation;
- root ownership, modes, systemd sandbox properties, and live service health;
- persistent nftables/firewalld and enforcing AppArmor/SELinux policy;
- exact Coraza WAF and Vinyl native package records without package drift; and
- live NGINX configuration, panel, API, and phpMyAdmin sign-on checks.

The result contained nine `ReadOnlyPreflight`, `FirstInstall`, `NoOpRerun`,
`ServiceHealth`, `WAFNativePackage`, and `VinylNativePackage` passes. The matrix
runner returned exit status zero and stopped every guest.

## Qualified artifacts

| Artifact | SHA-256 |
| --- | --- |
| `stackfort-0.0.0-dev-linux-amd64.tar.gz` | `4329caa77c6c11db5051f68d77bb76ee44bfb52a8fd9493b43a4e011f370093a` |
| `stackfort-release_0.0.0~dev-1_amd64.deb` | `6ee8ed8a63472513505070d39207a982e95746b6b4112b30a6893f60fe30c250` |
| `stackfort-release-0.0.0~dev-1.sf1.x86_64.rpm` | `eaaaab174d82be0acd04b2aa94f697adc01cb6df75df697b72e84b36064d1768` |

These are development qualification artifacts, not published releases.

## Bootstrap trust boundary

Production `install.sh` remains hard-locked to `RTBGG/stackfort` GitHub
Releases over HTTPS with TLS 1.2 or newer. The matrix used the exact production
bootstrap with an explicit root-only local release fixture so an unreleased
build could be tested without weakening or simulating the download and archive
verification logic. That seam requires `STACKFORT_BOOTSTRAP_TESTING=1`, rejects
unsafe fixture ownership or modes, and is unavailable by default.

The automated bootstrap corpus also proves that fixture mode is rejected
without its explicit testing flag, unsafe writable fixtures fail closed,
duplicate checksums fail closed, symbolic-link archive members fail closed,
and the production repository, release URL, HTTPS, and TLS policy remain fixed.

## Defects closed during qualification

The matrix found and closed cross-platform release issues before publication:

- the host harness now selects the Coraza package matrix rather than the retired
  ModSecurity fixture path;
- complete manifests with nested artifact architecture fields are parsed
  correctly by the native carrier builder;
- archives built from a Windows checkout retain normalized Linux modes and the
  four required executable payloads;
- `SHA256SUMS` uses the portable text format expected by strict verification;
- Vinyl's fixed configuration compiler is allowlisted, and its large successful
  generated output no longer exceeds the command-capture boundary; and
- archive extraction rejects links and special files and does not import owner
  or permission metadata from the transport archive.

## Reproduction

From an elevated PowerShell session with the three clean checkpoints and the
complete WAF, Vinyl, and native carrier records prepared:

```powershell
.\infra\host-tests\Test-StackfortCleanInstallersHyperV.ps1 -SkipBuild
```

Omit `-SkipBuild` to assemble the development release archive first. The runner
restores the clean checkpoint before every method and returns guests it started
to `Off` even if a cell fails.

## Scope

This record closes the second Phase 6 roadmap item. Published stable/beta
channels, automatic update checks, migration, health-gated activation,
rollback, and prior-release upgrade matrices remain separate Phase 6 work.
