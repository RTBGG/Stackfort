# Deterministic NGINX configuration renderer

F-002 turns persisted account and domain intent into one bounded, byte-stable
NGINX `http` include. `internal/nginxconfig` is a pure Go package: it reads no
files, runs no process, and does not activate configuration. F-003 places its
output into a private revision, validates that complete candidate, and promotes
or rolls it back transactionally. That consumer is now implemented; see
[Transactional NGINX site activation](transactional-nginx-activation.md).

## Input and output contract

`RenderAccount` accepts one fully derived `hostingidentity.Spec`, a set of
persisted `core.Domain` values, and renderer-owned header-policy enums. It
returns:

- `account-<canonical UUIDv7>.conf` as a safe fixed filename;
- a fresh byte slice containing the account include;
- its SHA-256 digest; and
- the number of eligible domains rendered.

At most 10,000 domain records and 4 MiB of output are accepted. `pending` and
`active` records are eligible; `suspended` and `removed` records produce no
route. Static and redirect-only targets are implemented. PHP and OCI targets
fail closed with `ErrUnsupportedTarget` until their typed upstream schemas are
implemented.

The renderer does not trust a value merely because it came from Stackfort's
private database. It repeats account identity, IDNA round-trip, canonical
lowercase hostname, lifecycle, tagged-target-union, account ownership,
document-root, HTTPS redirect, redirect-host, status-code, and wildcard-overlap
checks. Invalid persisted state produces no partial result.

## Context-specific source construction

No field accepts an NGINX snippet or directive. Source is assembled only from
fixed templates and these context-specific representations:

| Context | Representation |
| --- | --- |
| `server_name` | Revalidated lowercase IDNA ASCII exact names. The renderer alone may add `www.` and a leading `*.` for a typed wildcard redirect. |
| `root` | The validated immutable account home plus a canonical account-relative path. Backslashes, absolute paths, dot segments, control characters, `$`, quotes, and non-allowlisted path characters are rejected before joining. |
| Redirect target | A normalized absolute HTTPS URL inside a quoted `return` value. Backslash and quote receive NGINX-string escaping. A user-originated literal `$` becomes URL encoding `%24`; a backslash escape is insufficient because NGINX removes it before its rewrite module compiles variables. |
| Dynamic redirect suffix | Only fixed renderer tokens (`$uri`, `$is_args`, `$args`, or a renderer-derived map variable) are appended after the literal URL has been encoded. |
| Response header | A closed `HeaderPolicy` enum maps to fixed header name/value pairs. Unknown or duplicate enum values fail closed. |

This distinction is important: the same characters do not have the same
meaning in an NGINX token, quoted string, rewrite expression, or URL. Generic
"escape all strings" logic would not establish a safe source boundary.

## Stable routing output

Before writing, domains are sorted by canonical ASCII base name and headers by
their enum identifier. Duplicate base routes and exact names below any wildcard
redirect are rejected independently of input order. A redirect that combines a
fixed target query with source-query preservation receives a map variable named
with a short, collision-free ordinal assigned after canonical domain sorting;
maps are emitted in that same order.

Static output uses exact server names, an account-derived `root`, `index.html`,
and `try_files $uri $uri/ =404`. `prefer_apex` and `prefer_www` receive a
separate exact alias server with a 301 HTTPS canonical redirect;
`serve_both` serves both exact names. Redirect-only domains emit only the
selected 301/302 `return`, optional wildcard name, and explicitly selected path
and query suffixes. G-001 now adds only the fixed typed HTTP-01 exception before
those redirects. F-005 adds explicit apex-only, `www`-only, and both-host
redirect scopes plus a server-verified preview. Unselected exact hosts receive
an isolated 404 route while keeping HTTP-01 available. TLS listeners and
certificate readiness are implemented by G-002. F-003 supplies a local
route-selection health check, while external reachability remains lifecycle
work.

Equivalent desired state therefore yields identical bytes and an identical
digest regardless of Go slice order. Generated files remain artifacts; they
are never parsed back as product state.

## Verification

Unit tests include golden bytes, reordered inputs, invalid/noncanonical
hostnames, traversal and source-injection paths, malformed redirect URLs,
literal NGINX-variable syntax in redirect URLs, forged redirect-host metadata,
invalid/duplicate header policies, wildcard conflicts, lifecycle filtering,
and unsupported targets.

The disposable-host integration test renders a static site and a wildcard
redirect containing unknown dollar-prefixed URL markers into an isolated
temporary `http` context. The vendor `/usr/sbin/nginx -t` accepts the output on
Debian 13, Ubuntu 26.04, and Rocky Linux 10. This confirms both general syntax
and the non-obvious property that those user markers remain literal rather than
becoming NGINX variables.

Upstream references:

- [NGINX server names](https://nginx.org/en/docs/http/server_names.html)
- [`return` directive](https://nginx.org/en/docs/http/ngx_http_rewrite_module.html#return)
- [`add_header` directive](https://nginx.org/en/docs/http/ngx_http_headers_module.html#add_header)
- [`root` directive](https://nginx.org/en/docs/http/ngx_http_core_module.html#root)
- [`try_files` directive](https://nginx.org/en/docs/http/ngx_http_core_module.html#try_files)
- [NGINX embedded variables](https://nginx.org/en/docs/http/ngx_http_core_module.html#variables)
