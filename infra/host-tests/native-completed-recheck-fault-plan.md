# Exact-candidate completed-recheck process-loss plan

Reviewed 2026-09-12. **Proposed qualification only; not an executed result.**
Run only on the explicitly disposable Debian test VM after a complete installation
of the final tag-authenticated candidate. Preserve and identify a checkpoint
before injecting the failure. This intentionally makes the installed panel
unavailable and leaves explicit recovery necessary; it is not a production
troubleshooting recipe, power-cut test or filesystem-conversion test.

## Target and source binding

Use the actual public `onboard` completed-installation rerun to create its real
transient unit. Do not substitute the old laboratory `RuntimeInterrupt` test,
which targets a different, persistent installation service. Do not stop the VM,
APT/dpkg, `stackfort-native-install.service`, the source client, or an entire
cgroup for this test. Do not modify records, insert sleeps into the installed
binary, invent another unit or weaken release provenance to create a stop window.

Before starting the rerun, inspect through the locked operator interface:

```sh
sudo /var/lib/stackfort-installer/native-runtime-installer native status --format=json
sudo /var/lib/stackfort-installer/native-runtime-installer native recovery-plan --format=json
sudo sha256sum /var/lib/stackfort-installer/native-runtime-installer
```

Bind the test to **independently recorded final candidate** version, source commit,
archive SHA-256, installer SHA-256, VM identity and operation. Do not derive the
expected release identity solely from whichever files happen to be on the host.
The real record fields and files are:

| File below `/var/lib/stackfort-installer/` | Required observation |
| --- | --- |
| `native-release-manifest.json` | `release.policy.class` is `tag-release`; policy `version`/`commit`, `release.archiveSHA256` and `release.source.installerSHA256` match the independently selected candidate |
| `native-runtime-intent.json` | `installerSHA256` matches; canonical file digest matches manifest `bootSHA256` |
| `storage-state.json` | Phase `ready`, exact unchanged plan; operation is `plan.operationId` |
| `installation-admission.json` | Phase `admitted`, exact same plan, record its `attempt` and `bootId` |
| `install-state.json` | Complete bound package journal; operator status `admission.packageComplete` is true |

Preserve private before-copies or hashes of these records and of setup commitment
and registration receipts. Do not publish raw state or setup material. No pending
recovery approval, package operation, boot transition or competing rerun may be
in progress. Record actual successful panel/agent checks before injection.

## Observe and pause only the completed-recheck MainPID

The operation must be its canonical lowercase UUID. The sole allowed unit is:

```sh
unit="stackfort-native-recheck-${operation}.service"
sudo systemctl show "$unit" \
  --property=Id,Transient,MainPID,ControlGroup,Restart,KillMode,User,Group,ExecStart,ExecStopPost,InvocationID
```

Start the observer before invoking the same pinned public rerun from another
session. `Transient=yes`, `Restart=no`, root User/Group, `KillMode=control-group`
and `/system.slice/$unit` must be verified. The complete command lists must be
exactly the following, with no second command or caller-selected wrapper:

```text
ExecStart:
/var/lib/stackfort-installer/native-runtime-installer native-service recheck-completed --operation-id=<operation>
ExecStopPost:
/var/lib/stackfort-installer/native-runtime-installer native-service recheck-cleanup --operation-id=<operation>
```

The unit's limits are `RuntimeMaxSec=600`, `TimeoutStopSec=90`; capture its
InvocationID and a journal time/cursor before it can be collected. For its positive
MainPID, verify all of:

- `/proc/<PID>/exe` is exactly the sealed runtime path, not a deleted inode;
  SHA-256 of `/proc/<PID>/exe` equals the independently expected installer digest.
- `/proc/<PID>/cmdline`, split on NUL, is exactly the four arguments above,
  including argv[0]. Never search with a substring, `pgrep` or `pkill`.
- `/proc/<PID>/cgroup` is exactly `0::/system.slice/$unit` plus a newline.
- The process is root-owned; record `/proc/<PID>/stat` start time and confirm
  systemd still reports this MainPID and InvocationID.
- The durable admission is `checking` at **previous attempt + 1**, same boot
  and full plan; storage remains `ready`, package journal unchanged.

Use a Linux pidfd for the two signals, retaining that descriptor from before
the first signal until completion. The exact Python primitives are:

```python
process_fd = os.pidfd_open(main_pid, 0)
signal.pidfd_send_signal(process_fd, signal.SIGSTOP, None, 0)
# Revalidate the stopped process, unit and state before permitting SIGKILL.
signal.pidfd_send_signal(process_fd, signal.SIGKILL, None, 0)
```

These are primitives for the guarded observer, **not** a standalone unguarded
kill script. Do not fall back to signalling a numeric PID after pidfd failure.
After SIGSTOP, wait only a short bounded interval for the process's threads to
be stopped, then repeat the identity/unit checks and re-read admission. The
coordinator could have advanced between the first read and the stop: if the
phase is already `admitted`, the process disappeared, the attempt/plan differs,
or any check fails, do not send SIGKILL. Resume only that same held process using
`signal.pidfd_send_signal(process_fd, signal.SIGCONT, None, 0)` where it still
exists, close the pidfd, and classify the attempt as **not exercised**. A helper
must put this bounded cleanup in `finally` so failed checks do not leave a
verified process stopped. Missing the short window is not a failed product test
and must not justify an unsafe broader kill.

Only after the stopped snapshot proves `checking` may SIGKILL target that one
MainPID. Never kill the service cgroup: its independent ExecStopPost process must
be allowed to run. Do not manually run quarantine during the observation period,
since that would conceal whether supervision actually performed cleanup.

## Required post-failure evidence

Allow the real service stop/cleanup budget to finish, while collecting the unit
journal and output of the waiting public rerun. The unit uses `--collect`, so it
can disappear after completion; missing late `systemctl show` output alone is
neither proof of cleanup nor a test failure. Preserve its pre-kill properties,
invocation-specific journal and observable before/after consumer state.

```sh
sudo journalctl --no-pager -u "$unit" --since "$observed_start" -o short-iso
sudo /usr/sbin/nft -s -n -y list table inet stackfort_install_admission
sudo systemctl show --property=LoadState,ActiveState \
  stackfort-panel-renew.timer stackfort-panel-renew.service nginx.service \
  stackfort-api.service stackfort-phpmyadmin.service stackfort-agent.service \
  vinyl.service mariadb.service php8.4-fpm.service
sudo /var/lib/stackfort-installer/native-runtime-installer native status --format=json
sudo /var/lib/stackfort-installer/native-runtime-installer native recovery-plan --format=json
```

The exact nft table must retain comment
`stackfort-install-admission-v1:<operation>`, its input chain at priority -150,
and non-loopback TCP **and** UDP drops for `{ 80, 443, 8443 }`; do not accept a
substring match or merely the table's presence. The named loaded consumers must
be inactive/failed, including consumers observed active before the kill. Test
closed public access from the independent host for supported IPv4/IPv6 paths;
loopback and SSH are intentionally not blocked. Record the main-process signal
failure and successful cleanup where exposed. No successful
`NATIVE_COMPLETED_RESULT=` may be reported by this interrupted rerun.

Expected durable outcome: storage stays `ready`; package journal and bound source
are byte-for-byte unchanged; admission stays **`checking`**, attempt + 1.
SIGKILL skips Go defers, and ExecStopPost quarantines but does not rewrite that
record to `recovery-required`. A later ordinary public rerun must reject this
partial admission without issuing a setup code, rebooting, converting storage,
opening the web gate or consuming an approval. `native recovery-plan` should
classify it as `admission-review` and exit 2; plain `native status` can still exit 0
because it reports valid recorded state, not live health.

Do not claim successful recovery from this test. `native approve-recovery --yes
--state-sha256=<reviewed> --package-sha256=<reviewed>` only queues a state-bound
approval; public resume remains disabled. A separately authorized supervisor
recovery test would require its own full source/storage/gate verification and
consumption evidence. Otherwise preserve the failed fixture and use the separately
approved checkpoint cleanup or full-OS reprovision procedure; a checkpoint restore
does not satisfy the experimental beta's removal qualification.

Source references:
[`native_completed.go`](../../internal/installapply/native_completed.go),
[`native_completed_linux.go`](../../internal/installapply/native_completed_linux.go),
[`native_runtime_linux.go`](../../internal/installapply/native_runtime_linux.go),
[`admission.go`](../../internal/installapply/admission.go),
[`admission_gate_linux.go`](../../internal/installapply/admission_gate_linux.go),
[`native_operator_linux.go`](../../internal/installapply/native_operator_linux.go).
