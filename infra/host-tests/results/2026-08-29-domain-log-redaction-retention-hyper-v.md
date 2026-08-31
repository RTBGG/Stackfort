# K-008 domain-log redaction/retention Hyper-V qualification

- Date: 2026-08-29
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `c4985b68220677d1a83b2e0520ab9ad0862e003ce67b1563757ce7d8dfb80d16`

The integration binary was built once from the final K-008 implementation and
reused unchanged on all three guests. Older disposable guests installed
`logrotate` through the focused harness because it became a required base
package in K-008; the production installer now installs and verifies it.

| Guest | Result | Test duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 0.45 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 1.04 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 2.42 s |

Each guest verified with production NGINX and the root log manager:

- unique query, authorization, cookie, referrer, and user-agent markers never
  reached the raw managed access file;
- actual access JSON parsed into a capped typed record with no query, while a
  crafted native error line received query, credential, and sensitive-path
  redaction before the account-facing response;
- newest-first inode/offset pagination and cross-account non-disclosure;
- exact root ownership/modes and rejection of a symlink-backed account log
  directory;
- two real forced `logrotate` cycles produced active `0640`, `.1`, and delayed
  `.2.gz` files using the shipped policy; and
- Rocky retained the expected `httpd_log_t` context under enforcing SELinux.

The `STACKFORT_QUALIFICATION domain-log-redaction-retention=passed` marker was
observed on each guest. All three VMs were `Off` afterward.
