# Secure administrator bootstrap

Stackfort has no default username or password. A local operator creates a
short-lived bearer capability after installation and uses it once to create the
first platform administrator.

## Operator flow

Run the command as the operating-system identity that can access Stackfort's
private state database:

```sh
stackfort-api bootstrap create
```

The command prints a 256-bit URL-safe token once, its UTC expiry, and no other
secret. The default lifetime is 15 minutes. An operator may select a lifetime
from one minute through one hour or deliberately invalidate an outstanding
capability:

```sh
stackfort-api bootstrap create --ttl=10m
stackfort-api bootstrap create --replace
```

Do not place the generated token in command arguments, URLs, screenshots, issue
reports, or shell scripts. The future installer/UI handoff must keep it in the
URL fragment or another channel that does not enter proxy request logs; that UI
work is separate from C-001.

## API

`GET /api/v1/bootstrap` returns only lifecycle state:

```json
{
  "required": true,
  "capabilityActive": true,
  "expiresAt": "2026-08-16T12:15:00Z"
}
```

It never returns a capability or digest. `POST /api/v1/bootstrap` accepts one
bounded JSON object containing `token`, `email`, `displayName`, `password`, and
an `en` or `de` locale. Unknown fields, trailing JSON values, and bodies above
4 KiB are rejected. Successful creation returns non-secret identity fields and
does not create a browser session; login and cookie sessions belong to C-002.

The API currently derives the source from its direct TCP peer and deliberately
does not trust `Forwarded`, `X-Forwarded-For`, or similar client-supplied
headers. Behind the local NGINX panel proxy this makes the per-source bucket the
proxy peer until an authenticated/trusted proxy boundary is implemented. The
separate global bucket remains effective.

## Persistence and atomicity

The raw capability exists only in process memory and command output. SQLite
stores a SHA-256 digest, UUIDv7 identifier, creation/expiry timestamps, and one
terminal transition (`consumed` or `invalidated`). A partial unique index
allows only one active capability. Triggers reject mutation of capability
history and deletion.

Before password derivation, one serialized transaction:

1. confirms that no platform administrator exists;
2. consumes persistent global and direct-source rate-limit capacity;
3. checks the active capability digest in constant time; and
4. invalidates an expired capability.

The final serialized transaction rechecks the administrator and capability,
then inserts the identity, Argon2id credential, platform-administrator role,
capability consumption, and hash-chained audit event together. A crash or
constraint failure cannot leave a partially bootstrapped administrator. Racing
requests can produce only one administrator. At most two Argon2id derivations
run concurrently in the API process, bounding this path to approximately
128 MiB of password-hashing working memory.

The source bucket allows five attempts per minute and blocks for five minutes
on the sixth. The global bucket allows 30 attempts per minute and blocks for one
minute on the 31st. Both use SQLite state and therefore survive API restarts;
source rows inactive for 24 hours are removed opportunistically.

## Password handling

Bootstrap passwords:

- contain 15 through 128 Unicode code points and at most 1,024 UTF-8 bytes;
- accept Unicode and whitespace without composition rules;
- are neither trimmed, normalized, nor silently truncated; and
- use a fresh 16-byte cryptographic salt.

Credentials use Argon2id v1.3 with 64 MiB memory, three iterations, four lanes,
and a 32-byte result. Parameters are stored with each credential so C-002 can
verify and later upgrade them. Breached-password screening and an interactive
strength meter remain follow-up work because the bootstrap path must not depend
on an external service.

References:

- [Go Argon2 package and RFC 9106 parameter guidance](https://pkg.go.dev/golang.org/x/crypto/argon2)
- [OWASP Password Storage Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [OWASP Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
