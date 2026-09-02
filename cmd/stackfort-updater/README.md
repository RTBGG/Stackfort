# Stackfort updater

`stackfort-updater` is the root-only staged release activator. It accepts only
an exact canonical immutable GitHub Release version, verifies the complete
release asset gate, local SHA-256, and an exact-tag GitHub provenance bundle
without a server-side GitHub token, then runs the journaled
migration/health/rollback transaction.

```sh
sudo /usr/local/sbin/stackfort-updater apply --version=1.2.3 --yes
sudo /usr/local/sbin/stackfort-updater status
```

The updater never selects an arbitrary branch or moving URL and does not enable
automatic functional updates. An interrupted activation is rolled back before
an explicit retry is accepted.
