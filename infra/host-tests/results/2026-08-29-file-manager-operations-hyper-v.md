# K-004 bounded file-operations Hyper-V qualification

- Date: 2026-08-29
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `01a6d04acf9c54949b5c7d83374026b7e485df44c3885379ba8d4daff26b5b22`
- Production agent/helper SHA-256:
  `451bae1c8fa384da82a91219101a83180c69d0948e3bd67e1968d0e4e3e70d06`

The integration and production agent/helper binaries were built once from the
final K-004 tree. Every guest executed namespace and recursive operations as a
fresh disposable hosting account UID/GID through the production helper.

| Guest | Result | Duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 0.25 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 0.16 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 0.15 s |

Each run verified:

- atomic no-replace rename and cross-directory move;
- recursive file/directory copy through hidden staging with source preservation;
- copy conflict handling without changing an existing destination;
- rejection of a symlink to `/etc/passwd` as a copy source;
- concealment of `.stackfort-operations` and `.stackfort-trash` from normal
  account listings;
- ordered trash metadata, atomic move to trash, and successful directory-tree
  restoration;
- restore conflict behavior that preserves both the recreated visible path and
  recoverable trash payload;
- bounded permanent purge and an empty trash result afterward; and
- real project-quota exhaustion returning the typed quota error without a
  visible partial copy destination.

All wrappers observed `STACKFORT_QUALIFICATION file-manager-operations=passed`.
All three VMs were Off after qualification.

## Final-tree validation

- `go test ./...` and `go vet ./...` passed.
- Linux `amd64` gosec scanned 148 files and 43,077 lines with zero findings.
- govulncheck found no reachable vulnerabilities in the Go code.
- All 45 web tests, the production build, and the English/German literal check
  passed; `npm audit` reported zero vulnerabilities.
