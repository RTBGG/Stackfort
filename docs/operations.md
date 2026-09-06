# Operations guide

This guide covers an installed Stackfort test host, not the development server.
Stackfort is pre-beta: use fresh, disposable Debian 13, Ubuntu 26.04 LTS, or
Rocky Linux 10 hosts on `amd64`. Do not place valuable data on them. There is
no public release or production support window yet.

## Prepare and install

1. Retain authenticated console/SSH access independent of Stackfort. Record
   the host identity and take a clean, powered-off VM checkpoint for testing.
2. Meet the [preflight requirements](installer-preflight.md): systemd/cgroup
   v2, at least 2 logical CPUs and nominal 4 GiB RAM, enforcing MAC policy,
   and a quota-enabled `/srv/hosting` filesystem with at least 5 GiB free.
   Provision and verify storage deliberately; the installer does not partition
   disks or silently enable quotas on the root filesystem.
3. Reserve ports 80/443 for customer sites and 8443 for management. Restrict
   management access to trusted operator networks using your upstream firewall
   or VPN. Keep existing SSH access when changing network rules. Never expose
   API port 8080, database sockets, cache management, or a Podman API socket.
4. Follow the [installation guide](installer-installation.md) for one exact
   release and its checksum. The examples become usable when release assets
   exist; development qualification uses the separate disposable host harness.
5. Record the version, source commit, archive digest, installation method,
   preflight report, and final installer result. Keep private journals local.

For a beta-only release, select its exact version: the convenience installer's
default `latest` lookup selects a stable release, not a prerelease.

## First access and account setup

Open `https://<server-address>:8443/`. The initial certificate is locally
generated, not publicly trusted. Confirm the server identity through your
trusted console before deciding whether to accept its browser warning; never
make certificate-verification bypass a general operating practice. Automatic
public certificate provisioning for a panel hostname is still separate work.

From the installed host's authenticated console:

```sh
sudo -u stackfort -- /usr/local/bin/stackfort-api bootstrap create --ttl=15m
```

Enter the one-time capability in the panel's bootstrap form, select English or
German, and create the first administrator. There is no default password. The
capability is not a password-reset mechanism and cannot create a second first
administrator. Log in afterwards; bootstrap does not create a login session.
Keep tokens out of URLs, screenshots, logs, and issue reports.

[TOTP enrollment/removal](totp-recovery-and-session-management.md) and single-use
recovery codes are implemented in the authenticated API; browser MFA login is
implemented, but a browser enrollment/settings flow is still missing. Do not
assume there is an enable-MFA button in the current panel. Complete and qualify
that EN/DE workflow before public beta. When testing API enrollment, save the
returned recovery codes offline and verify a recovery login before relying on
it. Factor changes revoke existing sessions. Review active sessions and revoke
unrecognized ones. There is no documented break-glass bypass for a lost password
or for losing both the authenticator and all recovery codes.

Create a package, then a hosting account with its owner. Wait for host
provisioning to complete before creating domains. Package revisions and account
assignments are explicit snapshots; do not assume editing a package silently
changes existing accounts. A CPU quota of 100% means one logical CPU, not all
server capacity. PHP, jobs, and rootless applications share the account's
resource boundary. Monthly traffic accounting/enforcement is not established
by displaying a package bandwidth limit. See [resource control](account-resource-control.md).

For customer TLS, point every requested DNS name at the intended server,
register the supported ACME account in administrator settings with explicit
terms acceptance, and request a certificate for the domain. HTTP-01 requires
port 80 reachability. A `www` alias also needs correct DNS. Customer certificates
do not replace the panel's bootstrap certificate.

Account owners should use their own workspace for domains, files, databases,
backups, and applications. Administrators need explicit account membership to
enter that account workspace. A selected shared document root intentionally
serves the same files on all domains that reference it.

## Routine checks

Review administrator host information, failed operations, update status, and
account-facing application health. The following host checks are read-only;
inspect their output locally and redact it before sharing:

```sh
sudo systemctl --no-pager --failed
sudo systemctl --no-pager status stackfort-api.service stackfort-agent.service nginx.service mariadb.service vinyl.service
curl --fail --silent --show-error http://127.0.0.1:8080/api/v1/health
curl --fail --silent --show-error http://127.0.0.1:8080/api/v1/build
findmnt --target /srv/hosting
df -h /srv/hosting /var/lib/stackfort
df -i /srv/hosting /var/lib/stackfort
timedatectl status
```

Health must report `status: ok` and `storage: ok`; it is not proof that every
hosted application, certificate, or backup is healthy. Check representative
sites separately, including a PHP/database transaction and a rootless workload
when used. Monitor free space and inodes on both hosting and system storage.
Backups, image preparation, and update staging need additional capacity beyond
the installation minimum. Keep host time synchronized for TLS, TOTP, and release
verification. Scheduled account jobs use UTC.

For a bounded local diagnosis:

```sh
sudo journalctl --no-pager -u stackfort-api.service -u stackfort-agent.service --since '-15 min' -n 100
sudo /usr/sbin/nginx -t -c /etc/nginx/stackfort/nginx.conf
```

The distribution's default NGINX config is not the active Stackfort config.
Syntax success alone does not prove Coraza worker initialization or application
health. Do not run `database check` as a supposedly read-only diagnostic: like
`database migrate`, that CLI command opens the store and may apply migrations.

### Managed files and secrets

| Location | Purpose and handling |
| --- | --- |
| `/etc/stackfort/` | Managed configuration and private panel TLS material; do not hand-edit or share as a support bundle. |
| `/etc/nginx/stackfort/` | Managed baseline and transactional site revisions; change domain intent through Stackfort. |
| `/var/lib/stackfort/stackfort.db` | Private control-plane SQLite state; live WAL files matter. |
| `/var/lib/stackfort/master.key` | Encryption key needed with state for MFA and other encrypted records; never regenerate it as a repair. |
| `/var/lib/stackfort-agent/backup-manifest.key` | Independent local backup authentication key; preserve with the protected host recovery set. |
| `/srv/hosting/` | Quota-bound account storage and separate root-only backup repository. |
| `/var/lib/stackfort-installer/` | Root-only fresh-install journal. |
| `/var/lib/stackfort-updater/` | Root-only update journal, staged releases, and SQLite rollback snapshots. |
| `/var/log/stackfort/accounts/` | Root-owned domain logs with bounded account views and fixed rotation. |

These are key locations, not a complete filesystem recovery manifest. Do not
delete journals, staging, snapshots, or backup keys to make a failing operation
retry. Native package removal is not a Stackfort uninstaller.

## WAF, cache, and runtime changes

WAF and page caching are **off by default per domain**. For a test application,
start WAF in detection-only mode, exercise normal traffic and inspect sanitized
events, then choose Blocking PL1. Detection-only does not stop attacks. Prefer
a narrow, expiring administrator exception over disabling inspection globally;
keep an explanation and test its scope. Coraza's connector is experimental,
and response-body inspection is not enabled.

PHP domains can opt into the closed Vinyl `respect_origin` or `wordpress`
presets. Cookie/authorization-bearing and sensitive-path traffic bypasses the
cache. Confirm this for the actual application, including login, cart, account,
and tenant separation. Purge the intended domain/path after a content change
when needed. WAF checks also run on cache hits when WAF is enabled.

NGINX FastCGI cache is a **benchmark-proven future preset**, not currently a
selectable production cache. PageSpeed/Cyclone is evaluation-only and is not
installed. See [benchmark scope and results](benchmarks.md); do not paste
benchmark configuration into managed production files.

Keep NGINX, its Coraza module, and their native package revisions together.
Do not independently replace the managed ABI-locked packages or turn off MAC
enforcement to work around drift. Plan OS/package maintenance and reboots in a
disposable clone first; Stackfort's updater is not a general OS upgrade tool.
Rootless applications must pass the fixed image scanner and health gates;
scanner failure is not permission to skip scanning or enable privileged mode.

## Updates and recovery

Automatic **checks** are on by default every six hours on the stable channel.
Installation requires an explicit, recently authenticated administrator action.
Select beta deliberately if testing beta releases. A development version or
unpublished rehearsal cannot establish a verified published-release upgrade.

Before activating a candidate, review release notes and support status, reserve
a maintenance window, verify backups/recovery access and staging capacity, and
avoid concurrent host changes. The transaction can interrupt the panel and
hosted services. Use **Administration → Updates → Stage and install update**;
follow the durable result after reconnecting.

```sh
sudo stackfort-updater status
```

- `complete`: verify the expected build and representative hosted sites.
- `rolled_back`: the update did not install successfully; review the failure
  and verify the restored previous release before scheduling a retry.
- Interrupted/non-terminal: correct the underlying host issue and invoke the
  **same target version** for fail-closed recovery, as documented in
  [staged updates](staged-platform-updates.md). Do not start a different update
  or edit its journal.
- `rollback_failed`: stop additional changes, retain evidence and use console
  access for diagnosis. Do not force forward activation or restore SQLite alone
  underneath running services.

The updater's rollback is a transaction safety mechanism, not a general
downgrade UI or a disaster-recovery backup.

## Backups and disaster recovery

| Facility | What it covers | What it does not cover |
| --- | --- | --- |
| Account-files backup | Visible account-root files; internal upload/operation/trash trees are excluded and preserved on restore | MariaDB data/users, TLS, panel state, generated host state, a complete OCI deployment |
| Document-root backup | Files in one selected account-relative directory | Other roots and services; domains sharing that root all see the restored files |
| Updater SQLite snapshot | Consistent panel state for rollback of that update | Customer databases/files, whole-host recovery, independent off-host retention |

Create a file backup in the account's backup manager, verify it, and download
its portable `payload.tar.gz` to separate protected storage. Downloaded payloads
are **not encrypted** by Stackfort. An upload is parsed and signed with the
receiving host's local key; do not upload or distribute a host manifest/key.
Only the owner can restore, after recent authentication and exact backup-ID
confirmation. A restore **replaces** the selected visible scope, it does not
merge it. Pause relevant application writes and retain the current content
before restoration. Test the restored files/application afterwards.

Limits are 4 GiB payload/content, 10,000 entries, 64 levels, and 30 minutes per
backup/restore. Package backup capacity is separate (20 GiB when unspecified),
with at most 256 backups. Retention is manual; permanent backup deletion is not
recoverable through trash. Scheduled, remote encrypted, and database backups
are not implemented. Account-files multi-tree activation does not yet have a
durable power-loss recovery journal. See the
[backup contract](local-file-backup-foundation.md).

For disposable VM recovery, retain an independent, powered-off whole-VM backup
covering **all** attached disks and restore it first to an isolated network.
Keep the matching release, configuration, filesystem metadata, SQL data, panel
state, and secret keys together. A checkpoint on the same storage is not an
off-host backup; neither a file-only export nor a copy of the live SQLite `.db`
alone is a full recovery set. The live `-wal` can contain committed data.

There is no supported one-command bare-metal/control-plane disaster restore.
Do not improvise by copying individual state files over a running panel or
generating replacement encryption keys. Rehearse complete VM recovery before
relying on test data, and keep this limitation in the beta readiness decision.
See [persistence](persistence.md) and [troubleshooting](troubleshooting.md).
