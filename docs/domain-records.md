# Domain records

Migration `003_domain_records.sql` and `internal/core/domain.go` implement the
B-003 persistence boundary. They store domain intent and history; they do not
activate NGINX, resolve DNS, issue certificates, or touch account files.

## Domain identity

Every domain has two immutable names:

- `display_name`: the normalized Unicode form shown to a person;
- `ascii_name`: lowercase IDNA ASCII used for routing, certificates, and
  uniqueness.

`core.NormalizeDomainName` uses `golang.org/x/net/idna` with an explicit,
non-transitional UTS #46 lookup profile, strict DNS characters, label and DNS
length validation, and the RFC 5893 bidirectional-text rule. Stackfort does not
use the package's mutable convenience lookup profile. The dependency version is
locked, and upgrades must run the normalization compatibility corpus before the
lock changes.

Input may contain one terminal DNS root separator. Schemes, ports, paths,
credentials, wildcard labels, IP addresses, empty labels, and single-label
names are rejected. The stored Unicode form must round-trip to exactly the
stored ASCII form.

The domain entity stores a canonical base name. Supplying `www.example.com`
therefore creates the same routing identity as `example.com`: one live row
reserves both `example.com` and `www.example.com` globally. The canonical mode
then selects `prefer_apex`, `prefer_www`, or `serve_both`. A partial unique index
releases that route only after logical removal.

This is DNS syntax and identity validation, not domain-registration policy.
Stackfort deliberately does not use the public suffix list to decide whether a
name may be hosted. DNS resolution and ownership/preflight checks belong to the
activation operation.

## Targets and document roots

A domain has exactly one current target and may retain any number of superseded
targets. A target is one of:

- static document root;
- PHP document root plus an allowed major/minor version;
- account-owned OCI application;
- immutable redirect specification.

The database shape reserves OCI routing, but the repository currently rejects
it. Accepting an application ID before an account-owned application parent
record exists would make tenant ownership unverifiable. The OCI phase removes
that capability gate after adding the required composite foreign key.

Document roots are immutable account-owned records. Their paths are relative to
the account root and use Linux separators independent of the control API's host
OS. Stackfort currently accepts only canonical slash-separated ASCII segments
matching `[A-Za-z0-9][A-Za-z0-9._-]{0,254}`. It rejects absolute paths,
backslashes, empty, `.` or `..` segments, whitespace padding, control characters,
and paths over 1,024 bytes.

Default resolution is deterministic:

1. if another live domain in the account is a DNS parent, use the new domain's
   full ASCII name;
2. otherwise use `public_html`;
3. an explicit shared-root choice reuses the selected live domain's root, with
   the account enforced by a composite foreign key.

The model-level path check prevents lexical escape. E-002 now shares that
validator with the agent, then uses file-descriptor-relative, no-symlink
filesystem operations and rechecks ownership, mode, project inheritance, and
filesystem device because a string validator cannot prevent a filesystem race.

Root rows are never updated or deleted. Logical domain removal does not remove
files or a shared/non-empty root. `ReferenceCount` lets the UI disclose when
multiple live domains serve the same location.

## Redirects and wildcard routes

Redirect records accept only HTTP 301 or 302 and an absolute HTTPS URL. User
information, fragments, malformed escapes, decoded control characters, and
obvious same-host loops are rejected. Unicode destination hosts are stored as a
normalized ASCII URL. Redirect target URLs are not copied into audit details,
where query data could otherwise disclose application secrets.

Optional path and query preservation are explicit booleans. Wildcard behavior
is also explicit. Each redirect selects `apex_only`, `www_only`, or `both` as
its exact source-host scope; existing/omitted values normalize to `both`.
Wildcard subdomains require `both`, avoiding a wildcard that silently
re-enables an explicitly inactive `www` route. Serialized writes reject both
directions of overlap:

- an exact domain cannot be added below a live wildcard redirect;
- a wildcard redirect cannot be added above an existing exact domain.

The private SQLite database is not a public write interface. F-002 repeats the
same invariants while rendering, constructs source only from typed templates,
and tests the final NGINX routing table before activation. See
[Deterministic NGINX configuration renderer](deterministic-nginx-config-renderer.md).
F-005 additionally exposes an authorized server-side routing preview using the
same normalization and loop checks; see
[Canonical host and redirect-only routing](canonical-and-redirect-routing.md).

## TLS state

TLS is enabled by default. Each domain records desired certificate names, ACME
or imported mode, challenge type, issuance state, active certificate reference,
issuer, validity, renewal time, and a bounded error code. Private keys and ACME
credentials have no column in these tables.

The ordinary intent contains the base and `www` names and selects HTTP-01. A
wildcard redirect adds `*.<base>` and selects DNS-01. DNS-01 provisioning is
post-MVP, so such intent cannot become active until a provider integration or an
approved imported wildcard certificate exists. Target changes update desired
names and return issuance state to `pending` only when the certificate intent
changed. They never clear the active certificate reference; G-002 must stage and
validate a replacement before switching it.

Domain removal retains TLS state for history. The removed domain lifecycle state
makes that route ineligible for generated configuration.

## Applied state and history

An applied-state revision links, within one account:

- one immutable desired-state revision;
- an optional account-owned operation;
- a 32-byte SHA-256 digest of the rendered configuration;
- application and supersession timestamps.

Only one revision is current per account. Recording the next revision
atomically supersedes the prior row and appends an audit event. Generated config
and secrets are not duplicated in this record.

Domain removal is a status/timestamp update. Domain, target, redirect, root, TLS,
operation, desired/applied revision, and audit rows remain available. Database
triggers reject hard deletion or rewriting of immutable history.

## Transaction and authorization boundaries

Creation checks the account's effective package snapshot, domain count, allowed
PHP versions, and redirect permission inside the same serialized immediate
transaction as the domain, target, TLS, and audit writes. Global routing
conflicts and wildcard overlap checks therefore cannot race another repository
writer.

All repository reads and mutations require both account ID and object ID.
Composite foreign keys prevent cross-account root, redirect, desired-state, and
operation substitution. UUIDs are identifiers, not authorization. C-003
authorizes the actor's current membership and role. F-004's domain application
service now requests the matching view/manage account-resource action before
listing or queueing domain lifecycle work.

## Activation boundary

B-003 alone does not claim a site is reachable. F-004 now creates the selected
document roots, establishes worker ACL/SELinux access, activates a complete
typed account revision through F-003, and changes only still-matching pending
targets to active. G-001 now supplies the fixed HTTP-01 response route, while
DNS ownership/preflight and certificate readiness remain in G-002; F-004's
verified content service is HTTP-only.

References:

- [Go IDNA package](https://pkg.go.dev/golang.org/x/net/idna)
- [Unicode UTS #46](https://unicode.org/reports/tr46/)
- [RFC 5893 bidirectional-text rule](https://www.rfc-editor.org/rfc/rfc5893)
