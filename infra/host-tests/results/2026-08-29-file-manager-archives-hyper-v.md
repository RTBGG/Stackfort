# K-005 file-manager archive Hyper-V qualification

- Date: 2026-08-29
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `4a6f074d7c1229d4ccfa4c4fd4dd4fc7fbc4878857ec423c5f73efab425bf4d4`
- Production agent/helper SHA-256:
  `77eec450c872581659c7ae04a51fbc864b78be62934d544be577242babc40809`

The integration and production agent/helper binaries were built once from the
final K-005 tree. Debian executed the initial pair; Ubuntu and Rocky used the
same files with the wrapper's `-SkipBuild` option. Every guest performed the
archive operations as a fresh disposable hosting-account UID/GID through the
production helper.

| Guest | Result | Test duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 0.56 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 0.49 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 0.54 s |

Each run verified:

- bounded creation and successful reading of four-entry ZIP and tar.gz trees;
- extraction of both formats with exact content, empty files, and safe output
  modes;
- atomic no-replace activation that preserves an existing destination;
- rejection of ZIP parent traversal and confirmation that no path escaped the
  account extraction root;
- rejection of a ZIP symlink to `/etc/passwd`, duplicate ZIP members, and a
  small ZIP expanding to 128 MiB;
- rejection of a tar.gz hardlink to `/etc/passwd`;
- rejection of a symlink as an archive-creation source without exposing a
  destination;
- concealment of `.stackfort-operations` from ordinary file-manager listings;
  and
- an empty operation-staging directory after every successful and rejected
  operation.

All wrappers observed `STACKFORT_QUALIFICATION file-manager-archives=passed`.
All three VMs were Off after qualification.

## Final-tree validation

- `gofmt`, `go test ./...`, and `go vet ./...` passed.
- Linux `amd64` gosec scanned 149 files and 44,085 lines with zero findings.
- govulncheck v1.1.4, using the vulnerability database updated on 2026-08-28,
  found no vulnerabilities reachable from the Go code.
- All 47 web tests, the TypeScript/Vite production build, and the
  English/German literal check passed; `npm audit` reported zero
  vulnerabilities.
