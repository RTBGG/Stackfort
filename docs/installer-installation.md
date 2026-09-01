# Fresh-host installation

I-002 installs Stackfort on a disposable fresh Debian 13, Ubuntu 26.04 LTS, or
Rocky Linux 10 `amd64` host after the read-only preflight passes. The project is
still pre-beta: do not use an unreleased build on a server containing valuable
data.

## Before installation

The host must satisfy the complete [preflight contract](installer-preflight.md),
including a project-quota-enabled `/srv/hosting` filesystem. Installation must
run as root. The installer refuses a foreign `stackfort` identity, existing
Stackfort units, active conflicting web/database services, unsafe release
files, symlinked destinations, and unmanaged configuration conflicts.

## GitHub bootstrap

The short convenience command is:

```sh
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh |
  sudo bash
```

For a reviewable and version-pinned invocation, download the bootstrap first,
inspect it, then select an exact release:

```sh
curl --proto '=https' --tlsv1.2 -fSLo stackfort-install.sh \
  https://raw.githubusercontent.com/RTBGG/stackfort/main/packaging/installer/install.sh
less stackfort-install.sh
sudo env STACKFORT_VERSION=0.1.0 bash stackfort-install.sh
```

The bootstrap accepts semantic versions only, downloads the matching `amd64`
archive and `SHA256SUMS` from the versioned GitHub Release, verifies the exact
archive checksum, constrains archive paths to the expected bundle root, and
runs the installer contained in that bundle. GitHub also records build
attestations for release assets. A checksum fetched from the same release is an
integrity check, not a substitute for reviewing the bootstrap or the GitHub
trust boundary.

If a journal already exists, the bootstrap resumes or verifies that journal's
version instead of silently switching to the newest release.

The production bootstrap has no alternate repository or transport setting.
Its explicit root-owned local-fixture mode is reserved for the project's
unreleased clean-host qualification and remains disabled unless the test flag
is deliberately set. The complete nine-cell evidence is recorded in the
[clean-host installer matrix](../infra/host-tests/results/2026-09-01-clean-installer-matrix-hyper-v.md).

## Manual release installation

### Native release package

GitHub Releases provide a `stackfort-release` DEB for Debian/Ubuntu and an RPM
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

### Release archive

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
Functional updates, automatic update checks, repair, rollback across versions,
and uninstall are separate roadmap work.

## First browser access

The successful text result prints the initial management endpoint:

```text
https://<server-address>:8443/
```

The first-start certificate is generated locally and therefore is not trusted
by public browsers. Confirm that the address belongs to the intended server
before accepting the warning. Then create the short-lived one-time capability
from an authenticated console or SSH session:

```sh
sudo -u stackfort -- /usr/local/bin/stackfort-api bootstrap create
```

Use the displayed value only in the bootstrap form. See
[Installed panel ingress](installed-panel-ingress.md) for the exact NGINX,
certificate, and browser boundary.

## Installed security boundary

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
