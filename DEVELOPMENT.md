# Developing Stackfort

Stackfort currently pins:

- Go 1.26.6 through `.go-version` and the `go` directive;
- Node.js 24.19.0 through `.nvmrc`;
- npm 12.0.2 and exact frontend packages through `web/package.json` and
  `web/package-lock.json`.

Use a disposable development machine. Do not run the host agent or future
installers on a server containing valuable data.

## Complete verification

On Linux, the repository-level verification runs formatting, static analysis,
race-enabled Go tests, an actual Unix-socket agent smoke test, locked frontend
installation, type checking, tests, a production build, dependency audit, and
the immutable GitHub Action reference check:

```sh
bash scripts/verify.sh
```

CI additionally runs ShellCheck, actionlint, govulncheck, gosec, CodeQL,
dependency review, and secret scanning. Windows development can run the API and
web checks, but it does not replace the Linux agent/runtime gates.

## Control API

```sh
go test ./...
go vet ./...
go run ./cmd/stackfort-api
```

The API binds to `127.0.0.1:8080`. Override it only for local development:

```sh
STACKFORT_API_ADDRESS=127.0.0.1:18080 go run ./cmd/stackfort-api
```

Linux panel state defaults to `/var/lib/stackfort/stackfort.db`. An unprivileged
development run can select a private absolute local path:

```sh
STACKFORT_STATE_PATH="$PWD/work/stackfort.db" go run ./cmd/stackfort-api
```

The startup sequence opens the database, verifies the SQLite version and
integrity, enables WAL, and applies checksum-locked migrations before binding
the HTTP listener. See [`docs/persistence.md`](docs/persistence.md).

Current public endpoints:

- `GET /api/v1/health`
- `GET /api/v1/build`
- `GET /api/v1/bootstrap`
- `POST /api/v1/bootstrap`
- `POST /api/v1/login`
- `POST /api/v1/login/mfa`

Current authenticated endpoints:

- `GET /api/v1/session`
- `POST /api/v1/logout`
- `GET /api/v1/mfa/totp`
- `POST /api/v1/mfa/totp/setup`
- `POST /api/v1/mfa/totp/setup/{challengeID}/confirm`
- `DELETE /api/v1/mfa/totp`
- `GET /api/v1/sessions`
- `DELETE /api/v1/sessions/{sessionID}`
- `POST /api/v1/sessions/revoke-all`
- `GET /api/v1/me`
- `PATCH /api/v1/me/profile`
- `GET /api/v1/accounts/{accountID}`
- `GET /api/v1/accounts/{accountID}/domains`
- `GET /api/v1/accounts/{accountID}/php`
- `GET /api/v1/accounts/{accountID}/files`
- `GET /api/v1/accounts/{accountID}/files/download?path=...`
- `GET /api/v1/accounts/{accountID}/logs?domainId=...&kind=access|error`
- `GET /api/v1/accounts/{accountID}/operations/{operationID}`
- `POST /api/v1/accounts/{accountID}/domains/preview` (read-only routing calculation)
- `POST /api/v1/accounts/{accountID}/domains`
- `PATCH /api/v1/accounts/{accountID}/domains/{domainID}`
- `POST /api/v1/accounts/{accountID}/domains/{domainID}/suspend`
- `POST /api/v1/accounts/{accountID}/domains/{domainID}/resume`
- `DELETE /api/v1/accounts/{accountID}/domains/{domainID}`
- `POST /api/v1/accounts/{accountID}/domains/{domainID}/tls/issue`
- `GET /api/v1/accounts/{accountID}/domains/{domainID}/tls/certificates`
- `GET /api/v1/admin/host/capabilities` (platform administrator, Linux agent required)

Create a development bootstrap capability against the selected state path:

```sh
STACKFORT_STATE_PATH="$PWD/work/stackfort.db" \
  go run ./cmd/stackfort-api bootstrap create --ttl=15m
```

The command is the only place that reveals the raw capability. It does not
start the HTTP server. See
[`docs/administrator-bootstrap.md`](docs/administrator-bootstrap.md).

Login accepts JSON only. Production browser sessions always use `Secure`
host-only cookies, so a plain-HTTP local browser will not retain them. Use an
HTTPS development proxy when testing cookie behavior; do not weaken the cookie
flags. The CSRF cookie must be copied to `X-CSRF-Token` for logout and every
future unsafe authenticated request. See
[`docs/password-authentication-and-sessions.md`](docs/password-authentication-and-sessions.md).

TOTP requires the external 256-bit host master key. By default the API creates
`master.key` next to the selected SQLite database. Development can select a
private absolute file with `STACKFORT_MASTER_KEY_PATH`; never commit or share
that file. MFA and session-control details are in
[`docs/totp-recovery-and-session-management.md`](docs/totp-recovery-and-session-management.md).

## Host agent

The agent requires Linux and listens only on a Unix-domain socket. For an
unprivileged local smoke test, choose a private temporary directory:

```sh
agent_dir="$(mktemp -d)"
go build -trimpath \
  -ldflags "-X main.configuredSocketPath=$agent_dir/agent.sock" \
  -o "$agent_dir/stackfort-agent" ./cmd/stackfort-agent
"$agent_dir/stackfort-agent"
```

In another terminal:

```sh
curl --unix-socket "$agent_dir/agent.sock" http://localhost/v1/health
```

Production builds always use `/run/stackfort/agent.sock`; there is no runtime
socket-path input for the privileged process. The development process currently
provides health plus the typed `protocol.handshake`,
`host.capabilities.inspect`, `hosting.identity.reconcile`,
`hosting.identity.delete`, `hosting.filesystem.reconcile`, and
`hosting.document-root.ensure`, plus managed NGINX baseline/activation RPCs.
G-001/G-002 add typed HTTP-01 and fixed-path certificate-staging RPCs; the
managed PHP slice adds typed account-pool reconciliation and bounded read-only
pool inspection. K-001 adds metadata-only managed file listing; K-002 keeps
file bytes on a separate bounded stream and reads them in an account-credential
helper rather than in the privileged parent.
The mutation operations remain privileged and must only be exercised by the
disposable host harness. The Linux smoke build
injects only its disposable socket path and expected numeric peer UID/GID; the
production default resolves the `stackfort-api` service identity. Do not add
arbitrary command execution, shell-string parameters, arbitrary environments,
or caller-selected paths to the agent protocol. See
[`docs/local-agent-protocol.md`](docs/local-agent-protocol.md).

Capability fixture and parser tests can be run on every development platform:

```sh
go test ./internal/hostcapabilities
```

The real kernel/systemd/quota check still requires a supported disposable VM.
Real E-001 user/group/ownership mutation also requires that VM; normal local and
shared CI runs cover the reconciler with non-mutating host fixtures.

## Web interface

```sh
cd web
npm ci
npm run typecheck
npm test
npm run dev
```

Vite serves `127.0.0.1:5173` and proxies `/api` to `127.0.0.1:8080`.
Production assets are created with `npm run build` in `web/dist`.

The real session and CSRF cookies use the `__Host-` prefix and `Secure`
attribute. An end-to-end browser login therefore requires an HTTPS development
front end or the production NGINX boundary; do not weaken cookie attributes to
make plain HTTP retain a session. Component tests use controlled API responses.

English is the source locale. Every new user-facing English message must receive
a German translation in the same change. Catalog tests reject missing keys,
empty messages, and mismatched interpolation placeholders. `npm run check:i18n`
parses Vue templates and rejects literal critical user-interface text and
accessibility labels. Dates, numbers, bytes, byte rates, percentages, and
durations must use the helpers in `web/src/formatting.ts`.

## Build provenance

Release automation injects provenance without modifying source files:

```sh
go build -trimpath \
  -ldflags "-s -w \
    -X github.com/RTBGG/stackfort/internal/buildinfo.Version=$VERSION \
    -X github.com/RTBGG/stackfort/internal/buildinfo.Commit=$COMMIT \
    -X github.com/RTBGG/stackfort/internal/buildinfo.BuildDate=$BUILD_DATE" \
  -o bin/stackfort-api ./cmd/stackfort-api
```

Build dates must use UTC RFC 3339. Release builds will use an immutable source
commit and reproducible timestamps supplied by CI.

The release-shaped archives can be generated on Linux with:

```sh
VERSION=0.1.0 \
COMMIT="$(git rev-parse HEAD)" \
SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)" \
STACKFORT_WAF_PACKAGE_DIR=/path/to/three-native-package-records \
bash scripts/build-release.sh
```

CI performs the build twice and rejects differing archive checksums. A tag or
manual release-candidate run also creates an SPDX JSON SBOM and GitHub/Sigstore
build attestations. It does not automatically create or publish a GitHub
Release.

The WAF package directory must contain the three native packages and their
adjacent `*.release.json` records produced on the locked Debian 13, Ubuntu
26.04, and Rocky Linux 10 targets. Non-development release versions fail
closed without this complete matrix. A development build may omit it and then
contains an explicit incomplete component manifest; that archive is useful for
cross-build reproducibility checks but source inspection rejects it before
journal creation or host mutation. Public non-development archives remain
amd64-only until arm64 WAF packages are qualified. The release workflow builds
the matrix before assembling and attesting the final archives.

## Local verification boundary

Windows can run the API and web interface and can cross-build the Linux agent.
Agent runtime, Unix socket permissions, race detection, and systemd hardening
must be verified on Linux CI and disposable supported-distribution machines.
