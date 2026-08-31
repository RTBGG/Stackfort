# Canonical host and redirect-only routing

F-005 makes the domain routing preview and the activated NGINX behavior one
validated contract. Migration 015 adds an explicit source-host scope to every
immutable redirect revision.

## API contract

`POST /api/v1/accounts/{accountID}/domains/preview` accepts a domain name,
canonical mode, and typed target. It is an authenticated, authorization-checked
read-only calculation: it neither creates an operation nor changes account or
host state.

The response contains normalized Unicode and ASCII names and deterministic
examples using `/example/path?source=preview`. Every exact source host is shown
as `serve`, `redirect`, or `inactive`; redirect entries include the exact 301 or
302 destination and the selected path/query preservation flags. A wildcard
entry uses a concrete `preview.<base>` source URL while retaining `*.<base>` as
its source pattern.

The preview uses the same domain, IDNA, HTTPS target, host-mode, wildcard, and
obvious-loop validation boundary as persistence. Account package and global
route conflicts remain authoritative in the serialized mutation transaction.

## Source-host and canonical behavior

Static targets support these canonical modes:

| Mode | Apex | `www` |
| --- | --- | --- |
| `prefer_apex` | Serve | Permanent redirect to apex |
| `prefer_www` | Permanent redirect to `www` | Serve |
| `serve_both` | Serve | Serve |

Redirect-only targets independently select `apex_only`, `www_only`, or `both`.
An unselected exact host receives an isolated 404 route rather than the
customer redirect. Its fixed HTTP-01 location remains available when required,
so narrowing customer traffic cannot prevent certificate issuance.

Wildcard subdomain redirects require `both`. This rejects the ambiguous case
where `*.example.com` would silently match `www.example.com` despite a source
scope that declares `www` inactive.

## Destination construction

The normalized HTTPS target always retains its own path and query. Source path
and query data are appended only when their separate booleans are enabled:

| Preserve path | Preserve query | Example destination |
| --- | --- | --- |
| No | No | `https://target.test/base?fixed=1` |
| Yes | No | `https://target.test/base/example/path?fixed=1` |
| No | Yes | `https://target.test/base?fixed=1&source=preview` |
| Yes | Yes | `https://target.test/base/example/path?fixed=1&source=preview` |

The renderer emits literal URL fragments and its own NGINX variables as
separate quoted segments. In particular, `$uri` is inserted before the fixed
query delimiter; user-supplied dollar characters remain percent-encoded.

## Validation

Automated tests cover canonical modes, both redirect status codes, all four
path/query combinations, source-host modes, wildcard ambiguity, credentials,
fragments, non-HTTPS targets, malformed inputs, and same-host/wildcard loops.
The disposable Debian 13, Ubuntu 26.04, and Rocky Linux 10.2 tests make real
Host-routed HTTP requests and compare status and `Location` headers. Rocky runs
with SELinux enforcing.

