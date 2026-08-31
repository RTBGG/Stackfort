# ACME account and HTTP-01 routing

G-001 provides the durable ACME account identity and the safe port-80 response
path needed by G-002. It does not request or activate certificates yet.

## Administrator API

| Method and path | Behavior |
| --- | --- |
| `GET /api/v1/admin/acme/accounts` | Lists non-secret metadata for configured environments. |
| `POST /api/v1/admin/acme/accounts` | Queues replay-safe account registration. |

Registration accepts only `environment`, `contactEmail`, and the explicit
`termsAccepted` boolean. Supported environment values are
`letsencrypt-staging` and `letsencrypt-production`; their directory URLs are
compiled into the server. Mutations require a browser session, CSRF proof,
recent platform-administrator authentication, `Idempotency-Key`, and strict
JSON without unknown fields.

The list response omits the encrypted key fields, public-key thumbprint, and
authority account/order URLs. Account-scoped APIs do not expose ACME account
records at all.

## Registration recovery

The `acme.account.register` operation first commits a generated P-256 key using
per-record AES-256-GCM envelope encryption under the external host master key.
It then discovers and registers against the fixed authority. If the authority
accepted the key before a local crash, replay uses the same key and retrieves
the already-existing account before committing the result. Operation progress
uses stable, non-secret message codes; raw protocol errors and credentials are
not persisted in progress or audit details.

Automated protocol coverage uses an in-process private RFC 8555 staging CA, so
tests exercise discovery, nonce retrieval, JWS registration, terms agreement,
and account metadata without creating external accounts. A separate handler
test proves selection of Let’s Encrypt’s official staging directory.

## HTTP-01 data path

The typed domain snapshot contains only a boolean indicating whether the fixed
HTTP-01 route is required. NGINX serves
`/.well-known/acme-challenge/<token>` directly from the root-owned
`/var/lib/stackfort-agent/acme-http01` directory. That location:

- is emitted before canonical and customer redirect locations;
- permits only GET and HEAD;
- returns `Cache-Control: no-store`;
- never enters Vinyl or an account document root;
- disables auto-indexing and symbolic-link traversal.

The agent accepts only `present` and `cleanup` tagged intents. Tokens use the
unpadded base64url alphabet with a minimum length representing RFC 8555’s
128-bit entropy requirement. A presented value must be the exact token followed
by one P-256 JWK thumbprint. Descriptor-relative Linux filesystem operations
create or inspect only the fixed root-owned response file. Conflicting files,
symlinks, ownership, or modes fail closed.

Debian 13, Ubuntu 26.04, and Rocky Linux 10.2 integration tests present a real
token, retrieve it from both a content host and its normally redirected `www`
alias, remove it, and verify the route returns 404. Rocky remains SELinux
enforcing while the fixed subtree uses a persistent `httpd_sys_content_t`
mapping.

References:

- [RFC 8555 HTTP-01 challenge](https://www.rfc-editor.org/rfc/rfc8555.html#section-8.3)
- [Let’s Encrypt staging environment](https://letsencrypt.org/docs/staging-environment/)
- [Go `x/crypto/acme` client](https://pkg.go.dev/golang.org/x/crypto/acme)
- [`semanage-fcontext(8)`](https://www.man7.org/linux/man-pages/man8/semanage-fcontext.8.html)
- [`restorecon(8)`](https://www.man7.org/linux/man-pages/man8/restorecon.8.html)
