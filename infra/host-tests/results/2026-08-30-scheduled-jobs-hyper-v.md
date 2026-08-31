# K-009 scheduled-jobs Hyper-V qualification

- Date: 2026-08-30
- Host: Windows Hyper-V
- Architecture: Linux `amd64`, `CGO_ENABLED=0`
- Integration binary SHA-256:
  `69023e02a323c45e7983951a933382174573907b0c4ef96acbd2019becc6ab99`

The integration binary was built once from the final K-009 implementation and
reused unchanged on all three guests. The focused harness ensured only the
distribution-native PHP CLI already required by the production installer.

| Guest | Result | Test duration |
| --- | --- | ---: |
| Debian 13 (`stackfort-debian-13`) | passed | 1.44 s |
| Ubuntu 26.04 (`stackfort-ubuntu-26-04-v2`) | passed | 1.38 s |
| Rocky Linux 10 (`stackfort-rocky-10`) | passed | 1.26 s |

Each guest verified against its real systemd and native PHP runtime:

- systemd accepted all fixed interval/hourly/daily/weekly UTC calendars and
  the complete rendered service/timer pair;
- real Shell and PHP scripts ran with the exact hosting UID, account home as
  working directory, and the UID-derived account resource slice;
- the service exposed `NoNewPrivileges=yes`, `PrivateTmp=yes`,
  `ProtectSystem=strict`, a persistent timer, and root-owned `0644` units;
- the private temporary-file probe was absent from the host `/tmp`;
- identical reconciliation was a no-op, while a runtime/schedule update,
  disable, delete, and repeated delete converged exactly; and
- descriptor-relative validation rejected both symlinked and hard-linked
  account scripts before changing unit state.

The `STACKFORT_QUALIFICATION scheduled-jobs=passed` marker was observed on each
guest. All three VMs were `Off` afterward.
