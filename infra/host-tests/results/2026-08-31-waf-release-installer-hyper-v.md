# WAF release-manifest and installer qualification on Hyper-V — 2026-08-31

> Historical installer-gate evidence: the final runtime gate subsequently
> rebuilt Ubuntu's connector against the exact patched distribution source,
> moved CRS compilation to shared HTTP scope, and rebuilt all three packages
> under the common updated source lock. Current archive/package hashes and the
> complete runtime matrix are in the
> [final runtime qualification](2026-08-31-waf-runtime-hyper-v.md).

## Scope

This result qualifies the K-010 release-consumption gate on clean Debian 13,
Ubuntu 26.04, and Rocky Linux 10 amd64 checkpoints. One release-shaped archive
carried the three native `stackfort-waf` packages and a strict embedded
`RELEASE-MANIFEST.json`. The privileged production installer selected only the
matching target, rehashed it, installed it in a journaled stage, and verified
the installed package database, complete rooted qualification inventory,
component and NGINX package versions, private library symlinks and RUNPATH,
ELF architecture/SONAME/imports, runtime ownership, and an isolated NGINX
module load.

The local qualification archive is not a published or attested release. The
release workflow builds the same closed three-target package matrix before its
reproducibility check, SBOM generation, and GitHub artifact attestation of the
final archives.

## Qualified artifacts

| Artifact | SHA-256 | Bytes |
| --- | --- | ---: |
| `stackfort-0.0.0-dev-linux-amd64.tar.gz` | `53db74ace87f64e26502b9fa0f7dbd394cef98a23b32e08ff35d75c90c3cc95a` | 33,364,931 |
| Debian 13 DEB | `82f6fa20da397c8ea2b69b501f1209a9d6388c6925469693353ff872848f3776` | 786,432 |
| Ubuntu 26.04 DEB | `d7dfa47ee0430215222b4f29968353bc1d2b3145a4c76daccf94113f9e1749ba` | 815,668 |
| Rocky Linux 10 RPM | `617188a45a38903e87e0b602601f5d56edf53063518eb065bcc0ca951e210cea` | 901,097 |

All three final installs reported source-tree digest
`7f4d1fb0485b2fd11d8940e259ebdb8e93eab53c9e18fc2e4d2f300db76611d66`.
The manifest assembler rejected incomplete or mismatched target records in
unit tests and copied only files whose size and SHA-256 matched their strict
native records. The installer source inspection revalidated every embedded
artifact before creating installation state.
An incomplete development manifest was also rejected during source inspection,
before journal creation, payload inspection, or any host mutation.

## Guest results

| Guest | Fresh install | WAF stage | Native drift check | NGINX/module check | Second run |
| --- | --- | --- | --- | --- | --- |
| Debian 13 | Passed | Passed | `dpkg --verify` clean | Passed | Unchanged journal; no-op |
| Ubuntu 26.04 | Passed | Passed | `dpkg --verify` clean | Passed | Unchanged journal; no-op |
| Rocky Linux 10 | Passed | Passed | `rpm -V` clean | Passed | Unchanged journal; no-op |

The independent host harness also rechecked root-owned module and loader
metadata, root/worker cache-directory modes, active/enabled services, systemd
sandboxing, firewall persistence, AppArmor or enforcing SELinux state, panel
health, and the existing phpMyAdmin boundary. The installer journal recorded
the new `waf-native-package` stage exactly once and the second invocation
returned `alreadyInstalled=true` and `changed=false` without modifying it.

## Failure and rollback evidence

- Linux fault-injection tests forced both a partial DEB command failure and a
  post-install verification failure. A newly visible package was removed, and
  the original error remained observable; the tests also assert the bounded
  APT lock-wait arguments.
- An initial Debian/Ubuntu qualification run exposed an incorrect expectation
  for the first YAJL ABI symlink. Verification failed closed and both new DEBs
  were removed before any later installer stage ran.
- The initial RPM qualification exposed an `rpmbuild` post-processing change
  to an already-qualified ELF module. The installed inventory check failed and
  removed the RPM. The wrapper now disables that second stripping pass and
  extracts the finished package to compare all qualified bytes, file modes,
  and private-library symlinks before emitting its release record.
- A Debian retry encountered an independent APT frontend lock immediately
  after base-package setup. Native DEB install/removal now uses the existing
  120-second APT lock-wait contract; the final clean qualification passed.
- A conflicting pre-existing `stackfort-waf` version remains a fail-closed
  precondition and is never replaced or removed by this stage.

## Verification performed

- `go test ./...` and `go vet ./...` passed on the Windows workspace.
- The Linux `internal/installapply` test binary passed the WAF install,
  verification-failure rollback, and ABI-symlink tests on a supported guest.
- The release and WAF shell scripts passed syntax checks; the earlier complete
  WAF script set also passed ShellCheck. The workflow passed actionlint.
- The complete amd64 release was assembled from the three strict native
  records and its extracted package hashes matched the manifest.
- The production installer harness passed on all three clean Hyper-V guests.

## Gate status at the time

The installed benign/malicious corpus, mode transitions, TLS, static/PHP,
redirect, ACME, failed-reload recovery, mandatory-access-control, and
performance matrix subsequently passed. Sanitized event views and
administrator exceptions remain K-011 and K-012.
