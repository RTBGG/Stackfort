# Fresh-host installation

The existing I-002 path installs Stackfort on disposable Debian 13, Ubuntu 26.04
LTS, or Rocky Linux 10 `amd64` hosts with **already quota-prepared storage** after
the read-only preflight passes. This is not qualification of those systems'
default root filesystems.

The first native fresh-default beta is limited to **Debian 13 `amd64`**, within
the [native host profile and release gates](one-line-installation-readiness.md).
Ubuntu/Rocky native fresh-default installation is not advertised until qualified.
The maintainer has authorized an experimental beta without independent security
review, not approved any particular candidate. It is for fresh disposable test
servers only, never production or important data. Support is community-only,
without guaranteed responses or fixes; see the [security policy](../SECURITY.md).
The initial 8 GiB/100,000-free-inode check is installation headroom, not an OS
reserve. Account aggregation and platform growth can exhaust shared-root disk
space/inodes and make the test server unavailable; see the
[experimental capacity limitation](one-line-installation-readiness.md#experimental-capacity-limitation).

**Removal requires complete OS reinstallation and destroys all server data,
configuration and services.** This experimental beta has no in-place uninstaller;
passive package removal is insufficient. Review the
[removal scope](experimental-beta-removal.md) before installing.

No public release has been published yet. The current bootstrap explicitly pins
`0.1.0-beta.5`; that is a planned experimental candidate, not a claim that its
assets exist or its exact tagged installation has passed qualification. The
commands below require matching published assets from the
[release list](https://github.com/RTBGG/Stackfort/releases).
Use the [operations guide](operations.md) for first setup and ongoing checks.

## Before installation

The host must satisfy the complete [preflight contract](installer-preflight.md),
including a project-quota-enabled `/srv/hosting` filesystem for the existing
prepared-storage installation routes below. Native preparation has its own
restricted host, recovery, provenance and consent contract; do not bypass the
preflight or treat a passive carrier package as native-conversion authority.
Installation must
run as root. The installer refuses a foreign `stackfort` identity, existing
Stackfort units, active conflicting web/database services, unsafe release
files, symlinked destinations, and unmanaged configuration conflicts.

## GitHub bootstrap

Once the selected experimental release is qualified and published, the short
convenience command is:

```sh
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh |
  sudo bash
```

For a reviewable invocation, download the bootstrap first, inspect it, then
select an exact release. To pin the
bootstrap itself, replace `main` in its URL with a reviewed full commit SHA;
`STACKFORT_VERSION` pins the release payload, not the branch-hosted script:

```sh
curl --proto '=https' --tlsv1.2 -fSLo stackfort-install.sh \
  https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh
less stackfort-install.sh
sudo env STACKFORT_VERSION=0.1.0-beta.5 bash stackfort-install.sh
```

With no journal or explicit `STACKFORT_VERSION`, the current script selects
`0.1.0-beta.5`. It does not query GitHub's latest-stable channel or dynamically
choose a newer beta. An explicit version may select another published release;
an existing journal pins its original version and conflicting selections stop.
This bootstrap selection is separate from the panel's stable/beta update-check
settings and does not turn a beta into a stable release.

The bootstrap accepts semantic versions only, downloads the matching `amd64`
archive and `SHA256SUMS` from the versioned GitHub Release, verifies the exact
archive checksum, constrains archive paths to the expected bundle root, and
runs the installer contained in that bundle. It also downloads
`build-attestation.jsonl`; native onboarding verifies the exact archive against
the expected repository, workflow, tag and embedded build commit before
preparation. The public native path never accepts the laboratory origin exception.
A checksum fetched from the same release is an
integrity check, not a substitute for reviewing the bootstrap or the GitHub
trust boundary.

The dispatcher chooses the existing installation path when ordinary preflight
passes. A preflight blocker on Debian 13 may enter `onboard`, which independently
checks the full restricted native host profile; not every preflight failure is
convertible. Other operating systems or inspection errors stop. Existing native
state always goes to native validation, never to the ordinary `install --yes`
path. See [retry behavior](#journal-and-retry-behavior).

The production bootstrap has no alternate repository or transport setting.
Its explicit root-owned local-fixture mode is reserved for the project's
unreleased clean-host qualification and remains disabled unless the test flag
is deliberately set. The older
[clean-host installer matrix](../infra/host-tests/results/2026-09-01-clean-installer-matrix-hyper-v.md)
covers already prepared storage, not the new beta.4 public-native flow. Seven
shell-routing qualification markers now pass; these isolated fixtures do not
substitute for running the exact tagged archive on a clean host.

### Interactive native setup

Use a real root console or SSH session with a controlling terminal. The script
can be piped to Bash, but consent is read directly from `/dev/tty`, not piped
stdin. `onboard` has no unattended `--yes`, caller-supplied consent digest,
device override or force/reset option.

1. Review the experimental warning and authenticated exact host/release summary.
   Confirm `FRESH-DISPOSABLE NO-DATA REINSTALLATION-RISK` only for an empty,
   disposable test server, accepting that failure may require provider
   reinstallation.
2. Separately confirm `REBOOT`. The SSH session will disconnect after successful
   preparation and one-shot boot arming.
3. Save the one-use administrator setup code securely, then confirm `SAVED`.
   It appears only on the controlling terminal, not normal command output or
   installer logs. Avoid terminal recording; the installer cannot recover it.

Only then does preparation install approved prerequisites, seal the runtime and
setup commitment, arm the controlled offline quota boot and request the reboot.
Failure before completed arming does not request a reboot. Interrupted or partial
state is preserved for operator inspection, not silently reset or replayed.

After installation, open `https://<server-IP>:8443/` and use the saved code in the
setup form. Its one-hour lifetime begins at local activation after installed
payload, service-identity and health checks, not when it was displayed before
reboot. Only its digest is persisted; reboots/reruns never extend its lifetime.
See [setup delivery and recovery](native-installer-bootstrap-handoff.md).

## Manual release installation

### Passive native package (prepared-storage route)

Published releases will provide a `stackfort-release` DEB for Debian/Ubuntu and an RPM
for Rocky Linux. Download the matching package together with `SHA256SUMS`, then
verify the exact filename before installing it:

```sh
grep " ./<downloaded-package>$" SHA256SUMS | sha256sum --check --strict

# Debian 13 or Ubuntu 26.04
sudo dpkg -i ./stackfort-release_<native-version>_amd64.deb

# Rocky Linux 10
sudo rpm -Uvh ./stackfort-release-<native-version>.x86_64.rpm

sudo stackfort-install preflight
sudo stackfort-install --yes
```

The native package is a passive carrier. It lays down one immutable release at
`/usr/lib/stackfort/releases/<version>` and `/usr/sbin/stackfort-install`, but
has no maintainer scripts/scriptlets and does not configure or start Stackfort.
Removing it later removes only those packaged source files; it is not an
uninstaller and does not delete an active installation or customer data. See
[ADR 0059](adr/0059-passive-native-release-carrier.md).
This is not the native default-root onboarding route: installing the carrier
first creates an existing-package conflict with that fresh-host profile.

### Release archive (prepared-storage route)

After independently downloading the archive and `SHA256SUMS`, verify and
extract it as root so the installer's source-trust contract is preserved:

```sh
version=0.1.0
archive="stackfort-${version}-linux-amd64.tar.gz"
grep " ./${archive}$" SHA256SUMS | sha256sum --check --strict
sudo install -d -m 0755 /var/tmp/stackfort-release
sudo tar -xzf "$archive" -C /var/tmp/stackfort-release --same-owner --same-permissions
sudo "/var/tmp/stackfort-release/stackfort-${version}-linux-amd64/bin/stackfort-installer" \
  install \
  --source-dir="/var/tmp/stackfort-release/stackfort-${version}-linux-amd64" \
  --yes
```

Use `preflight --format=json` or `install ... --format=json` for automation.
Exit `0` is success, `2` is an actionable preflight blocker, and `1` is an
invocation, source, journal, stage, or verification failure.

## Journal and retry behavior

For native onboarding, a rerun selects the journal-bound release and requires
the exact completed, admitted installation with ready storage. It invokes the
sealed runtime through systemd supervision to recheck live storage, installed
payload and admission; successful completion repeats no conversion/installation,
reissues no setup code and schedules no reboot. Partial state, pending recovery,
unsafe records or mismatched release evidence stop for inspection. A journal's
presence alone never authorizes native resume or recovery.

The following ordinary installer behavior applies to the separate
already-prepared-storage path, not permission to replay interrupted conversion.

The root-only journal is
`/var/lib/stackfort-installer/install-state.json`. Each stage is saved as
`applying` before mutation and `complete` after verification. Rerun the exact
same release after a failure or interruption; completed stages are verified,
and the incomplete stage converges from its recorded state. Do not edit the
journal.

A successful second run performs verification only and returns:

```json
{
  "status": "complete",
  "changed": false,
  "alreadyInstalled": true,
  "resumed": false
}
```

The fresh-host installer intentionally refuses another source digest or
version. Installing a newer carrier package does not bypass that fence.
Use the separate [staged updater](staged-platform-updates.md) for verified
cross-version activation and transaction rollback. [Automatic update checks](update-channels-and-checks.md)
are implemented and enabled by default; automatic installation is not.
General repair, arbitrary downgrades, and uninstall are not provided by the
fresh-host command or by removing a passive carrier.

## First browser access

The successful text result prints the initial management endpoint:

```text
https://<server-address>:8443/
```

The first-start certificate is generated locally and therefore is not trusted
by public browsers. Confirm that the address belongs to the intended server
before accepting the warning. Native onboarding uses the code saved before
reboot. For the separate prepared-storage installation route, or explicit local
recovery of a lost/expired code before administrator creation, create a
short-lived one-time capability from an authenticated console or SSH session:

```sh
sudo -u stackfort -- /usr/local/bin/stackfort-api bootstrap create
```

Use the displayed value only in the bootstrap form. See
[Installed panel ingress](installed-panel-ingress.md) for the exact NGINX,
certificate, and browser boundary.
Do not automate replacement of an active capability or treat setup-code recovery
as authorization to reset an interrupted storage operation.

## Installed security boundary

After bootstrap, optionally configure a [custom panel hostname](panel-hostname.md)
with automatic Let's Encrypt HTTPS on port 443. The installer also provides
`/usr/local/sbin/stackfort-installer` for root-console management and enables
the renewal timer; without an ACME panel configuration the timer makes no CA request.

- API: locked `stackfort` user, loopback TCP 8080, private state, systemd
  sandbox, and AppArmor confinement on Debian/Ubuntu.
- Agent: root-owned binary and service, authenticated Unix socket accepting
  only the kernel-reported API UID, systemd sandbox, and no public listener.
- Panel: NGINX HTTPS on public TCP 8443, immutable assets, fixed `/api/`
  loopback proxy, and a root-only local bootstrap certificate.
- Firewall: a dedicated `inet stackfort` nftables table on Debian/Ubuntu, or
  persistent 80/443/8443 additions through firewalld on Rocky. Unrelated rules are
  preserved.
- SELinux: remains enforcing on Rocky, with persistent verified contexts for
  immutable web content, ACME HTTP-01 files, panel TLS material, managed PHP
  configuration/runtime paths, typed PHP document roots, and root-owned domain
  logs under the narrow `httpd_log_t` type, plus a local
  policy that permits NGINX to connect only to the dedicated API port type on
  TCP 8080 without broad HTTPD network booleans.
- Logs: `logrotate` is a required package. The installer owns
  `/var/log/stackfort/accounts` at root-only mode `0700` and installs the fixed
  seven-day, seven-rotation, 8-MiB-active-file policy in
  `/etc/logrotate.d/stackfort` without `copytruncate`.
- PHP: the approved native FPM package is installed while its distribution-wide
  pool remains inactive and disabled; Stackfort creates only account-scoped
  units and sockets when a PHP domain requires them.
- OCI: Podman, netavark, aardvark-dns, passt/pasta, slirp4netns, fuse-overlayfs, and
  subordinate-ID helpers are installed. `podman.socket` and `podman.service`
  are masked both as system units and in the global user configuration; the
  rootful API socket must be absent. The checksum-pinned release also installs
  its fixed Trivy scanner under `/usr/local/libexec` and root-only transaction,
  artifact, and scanner-cache directories. This prepares and scans rootless
  images but starts no container workload.
- Files: immutable executables and web assets are root-owned; API state is
  owned by `stackfort`; configuration and journal modes are verified exactly.

See [ADR 0033](adr/0033-journaled-idempotent-fresh-host-installation.md) for the
stage/recovery decision and
[ADR 0035](adr/0035-dedicated-bootstrap-panel-ingress.md) for management ingress.
