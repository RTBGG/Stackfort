# Beta.11: maintainer-authorized fresh-install-only scope

Recorded 2026-09-24. RTBGG requested publication of Beta.11 and explicitly
confirmed preparing it **only for new installations on fresh test servers,
without Beta.10 upgrade support**, with the remaining technical checks completed
before publication. This is conditional publication authorization, not a claim
that the release-readiness gate has passed.

The decision applies only to this frozen candidate:

- Version: `0.1.0-beta.11`.
- Source/tag target: `ec117da052b18e051870a224b89bee4b0ddf64e7`.
- Archive SHA-256: `d6fec6d5a894d0c120aef5d83829b335a8875b68a181c45a7ad2bb9d139d1304`.
- Original retained build: run `35991617289`, attempt 1, artifact `10804961581`.
- Retained ZIP SHA-256: `97c6acdb9aaa6695c880ab9a54c26724d3182394989ef38b7ae580d09ebe2c9e`.

## Limits

Experimental Debian 13 amd64 testing only, on fresh disposable servers without
important data. No production approval, independent security review, guaranteed
support response or fixes. Community support is through GitHub issues and private
security reporting. Removal requires complete OS reinstallation, destroying all
server data; removing a passive package does not uninstall the platform.

No upgrade from Beta.10 or another installed release is supported by this
candidate. Do not select Beta.11 in the updater or run its installer over an older
installation. The observed predecessor asset-name mismatch and nine unqualified
upgrade cells remain in the
[candidate report](../../infra/host-tests/results/2026-09-24-beta11-candidate-qualification.md).
This decision does not retire Beta.10 globally or turn those cells into passes.
The later source fix is not present in the frozen Beta.11 payload.

## Publication requirements remain

The unchanged candidate still requires exact-source CI/security results and
complete fresh-install, rerun, reboot, installed-host security, tenant/resource,
WAF/cache, rootless OCI, failure containment and full same-target OS-reprovision
evidence. A missing or failed technical result continues to block publication.
Retain the original archive, tag target and genuine tag-origin attestation;
never rebuild or replace them under this version.

The old tag workflow's upgrade gate remains blocked. Any narrowly scoped
fresh-install publication path must bind this explicit exception to the exact
candidate, retain the remaining readiness checks, and publish an honest
unsupported-upgrade disclosure rather than a successful upgrade-matrix receipt.
The public one-line selector must not change before release assets exist and
their public downloads have been verified.
