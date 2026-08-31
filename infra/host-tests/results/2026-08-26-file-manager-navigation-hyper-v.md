# K-001 file-manager navigation Hyper-V qualification

Date: 2026-08-26  
Result: passed on Debian 13, Ubuntu 26.04, and Rocky Linux 10

## Qualified artifact

- Focused Linux `amd64` integration binary:
  `infra/host-tests/work/stackfort-file-manager.test`
- SHA-256:
  `2157f156fb584a30e2e79d8a8fb5b651f20c0d66586cd1e033b3b9f6ee7f221e`
- Guest shape: 2 vCPU, 4 GiB startup RAM, system disk plus dedicated
  managed-hosting disk.

The binary was built once from the final K-001 tree. All three guest runs used
the same binary and the focused wrapper returned every VM to its prior
powered-off state.

## Assertions

The test creates an isolated reserved-range account identity and project-quota
layout, then verifies:

1. the exact six managed root directories and their directory type;
2. opaque, bounded ten-entry pagination across 105 account-owned files without
   duplicates or omission;
3. safe counting, but no actionable representation, of a filename containing
   a control character;
4. symlink metadata visibility with rejected descent into a symlink targeting
   `/etc`;
5. rejection of `../etc`; and
6. rejection of a forged redundant hosting identity before host access.

Every guest emitted:

```text
STACKFORT_QUALIFICATION file-manager-navigation=passed
```

| Guest | Result | Test duration |
| --- | --- | ---: |
| Debian 13 | passed | 0.33 s |
| Ubuntu 26.04 | passed | 0.41 s |
| Rocky Linux 10 | passed | 0.40 s |

## Commands

```powershell
.\infra\host-tests\Test-StackfortFileManagerHyperVVm.ps1 -ImageId debian-13 -VmName stackfort-debian-13 -SkipBuild
.\infra\host-tests\Test-StackfortFileManagerHyperVVm.ps1 -ImageId ubuntu-26.04 -VmName stackfort-ubuntu-26-04-v2 -SkipBuild
.\infra\host-tests\Test-StackfortFileManagerHyperVVm.ps1 -ImageId rocky-10 -VmName stackfort-rocky-10 -SkipBuild
```
