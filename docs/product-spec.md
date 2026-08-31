# Product Specification

Status: Draft 0.1  
Source language: English  
Initial UI languages: English and German

## 1. Product definition

Stackfort manages a single Linux hosting node. It provides administrators
with server, package, account, and service controls and gives hosting users a
restricted interface for domains, files, backups, databases, security, and
account settings.

The initial product is intentionally a single-node system. Its data model and
API should not prevent a future controller from managing multiple nodes, but
clustering is outside the first release.

## 2. Product principles

1. **Safe by default.** A newly created site must be TLS-enabled, isolated, and
   uncached until the system knows that caching is safe.
2. **No hidden root shell.** The browser-facing service is unprivileged. All
   privileged operations are typed, validated, allowlisted, and audited.
3. **Desired state over command sequences.** Configuration is generated from
   stored state and reconciled transactionally.
4. **Performance with evidence.** Optional components are selected through
   repeatable benchmarks rather than reputation alone.
5. **Recoverability.** Mutating operations expose progress, fail safely, and
   either roll back or provide a deterministic repair path.
6. **Honest limits.** The UI distinguishes hard kernel-enforced limits from
   measured or advisory limits.
7. **Accessible simplicity.** Common workflows use safe presets; advanced
   controls are available without being required.

## 3. Personas and authorization model

### 3.1 Identity

An identity is a person or automation principal that can authenticate. It has a
unique email address, password credentials, optional TOTP factors, recovery
codes, sessions, and scoped API tokens.

### 3.2 Hosting account

A hosting account owns domains, files, databases, backups, jobs, container
applications, and resource allocations. Each account maps to a dedicated Linux
UID/GID and a dedicated resource-control subtree.

### 3.3 Roles

- **Platform administrator:** manages the node, services, packages, identities,
  accounts, updates, and global policy.
- **Account owner:** has full access to one hosting account but no host-wide
  privileges.
- **Account member (post-MVP):** receives selected permissions for an account.
- **Read-only auditor (post-MVP):** views configuration, usage, and audit data.

An identity and a hosting account are separate records. This permits one person
to manage multiple accounts and later enables teams and reseller-style access.

## 4. Administrative requirements

### 4.1 Dashboard and server information

The administrator dashboard shall show:

- distribution, release, architecture, kernel, hostname, boot time, and uptime;
- public and private IPv4/IPv6 addresses, explicitly labelled by discovery
  method;
- logical CPUs and load;
- installed and used RAM and swap;
- filesystem capacity, usage, type, quota capability, and inode usage;
- pressure indicators when available;
- active panel version, update channel, and last successful update check.

Public-IP discovery must tolerate offline systems and must never block the rest
of the dashboard.

### 4.2 Service health

For every managed service the UI shall distinguish:

- installed/not installed;
- enabled/disabled;
- active/degraded/failed;
- health-check result;
- installed version and supported version range;
- last state change and recent relevant log messages.

Initial services include NGINX, PHP-FPM versions, MariaDB, Vinyl Cache when
installed, the panel services, Podman, and the firewall integration.

### 4.3 Packages

An administrator can create, update, clone, archive, and assign a package.
A package supports:

- maximum domains;
- maximum databases and database users;
- maximum scheduled jobs and OCI applications;
- CPU quota and optional CPU weight;
- memory, swap, and process limits;
- storage bytes and optionally inode limits;
- read/write bandwidth and IOPS limits when the storage stack supports them;
- monthly ingress, egress, or combined transfer allowance;
- allowed PHP versions and features;
- permission to use OCI applications, custom redirects, WAF exceptions, and
  scheduled backups.

Changing a package produces a preview. Reductions that place an account above a
count or storage limit do not silently delete resources: creation is blocked and
the account is marked over quota. Runtime limits may be applied immediately only
after an administrator sees their operational impact.

### 4.4 Account and identity administration

Administrators can create, suspend, resume, edit, and archive hosting accounts;
assign identities; reset an identity password; revoke sessions and API tokens;
and change an email address through a verified workflow.

Suspension must be reversible. It serves a neutral suspension page for public
sites, stops account-owned application runtimes and scheduled jobs, and preserves
data. It must not delete databases, files, or backups.

### 4.5 Administrator profile

An administrator can change their email address, password, language, TOTP
factors, recovery codes, and active sessions. Sensitive changes require recent
authentication and create audit events.

## 5. Hosting-account requirements

### 5.1 Domains

A user can create, inspect, update, and remove domains and subdomains within the
assigned package limits.

Supported target types:

- static document root;
- PHP document root and PHP version;
- reverse proxy to an account-owned OCI application;
- redirect-only domain.

Default roots:

- primary domain: `public_html`;
- subdomain: the full ASCII/Punycode domain name;
- shared-root option: the selected parent domain's root.

Document roots are relative to the account root. Absolute paths, `..`, unsafe
symlink traversal, control characters, and paths owned by another account are
rejected.

When a subdomain shares a root, the UI must state that both hosts deliver the
same files. Deleting either domain must not delete a shared root automatically.

Domain creation validates syntax, IDNA conversion, conflicts, DNS resolution,
port availability, target health, package limits, and generated server config
before activation.

### 5.2 Canonical host and redirects

Canonical-host choices:

- prefer apex/non-`www`;
- prefer `www`;
- serve both without redirect.

Redirect rules support HTTP 301 and 302, an HTTPS absolute target, optional path
and query preservation, and these source scopes:

- selected domains;
- all domains in the account;
- `www` only;
- both `www` and non-`www`;
- non-`www` only;
- wildcard subdomains, when explicitly enabled.

Before activation the UI displays example source and destination URLs. The API
rejects obvious redirect loops and invalid wildcard combinations.

### 5.3 TLS

TLS is enabled by default. A domain can use an automatically managed ACME
certificate or an administrator-approved imported certificate. The UI exposes
issuance state, names, issuer, expiry, next renewal, and errors.

HTTP-01 is the default challenge. DNS-01 and wildcard issuance are post-MVP
unless a provider integration is explicitly implemented. Failed issuance must
not replace a working certificate or break existing domains.

### 5.4 PHP

Every hosting account receives a separate PHP-FPM pool and Unix socket. Domains
select an administrator-allowed PHP version. Per-account limits bound the total
PHP processes and memory. Safe PHP presets are controlled by the administrator;
arbitrary `php.ini` directives are not accepted from users.

FrankenPHP worker mode is a future opt-in application runtime, not the default
for arbitrary PHP applications.

### 5.5 Page caching

Per-domain modes:

- disabled;
- respect origin caching headers;
- vetted application preset;
- administrator-managed advanced profile.

New PHP and OCI domains default to disabled. Cacheable methods are initially
GET and HEAD only. Requests with authentication, account sessions, application
administration paths, or known commerce/session cookies bypass cache according
to the selected profile.

The user can purge the whole domain or a safe URL scope and see HIT, MISS,
BYPASS, hit ratio, object count, and estimated cache memory. Direct arbitrary
VCL input is not an account-user feature.

### 5.6 File manager

The file manager supports:

- directory navigation and metadata;
- chunked upload and resumable upload when practical;
- download;
- create file/directory;
- rename, copy, move, and reversible delete;
- ZIP and TAR creation;
- ZIP, TAR, TAR.GZ, and TAR.ZST extraction.

All operations run with the account's filesystem identity. Archive extraction
rejects traversal, absolute paths, special device files, unexpected symlinks,
excessive expansion, excessive file counts, and quota overflow. Uploads land in
a staging file and become visible only after validation succeeds.

### 5.7 Backups

Backup scopes:

- complete hosting account;
- selected document root.

A complete account backup contains a versioned manifest, files, database dumps,
domain configuration, PHP settings, redirects, scheduled jobs, OCI definitions,
and encrypted secret material where applicable.

Backups support create, inspect, download, upload, verify, restore, and delete.
Restores unpack to staging, validate checksums and limits, preview conflicts, and
activate only after validation. A root-only restore never modifies databases or
account-wide configuration.

Scheduled backups and retention policies follow after manual backup/restore is
proven reliable.

### 5.8 Databases

The database wizard performs these discrete steps:

1. create a database;
2. create or select a database user;
3. select an access preset or explicit privileges;
4. review and apply.

Users can list, create, rename where safely supported, edit, and delete their
databases and database users. Destructive actions require typed confirmation and
offer a pre-deletion backup when possible.

Database names and usernames receive an internal account prefix to guarantee
global uniqueness. No hosting identity receives administrative MariaDB access.

phpMyAdmin uses its signon integration. Automatic login is limited to a selected
account-owned database identity through a short-lived, single-use handoff. Root
or panel service credentials are never supplied to phpMyAdmin.

### 5.9 Web application firewall

Per-domain choices:

- off;
- detection only;
- blocking with OWASP CRS paranoia level 1;
- administrator-managed advanced policy.

The user can see sanitized WAF events and request an exception. Account users do
not write raw SecLang directives. Administrators can create narrow rule-ID,
path, parameter, and domain exceptions with expiry and an audit trail.

### 5.10 OCI applications

The product accepts OCI/Containerfile and Compose-like project definitions only
through a constrained schema. Initial support may deliberately cover a subset
rather than claim full Docker Compose compatibility.

Applications run rootless under the account identity, expose only internal
ports, and are reachable publicly only through a panel-managed domain proxy.
Privileged containers, host namespaces, arbitrary host mounts, engine sockets,
device access, and unapproved capabilities are prohibited.

### 5.11 Domain logs

Owners, members, auditors, and administrators can view bounded per-domain
access and error records. Query strings, credentials, cookies, referrers, and
user agents are not captured in managed access logs; native error text is
redacted again before display. The UI discloses the active seven-day/size
retention policy and uses an opaque newest-first pagination cursor.

### 5.12 Account settings

Users can change email through verification, password after recent
authentication, language, TOTP factors, recovery codes, sessions, and scoped API
tokens. An account owner can view package limits and current usage.

## 6. Installer and lifecycle

Initial supported fresh installations:

| Distribution | Architecture | Initial status |
| --- | --- | --- |
| Debian 13 | amd64 | Required |
| Ubuntu 26.04 LTS | amd64 | Required |
| Rocky Linux 10 | amd64 | Required |
| All three | arm64 | After amd64 release gate |

The installer detects the exact distribution, validates kernel/cgroup/quota
features, available memory and storage, occupied ports, SELinux/AppArmor state,
and conflicting services. Initial releases abort on a non-fresh or unsupported
host instead of modifying an unknown configuration.

The one-line installer is a convenience entry point. Documentation must also
show how to download, inspect, verify, and execute it separately.

Updates use semantic versions and stable/beta channels. Automatic update checks
are enabled by default; automatic functional updates are opt-in. Downloaded
artifacts require cryptographic verification, staging, a health check, and a
rollback path. Database migrations are forward-compatible within the documented
rollback window.

## 7. Explicit non-goals for version 1

- mail server or mailbox hosting;
- authoritative DNS hosting;
- reseller billing and invoicing;
- multi-node scheduling or high availability;
- Kubernetes orchestration;
- importing arbitrary existing cPanel/CloudPanel installations;
- Windows or non-systemd host support;
- unrestricted root shells or arbitrary host commands from the panel;
- full Docker Compose compatibility.

## 8. Initial success criteria

The first public beta is possible only when:

- all supported distributions pass clean-install, upgrade, rollback, and
  uninstall tests in disposable VMs;
- cross-account file, process, database, backup, and container isolation tests
  pass;
- domain and account mutations survive injected failures without corrupting
  active configuration;
- backup/restore recovery is tested, not merely backup creation;
- authentication, authorization, path handling, archive handling, and agent RPC
  receive focused security review;
- repeatable NGINX-only and NGINX+Vinyl benchmarks are published;
- English and German critical workflows are complete;
- documentation clearly labels experimental functionality.

## 9. Open product decisions

- Vue component and CSS strategy after the first accessible UI prototype.
- Initial supported PHP version range on each distribution.
- Whether SFTP lands in the MVP or immediately after it.
- Whether monthly transfer overage suspends service, throttles it, or only alerts.
- Backup encryption defaults and initial remote storage providers.
