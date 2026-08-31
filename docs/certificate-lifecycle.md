# Certificate lifecycle

G-002 completes managed HTTP-01 certificates for the static-domain vertical
slice. It implements production issuance, staging, validation, transactional
NGINX activation, renewal, and logical retirement. DNS-01 wildcard issuance and
imported certificates remain separate future work.

## Account API

| Method and path | Behavior |
| --- | --- |
| `POST /api/v1/accounts/{accountID}/domains/{domainID}/tls/issue` | Queues a production issue operation. |
| `GET /api/v1/accounts/{accountID}/domains/{domainID}/tls/certificates` | Lists non-secret certificate history and renewal metadata. |
| `GET /api/v1/accounts/{accountID}/operations/{operationID}` | Reports bounded account-scoped issuance progress without operation input or results. |

The mutation requires an authenticated account-resource manager, session-bound
CSRF proof, `Idempotency-Key`, and a request ID. It accepts no authority URL,
certificate path, private key, or NGINX input. Once a valid production ACME
account exists, the background scheduler also queues the first certificate for
eligible active HTTP-01 domains. The owner browser follows the returned
operation ID to a terminal state and then refreshes the domain TLS state
automatically.

## Durable flow

1. Commit an immutable order record, exact sorted names, and an encrypted P-256
   certificate key.
2. Create or resume the RFC 8555 order and persist its URL.
3. Present each HTTP-01 response through the fixed root-owned agent operation,
   accept and await authorization, then clean the response.
4. Submit a CSR using the retained key, or fetch the certificate when replay
   finds an already-valid order.
5. Verify the exact SAN set, hostname coverage, key match, validity, server-auth
   usage, and chain signatures. Persist the public chain as `staged`.
6. Stage root-owned mode-`0644` chain and mode-`0600` private-key files below
   `/etc/nginx/stackfort/certificates/<certificate-id>`.
7. Render a complete HTTPS revision. The fixed certificate ID becomes a fixed
   path only inside trusted renderer/agent code. Test NGINX, atomically promote,
   reload, and health-check using the existing rollback transaction.
8. Atomically mark the candidate active and the predecessor retired.

The operation payload contains only domain ID, fixed authority environment,
and optional predecessor ID. Private keys are envelope-encrypted in SQLite and
materialized only for the internal worker and root agent call. API responses,
operation progress, and audit details contain metadata only.

The account workspace exposes the bounded certificate-history response on
demand. It distinguishes active, staged, ordering, and retired records and
shows names, issuer, validity, renewal, activation/retirement timestamps, and
the SHA-256 fingerprint. It never renders PEM material or authority URLs. An
open history is refreshed automatically after a successful issuance operation.

The automated protocol test uses an in-process private TLS staging CA and
exercises new-order JWS submission, two HTTP-01 authorizations, CSR finalization,
PEM-chain download, cryptographic validation, and replay from the retained
order URL without contacting a public authority.

## Failure and renewal behavior

A failed initial issue records `issuanceStatus: failed`. A failed renewal does
the same while retaining the active certificate reference, issuer, validity,
and served NGINX revision. Each operation has four bounded attempts. After a
renewal operation exhausts them, the scheduler waits for the persisted retry
time before creating a new bounded round.

The fallback renewal point is sampled once between 60% and 65% of the issued
certificate's lifetime, leaving at least 35% for recovery. This follows Let's
Encrypt's recommendation to use one-third remaining lifetime as a non-ARI
backstop while adding jitter. ARI support is intentionally deferred until its
replacement-order semantics are implemented end to end.

Removing a domain or changing its exact TLS names/challenge type retires and
detaches the old certificate. Records and root-only files are retained for
history and revision rollback; a future garbage collector will need an
explicit retention window and revision-reference check.

## Host validation

The disposable Hyper-V matrix runs a complete private-ACME lifecycle through
the authenticated account HTTP API, persisted operation runner, production
Unix-socket agent RPC boundary with kernel peer credentials, fixed host
reconcilers, and managed NGINX. The RFC 8555 fixture validates the apex and
`www` HTTP-01 key authorizations by requesting the real port-80 NGINX routes,
signs the submitted CSR, and returns a real chain. The suite then verifies
challenge cleanup, exact SAN/key/chain checks, root-owned staging, HTTPS SNI
activation, scheduled renewal with predecessor retirement, failed-renewal
retention of the still-valid certificate, non-secret API history, and final
domain/TLS retirement.

The release-shaped archive passes this lifecycle on Debian 13, Ubuntu 26.04,
and Rocky Linux 10.2. The harness requires the machine-readable
`private-acme-agent-rpc-lifecycle=passed` marker. Rocky is SELinux enforcing;
`/etc/nginx/stackfort/certificates` has `httpd_config_t`, the chain is
root-owned mode `0644`, and the private key is root-owned mode `0600`.

References:

- [RFC 8555 order flow](https://www.rfc-editor.org/rfc/rfc8555.html#section-7.4)
- [Go `x/crypto/acme` order API](https://pkg.go.dev/golang.org/x/crypto/acme)
- [Let's Encrypt integration guide](https://letsencrypt.org/ca/docs/integration-guide/)
- [Let's Encrypt ARI integration guide](https://letsencrypt.org/2024/04/25/guide-to-integrating-ari-into-existing-acme-clients/)
