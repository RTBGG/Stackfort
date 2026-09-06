# Troubleshooting

Start with the [operations guide](operations.md). Keep console/SSH access,
collect the stable error code and operation ID, and diagnose before changing
state. Commands below inspect the host; they do not repair it automatically.
Do not disable security enforcement, erase journals, or change managed file
ownership to suppress an error.

## Installation is blocked

Run the exact release's `stackfort-installer preflight --format=json` before
installation, or `sudo stackfort-install preflight --format=json` when using
the native carrier. Exit `2` means a host requirement failed; use that check's
`reasonCode` and remediation. Preflight describes a **fresh** host: conflicts
after installation are not a reason to delete the installed service.

Check the filesystem and service requirements in the
[preflight contract](installer-preflight.md). Missing project quotas need
deliberate filesystem provisioning, not a guessed `fstab` edit. Existing
hosting software is not automatically adopted or migrated.

If the GitHub bootstrap reports a missing release, check the
[release list](https://github.com/RTBGG/Stackfort/releases). No public release
exists yet. Once beta assets exist, select their exact version; `latest` does
not select a prerelease. Do not enable local-fixture flags to bypass production
release checks. If installation was interrupted, rerun the **same source** as
described in [installer recovery](installer-installation.md#journal-and-retry-behavior).

## Panel unavailable or login does not persist

1. Confirm the intended server and `https://<server-address>:8443/`, not plain
   HTTP or customer port 443. A local bootstrap certificate is initially
   untrusted; a changed certificate/host identity needs investigation.
2. Check service status and the local health endpoint from the
   [routine checks](operations.md#routine-checks). Local API success with
   external failure points to the NGINX/network/TLS boundary, not necessarily
   the database.
3. Inspect the listener and managed configuration:

   ```sh
   sudo ss -ltnp
   sudo /usr/sbin/nginx -t -c /etc/nginx/stackfort/nginx.conf
   sudo journalctl --no-pager -u nginx.service --since '-15 min' -n 100
   ```

4. Check upstream firewall/NAT/VPN access to 8443 without exposing loopback
   services. Stackfort preserves unrelated firewall rules, which can still
   deny ingress.

Browser sessions require same-origin HTTPS and `Secure`, host-only cookies.
Plain-HTTP development UI is not an end-to-end login environment. Do not weaken
cookie flags or accept arbitrary forwarded headers. API authentication limits
currently see the direct NGINX peer; a shared per-source bucket can affect
multiple users. Follow retry timing rather than repeatedly submitting logins.

## Bootstrap, MFA, or recent authentication fails

- Bootstrap tokens expire (15 minutes by default) and are single-use. Before
  the first administrator exists, an authenticated operator may replace an
  outstanding token using `bootstrap create --replace` as the `stackfort`
  service identity. Never publish the output.
- An existing administrator cannot be reset by bootstrapping again. Do not
  delete identities or edit SQLite as a recovery shortcut.
- Check host and authenticator time for TOTP failures. A previously accepted
  time-step code cannot be replayed. Use one saved recovery code if necessary;
  used recovery codes cannot be reused.
- Password and MFA changes revoke sessions. Log in again before retrying an
  operation that requires authentication within the last five minutes.
- Missing or mismatched `master.key` is a recovery-set problem. Preserve the
  original key and state; generating another key cannot decrypt old records.

See [bootstrap](administrator-bootstrap.md) and
[MFA/recovery](totp-recovery-and-session-management.md). Never attach secrets
or a whole database to a support report.

## Domain, TLS, WAF, or cache behaves unexpectedly

Unknown HTTP hosts are intentionally rejected with 444; unknown customer HTTPS
SNI is rejected during TLS negotiation. Test the configured domain, not only
the server IP. Wait for its durable operation to finish and check DNS for all
names, including `www`. HTTP-01 requires public port 80 and does not support
arbitrary DNS-provider configuration. Customer TLS and panel TLS are separate.

For a failed activation, keep the operation ID and inspect sanitized account
logs and administrator operations. The previous configuration may have been
restored automatically. NGINX syntax success is insufficient for Coraza: rules
initialize in the worker after fork. Do not replace the known-good revision
manually or relax AppArmor/SELinux to make a candidate load.

For a legitimate request blocked with 403, confirm its WAF event and rule scope.
An administrator can apply a narrow, expiring exception when justified. Keep
an attack regression test; switching off all WAF inspection changes protection.

A cache `BYPASS` is expected for cookies, authorization, bodies, and sensitive
paths. A response with `Set-Cookie`, `private`, `no-cache`, or `no-store` must
not be stored. Confirm the domain is PHP, has an enabled preset, and that the
origin permits caching before treating a miss as a fault. A stale public page
may need an account-authorized domain/path purge. Do not cache logged-in
responses to improve benchmark figures. See [WAF](waf-foundation.md) and
[cache](cache-foundation.md).

## Files, backups, databases, or containers fail

| Symptom | First checks |
| --- | --- |
| Upload, extraction, or copy refused | Account quota, host free bytes/inodes, transfer bounds, ownership, links/special files, and available staging space. Do not grant broader permissions to bypass a rejection. |
| Backup import/restore refused | Correct account/scope, owner authorization, recent login, exact confirmation, full integrity verification, package repository capacity, and the [backup limits](local-file-backup-foundation.md). Never replace the HMAC key. |
| Files restored but application data is missing | File backups exclude MariaDB and generated service state; use the separately protected application/host recovery set. |
| phpMyAdmin handoff expired | Start a new handoff from the account database manager. It is a 30-second, single-use flow; never copy a signon token into a URL. |
| Database deletion refused | Remove managed grants before a database user; confirm exact ownership and the destructive action. Do not delete native objects behind Stackfort's operation history. |
| Container prepare/deploy blocked | Approved digest/source revision, scanner availability/findings, host rootless readiness, private resources, and application health. HIGH/CRITICAL findings and scanner errors fail closed. |
| PHP/job/OCI workload runs out of resources | The account's effective package and shared CPU/memory/PIDs/quota boundary. Workloads aggregate; a container is not entitled to an independent extra allowance. |

## Update failed or was interrupted

```sh
sudo stackfort-updater status
```

Use the target version from that result to inspect the exact instance, for
example (replace this illustrative version):

```sh
sudo systemctl --no-pager status 'stackfort-update@1.2.3.service'
sudo journalctl --no-pager -u 'stackfort-update@1.2.3.service' --since '-30 min' -n 100
```

No eligible candidate can mean the selected channel has no newer complete,
immutable release; it does not necessarily mean a network error. Check the
displayed error and retry schedule. Validate DNS, outbound HTTPS, host time,
disk space, and native-package drift. Do not add a GitHub token or skip
attestation verification as a workaround.

`rolled_back` is an unsuccessful update with recovered prior state, not an
installed new version. For an interrupted transaction, fix the host issue and
invoke the same target to recover. For `rollback_failed`, stop further mutation
and investigate from the console. Preserve both source artifacts, journal, and
snapshot privately. Follow [staged updates](staged-platform-updates.md); never
delete the lock/journal or force a different version through the fresh installer.

## Ask for help safely

For a non-security bug, provide the commit/version, distribution/architecture,
installation method, operation ID, stable error code, UTC time, and a minimal
synthetic reproduction. Say which checks passed and which were not run. Review
logs for personal data and secrets; omit raw configs, environment dumps, URLs
with tokens, full journals, databases, and backup contents. Security reports
use the [private reporting process](../SECURITY.md).
