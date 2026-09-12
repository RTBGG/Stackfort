# Stackfort

**Secure hosting. Simple operations.**

[![CI](https://github.com/RTBGG/Stackfort/actions/workflows/ci.yml/badge.svg)](https://github.com/RTBGG/Stackfort/actions/workflows/ci.yml)
[![Security](https://github.com/RTBGG/Stackfort/actions/workflows/security.yml/badge.svg)](https://github.com/RTBGG/Stackfort/actions/workflows/security.yml)
[![License: AGPL-3.0-or-later](https://img.shields.io/badge/license-AGPL--3.0--or--later-blue.svg)](LICENSE)

Stackfort is a free and open-source server control plane for shared web hosting
and containerized applications. It combines approachable account management,
focused server administration, and secure application hosting without making
the web interface itself a privileged system process.

> [!WARNING]
> Stackfort is under active development. Phase 6 is in progress, and the
> project is not ready for production servers or valuable data. No public
> release is available yet; installation examples require published assets.
> The first native beta targets disposable Debian 13 test servers only.
> No independent security review has been performed; support is community-only.
> No in-place uninstaller is available. Removing the experimental beta requires
> complete OS reinstallation, destroying all server data, configuration and services.

[Documentation](docs/README.md) · [Operations](docs/operations.md) ·
[Roadmap](docs/roadmap.md) · [Security policy](SECURITY.md) ·
[Contributing](CONTRIBUTING.md)

## Install

Planned first native beta: **fresh, disposable Debian 13 `amd64` test servers**
within the [qualified host profile](docs/one-line-installation-readiness.md#scope).
Public native installation remains blocked until its release gates pass:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh | sudo bash
```

[Inspect the installer](https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh)
before running it as root. HTTPS-only transport and TLS 1.2 or newer are required
by this command. The current script explicitly selects **`0.1.0-beta.5`**, not
latest stable. **No public release exists yet:** its assets and exact tagged
installation qualification are still pending. Native setup requires an interactive
root console/SSH terminal, explicit reboot consent and saving the setup code.
See the
[installation guide](docs/installer-installation.md) for prerequisites and options.
Existing tests on already quota-prepared Debian, Ubuntu and Rocky storage are a
separate installation path, not qualification of their default root filesystems.
Disk/inode exhaustion can still make the shared-root test server unavailable;
there is no durable OS capacity reserve yet. See the
[experimental limits](docs/one-line-installation-readiness.md#experimental-capacity-limitation).
Read the [removal scope](docs/experimental-beta-removal.md) before installation;
removing the release package does not remove the active platform.

After installation, [configure a panel subdomain](docs/panel-hostname.md) such as
`https://panel.example.com/`, with automatic Let's Encrypt issuance and renewal.
The initial IP-based HTTPS endpoint on port 8443 remains available as a fallback.

## At a glance

| Area | Current state |
| --- | --- |
| Control plane | Unprivileged Go API plus a small, allowlisted Linux host agent |
| Web hosting | Static sites, isolated PHP-FPM pools, TLS, redirects, and domain lifecycle |
| Security | Role and account isolation, MFA, audit chain, Coraza WAF, and hardened services |
| Databases | Tenant-scoped MariaDB, guided lifecycle, credential rotation, and phpMyAdmin sign-on |
| File management | Browse, upload, download, copy, move, trash, archives, and local file backups |
| Installation | Quota-prepared host installer and passive DEB/RPM carriers; Debian 13 native beta qualification in progress |
| Containers | Rootless Podman, scanned images, private resources, health-gated Quadlets, routing, and three-OS isolation qualification |

## Design goals

- Fast static and PHP hosting with NGINX and isolated PHP-FPM pools.
- Clear separation between administrators, hosting accounts, and host privileges.
- Enforceable CPU, memory, process, storage, and I/O limits.
- Validated, atomic configuration changes with rollback and durable recovery.
- A simple English and German interface, with English as the source language.
- Reproducible installation and qualification on every supported distribution.

## Architecture

The browser talks to an unprivileged control API. Privileged host changes go
through a small, typed, allowlisted agent over a kernel-authenticated Unix
socket.

<details>
<summary><strong>Request path</strong></summary>

```text
Client
  -> NGINX edge (TLS, redirects, rate limits, Coraza WAF)
      -> static file
      -> NGINX origin -> PHP-FPM account pool
      -> optional cache -> NGINX origin -> PHP-FPM account pool
      -> rootless OCI application over a loopback-only upstream
```

Static files are served directly by NGINX. Full-page caching is optional and
personalized applications bypass it by default.

</details>

<details>
<summary><strong>Technology stack</strong></summary>

| Area | Technology |
| --- | --- |
| Control API and host agent | Go |
| Web interface | Vue 3, TypeScript, Vite |
| Panel state | SQLite in WAL mode |
| Edge and origin server | NGINX |
| PHP runtime | One PHP-FPM pool per hosting account |
| Optional page cache | Per-domain Vinyl Cache 9.x or NGINX FastCGI cache; disabled by default |
| Web application firewall | Coraza 3, coraza-nginx, and OWASP Core Rule Set |
| Managed SQL service | MariaDB |
| Container runtime | Rootless Podman with health-gated systemd Quadlets |
| Resource control | systemd, cgroup v2, and filesystem project quotas |

</details>

## Current capabilities

<details>
<summary><strong>Identity, accounts, and isolation</strong></summary>

- Single-use first-administrator bootstrap with no default password.
- Argon2id login, persistent rate limits, strict cookies, CSRF protection,
  session rotation, and server-side expiry.
- Browser TOTP setup, replacement, removal and MFA login, with one-time
  recovery-code display and identity-wide session revocation.
- Deny-by-default platform and account authorization with freshness checks for
  sensitive operations.
- Stable per-account Unix identities, project quotas, systemd slices, and
  descriptor-relative filesystem access without symlink traversal.
- Hash-chained audit records and durable, replay-safe background operations.

</details>

<details>
<summary><strong>Container foundation</strong></summary>

- Tenant-owned drafts accept only digest-pinned images or normalized
  Containerfile sources, one internal port, and a bounded health check.
- The installer provides Podman/netavark rootless dependencies while masking
  rootful and user engine API units.
- Every account receives deterministic, non-overlapping subordinate UID/GID
  mappings plus symlink-resistant storage, runtime, and Quadlet paths.
- Digest pulls and closed Containerfile builds now run rootlessly under fixed
  CPU, memory, storage-output, process, network, time, and log bounds.
- Checksum-pinned Trivy scans the OCI archive before an immutable deployed
  digest can be recorded; HIGH/CRITICAL findings fail closed.
- Account-private rootless bridge networks, envelope-encrypted environment
  references, and descriptor-verified quota-bound volumes are implemented.
- Fixed rootless Quadlets publish only stable loopback ports, load transient
  Podman secrets through stdin, health-gate activation, expose bounded logs,
  and support replay-safe deploy, suspend, resume, rollback, and removal.
- Active OCI domain targets use fixed NGINX upstreams and the existing atomic
  validate/reload/probe/rollback pipeline.
- PHP, scheduled jobs, and the delegated rootless user manager share one
  account cgroup boundary. Exhaustion, hostile policy, cross-account isolation,
  private ingress, and reboot recovery pass on all three supported guests.

[Application schema](docs/oci-application-foundation.md) ·
[Rootless runtime](docs/rootless-oci-runtime.md) ·
[Image preparation](docs/oci-image-preparation.md) ·
[Private resources](docs/oci-private-resources.md) ·
[Deployment lifecycle](docs/oci-deployment-lifecycle.md) ·
[Phase 5 qualification](infra/host-tests/results/2026-09-01-oci-phase5-exit-matrix-hyper-v.md)

</details>

<details>
<summary><strong>Hosting, TLS, WAF, and cache</strong></summary>

- Static-domain lifecycle with safe shared roots, suspend/resume, removal, and
  cross-account enforcement.
- Isolated PHP-FPM services with account sockets, package-selected PHP versions,
  and bounded pool-health metrics.
- ACME registration, certificate ordering, renewal, encrypted keys, and
  transactional HTTPS activation.
- Validated apex, `www`, wildcard, and 301/302 redirect policies.
- Per-domain Coraza modes: off, detection-only, or OWASP CRS PL1 blocking.
- Sanitized WAF events and expiring, exact-path CRS exceptions without raw
  SecLang input.
- Per-domain logs, bounded account views, redaction, and size-bounded rotation.
- Package-limited Shell/PHP scheduled jobs using isolated systemd timers.

Coraza more than doubled WAF-enabled throughput compared with the earlier
ModSecurity workload on every supported guest. Vinyl Cache remains opt-in:
NGINX FastCGI cache is the recommended direction for a future managed preset because
it delivered substantially higher PHP-cache throughput in qualification. A
separate mod_pagespeed 1.15/Cyclone evaluation improved the tiny fixture's
client resource shape, but was slower than both full-page caches and cannot be
a free cross-platform dependency under the evaluated license/package matrix.
These are small local-VM comparisons, not production capacity claims; see the
[methodology and limitations](docs/benchmarks.md).

[Coraza benchmark](infra/host-tests/results/2026-08-31-coraza-runtime-hyper-v.md) ·
[Cache design](docs/cache-foundation.md) ·
[Cache benchmark](infra/host-tests/results/2026-08-31-vinyl-cache-hyper-v.md) ·
[PageSpeed evaluation](infra/host-tests/results/2026-09-01-mod-pagespeed-nginx-evaluation.md)

</details>

<details>
<summary><strong>Databases, files, and backups</strong></summary>

- Native MariaDB installation/adoption with tenant-prefixed databases and
  users, least-privilege grants, and guided creation and deletion.
- Encrypted one-time credentials, failure-safe password rotation, and secure
  30-second phpMyAdmin sign-on handoffs.
- Bounded file browsing and constant-memory, Range-capable downloads.
- Resumable staged uploads, safe creation, atomic move/copy, reversible trash,
  and quota-aware failure reporting.
- Bounded ZIP and tar.gz creation plus staged hostile-input extraction.
- Root-owned local file backups with authenticated manifests, full verification,
  and staged document-root or visible-account restore.

File backups exclude databases, TLS, and control-plane state. They are not a
complete account or server recovery bundle; see [backup coverage](docs/operations.md#backups-and-disaster-recovery).

</details>

<details>
<summary><strong>Installation, recovery, and qualification</strong></summary>

- Read-only installer preflight with actionable blockers and text/JSON output.
- Release-bound fresh-host installer with immutable payload checks, hardened
  systemd units, MAC/firewall integration, and interruption recovery.
- Reproducible, scriptlet-free DEB/RPM release carriers that preserve the
  journaled installer as the only host-mutation path.
- Crash-recoverable NGINX revisions with syntax validation, atomic activation,
  health checks, rollback, and replay-safe reconciliation.
- Immutable-reference CI, CodeQL, secret and vulnerability scanning,
  deterministic archives, SBOMs, and build attestations.
- Default-on, ETag-aware stable/beta GitHub Release discovery that advertises
  only complete immutable releases and never installs them automatically.
- Explicit administrator-triggered updates with exact-tag GitHub provenance
  verification, root-owned staging/journal, database snapshot, health gate, and
  exact prior-release rollback; automatic installation remains disabled.
- One identical release archive qualified on Debian 13, Ubuntu 26.04, and Rocky
  Linux 10 using disposable Hyper-V guests.
- Exhaustive predecessor/OS/recovery upgrade gates with artifact-bound evidence;
  the [current nine-cell rehearsal](infra/host-tests/results/2026-09-05-upgrade-matrix-hyper-v.md)
  is not published-release evidence.

See the [Phase 1 qualification](docs/phase1-qualification.md),
[security review](docs/phase1-security-review.md), and
[performance baseline](docs/phase1-performance-baseline.md).

</details>

## Documentation

| Topic | Start here |
| --- | --- |
| Install and operate | [Installation](docs/installer-installation.md), [operations](docs/operations.md), [troubleshooting](docs/troubleshooting.md) |
| Features and design | [Documentation index](docs/README.md), [architecture](docs/architecture.md), [security model](docs/security.md) |
| Updates and releases | [Update checks](docs/update-channels-and-checks.md), [staged updates](docs/staged-platform-updates.md), [release checklist](docs/release-checklist.md) |
| Performance evidence | [Methodology, results, and reproduction](docs/benchmarks.md) |
| Contribute or report | [Development](DEVELOPMENT.md), [contributing](CONTRIBUTING.md), [private security reporting](SECURITY.md) |

<details>
<summary><strong>Repository layout</strong></summary>

```text
cmd/stackfort-api       Unprivileged HTTP API and orchestration
cmd/stackfort-agent     Minimal privileged host service
web                     Vue interface and translations
docs                    Product, architecture, security, and operations docs
docs/adr                Architecture decision records
packaging               Installer and DEB/RPM packaging
infra/host-tests        Supported-OS qualification and benchmark harness
tests                   Integration, end-to-end, and performance suites
```

</details>

## Development

Requirements and all verification commands are in [DEVELOPMENT.md](DEVELOPMENT.md).
For a local Linux/macOS start, use two terminals (Windows PowerShell examples
are in the development guide):

```sh
# Terminal 1
STACKFORT_STATE_PATH="$PWD/work/stackfort.db" go run ./cmd/stackfort-api
```

```sh
# Terminal 2
cd web
npm ci
npm run dev
```

The API listens on `127.0.0.1:8080` and the web development server on
`127.0.0.1:5173` by default. This starts the UI/API for development; real browser
login needs an HTTPS front end because session cookies are always `Secure`.
The Linux-only host agent should use a development socket outside production paths.

## Safety

Do not run development installers or unreleased builds on a server containing
valuable data. Early releases support fresh, disposable systems only.

## License

Stackfort is licensed under the GNU Affero General Public License v3.0 or later
(`AGPL-3.0-or-later`). See [LICENSE](LICENSE) and [COPYRIGHT.md](COPYRIGHT.md).
