# K-006 local backup/restore Hyper-V qualification

- Date: 2026-08-29
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `3185e23199e8977d07bbb37fd2bee5359abe3fba3629896eb66072ec4b028126`
- Production agent/helper SHA-256:
  `90a420c183f56c48b888e2b9d7fbde72890ae3e64d8ad523399ed9b71761ead4`

The integration and production agent/helper binaries were built once from the
K-006 implementation. Debian, Ubuntu, and Rocky used that same pair; every
guest created a fresh disposable hosting-account UID/GID and executed payload
creation/extraction through the production helper under that account identity.

| Guest | Result | Test duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 0.42 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 0.28 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 0.26 s |

Each run verified:

- root-only account repository, backup directory, manifest, and payload modes;
- mode-`0600` independent manifest key, schema-1 manifest authentication, and
  full payload SHA-256 verification;
- bounded document-root backup and replacement with exact content/modes and
  removal of content absent from the backup;
- bounded visible-account-files backup and replacement while internal
  Stackfort operation state remains present;
- rejection of a byte-modified payload before restore changes visible content
  and rejection of a structurally valid but HMAC-modified manifest;
- rejection of a symlink in the backup source without publishing a backup;
- account-derived repository isolation for a second valid identity; and
- removal of hidden repository staging after successful and rejected work.

All wrappers observed `STACKFORT_QUALIFICATION local-backup-restore=passed`.
All three VMs were Off after qualification.

## Final-tree validation

- `gofmt`, `go test ./...`, and `go vet ./...` passed.
- Linux `amd64` gosec, including integration-tagged code, reported no findings.
- All 49 web tests, TypeScript type checking, the English/German literal check,
  and the Vite production build passed; `npm audit` reported zero
  vulnerabilities.
