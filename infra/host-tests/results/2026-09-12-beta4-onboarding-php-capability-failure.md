# Exact beta.4 onboarding: PHP capability failure — 2026-09-12

Result: **failed qualification; do not publish this candidate**. Storage
preparation and installation completed, but the installed-product API smoke
could not select the installed PHP runtime. No passing readiness or publication
approval record was created.

## Exact retained bytes

- Version: `0.1.0-beta.4`; source: `595bfe56670a546109fe61d888fcf667849e54dc`.
- Manual build: `34684631255`, attempt 1, successful; artifact `10294594766`.
- Original ZIP SHA-256: `c5f9ccc143663f77f61ee5491de3240b7f3363571a9304d098b65ace244cc746`.
- Archive SHA-256: `fe86f2f20397279556ffc60b592211e8f9fe43af420b2d44d1633bbcdf106507`.
- Archived/standalone installer SHA-256:
  `ed896db3ec71a679d203a1caa4c7116e54601fa5d88bac42d762854062c49666`.
- Tag job: `34685008739`, attempt 1; unpublished artifact `10295840047`.
- Tag ZIP SHA-256: `afca2dd17659066d80d31dfd29a9beb45da4c6cb7efe3359be4e6cdac2d6d075`.
- Tag attestation bundle SHA-256:
  `21781381bb3be6fec085856c92f6ba1602565ee1b6358847b0ad6ed44d68ea3b`.
- Bootstrap SHA-256:
  `fcefe96faa159f6da36811de9eff03525fbbf7925fff575040e50714f1ceab3f`.

The exact-tag workflow reused all ten original payload files byte-for-byte,
issued genuine tag provenance and retained its unpublished artifact. The
publication gate then correctly stopped because readiness evidence did not exist
(HTTP 404). The tag still identifies this failed candidate and must not be moved.

## Actual host and observed progress

Disposable Hyper-V VM ID `4361f439-15e9-4f9e-a690-9a8e44b6cbd3`,
DMI `6365bd88-5141-4f15-b3f8-2ba9996baad2`, Debian 13 amd64,
kernel `6.12.107+deb13-cloud-amd64`, 2 vCPU, 8 GiB RAM and 50 GiB plain
GPT/ext4 root. The restored laboratory baseline had build/Podman packages;
this was not a pristine vendor-image package inventory.

The real controlling-terminal harness started at `2026-09-12T09:10:26Z`.
It used the unmodified archived installer and strict `tag-release` provenance;
only the asset transport used the explicitly opted-in root-owned local fixture.
It did not use a laboratory origin exception, generic consent bypass or a
replacement installer.

Observed before failure:

- Prerequisite installation completed for nftables and quota.
- Real terminal source/host review, fresh-disposable acknowledgement, reboot
  acknowledgement and saving the original setup code completed.
- One authorized conversion boot enabled ext4 project quotas automatically.
  Storage became `ready`, with `armAttempts=1`.
- Package installation completed, admission became `admitted`, and the API,
  agent, NGINX, PHP-FPM and MariaDB services started.
- Original setup redemption returned 201; replay returned 409; administrator
  login and session checks both returned 200.
- Host capabilities GET returned 200, and the real agent recorded a successful
  `host.capabilities.inspect` RPC. The API smoke then failed before any package
  or hosting-account creation.

Operation: `56650439-0eed-4a2c-a449-8fc38d1755bc`.
Initial boot: `2632937d-df36-4200-9f86-556eb19e18df`;
conversion boot: `2b3b4e95-8435-4835-a425-ae23abd8d598`.
Storage root UUID: `a88eaa57-e875-4855-a3cb-c231758653f8`;
partition UUID: `54964bdf-2add-41b7-b41e-6d483962d021`.

## Diagnosed product defect

A subsequent read-only request to the installed agent, made as its authorized
control UID through the existing Unix socket, reported Debian/systemd available
but PHP as:

```json
{"key":"php-fpm","packageName":"php-fpm","availability":{"status":"unknown","reasonCode":"package-query-failed"}}
```

The installed native package was actually `php8.4-fpm 8.4.24-1~deb13u1`.
The detector queried the generic `php-fpm` package, while
`phpworkspace.HostRuntime` requires the exact approved native runtime package
(`php8.4-fpm` on Debian 13). Installing the runtime therefore did not make it
selectable in the product. The fix must use the shared approved PHP profile and
test the real detector/report-to-workspace boundary, not merely weaken the smoke
assertion or install a metapackage.

The outer credential-owning C# harness also concealed the already-sanitized
PowerShell substage diagnostic. A test-only logging improvement will preserve
that local diagnostic without exposing response bodies, credentials or exception
chains.

## Preserved state and limits

The failed installed host is preserved in checkpoint
`native-beta4-595bfe5-installed-php-detection-failure`,
ID `8ccd87d3-3d9b-4208-8283-cf1e06ae9666`, created 2026-09-12 09:15 UTC.
Ignored local evidence resides under
`infra/host-tests/work/candidates/34685008739-attempt1/`:
`failed-onboard.json`, `failed-native-status.json` and
`capabilities-failure-readonly.json`. Setup/password/session material and raw
terminal transcripts were not retained.

Retained evidence SHA-256:

- `failed-onboard.json`: `44f228957eab8974b7f240523b669b5a6e67dc298b686cc0e96dd667fcd14757`.
- `failed-native-status.json`: `7d68681e23d10eed2ffd7af3a7f8cf5388d0351179fb186adac90b8838f8a17d`.
- `capabilities-failure-readonly.json`: `fe4b0d143f9949ee6b94267c54b8b25033afe63fbacdbebf6b2da1c80c1123fd`.

Same-release rerun, normal reboot persistence, tenant/file/backup/database/WAF
product smoke, real rootless image preparation, fault recovery and full-OS
removal were **not completed by this run**. The new source requires a new
candidate/version/build and new exact-candidate evidence; the successful partial
observations above cannot satisfy that future candidate's readiness gates.

## Subsequent fix probe, not candidate qualification

The detector and the privileged command-query allowlist now both derive their
exact PHP package/service names from the shared `phpruntime` profiles. Regression
tests cover Debian, Ubuntu and Rocky, missing native runtimes despite a generic
metapackage, downstream managed-version selection and rejected wildcard queries.
The Windows `go test ./...` suite passed.

On the same installed Debian host, a separate read-only Linux probe compiled from
the corrected working tree used the production inspector and command runner.
It reported `php8.4-fpm 8.4.24-1~deb13u1` available, managed versions `["8.4"]`
and PHP readiness available. The vendor unit was disabled/inactive, as expected
for managed per-account pools; package availability is the runtime criterion.
No installed binary or platform configuration was replaced by this probe.

Probe SHA-256: `443574e7642491c7bcd6ea7ebbe04a006fe818baa0575d7625530a417c4b7eb4`.
Redacted `work/php-capability-fix-v1/live-result.json` SHA-256:
`ef1027643a72e7c03a2ecc76779ce83f14f41e1cadc67bee14a99310794cdf49`.
This establishes the real-query correction only; beta.5 still needs a fresh
artifact build and its complete installed-product qualification.
