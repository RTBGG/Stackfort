# Staged update transaction qualification — Hyper-V — 2026-09-02

Phase 6's persistent update transaction passed on all three supported `amd64`
guests. The same cross-compiled test binary exercised the production update
engine and root-owned file journal on every guest.

## Qualified guests

| Guest | VM | Result |
| --- | --- | --- |
| Debian GNU/Linux 13 | `stackfort-debian-13` | passed |
| Ubuntu 26.04 LTS | `stackfort-ubuntu-26-04-v2` | passed |
| Rocky Linux 10 | `stackfort-rocky-10` | passed |

All three VMs were returned to `Off` after qualification.

## Evidence

Each guest ran as root with an isolated native SQLite database and private
`0700` updater state directory. The test covered:

- all eight ordered stages from service quiescence through the final health
  gate, with every transition saved through the production `FileStore`;
- a successful schema-1 to schema-2 migration and terminal `complete` journal;
- an injected final health failure after native-package, payload,
  configuration, and database changes;
- exact restoration of the schema-1 SQLite snapshot and all simulated active
  release surfaces, followed by a terminal `rolled_back` journal;
- process-loss simulation after the migration stage, construction of a fresh
  engine/runner process, refusal to continue forward, and fail-closed recovery
  to the exact prior state; and
- private regular `0600` journal metadata after commit and both rollback paths;
- a genuinely read-only idle status inspection that did not create the absent
  updater state directory; and
- on Debian 13, the actual bundled GitHub CLI 2.99.0 under the updater unit's
  `ProtectHome`, private temporary directory, empty credential environment, and
  address-family restrictions. It locally verified a public, digest-selected
  SLSA bundle with repository, exact tag, release workflow, and hosted-runner
  policy and emitted
  `STACKFORT_GH_UNAUTHENTICATED_OFFLINE_ATTESTATION=passed`.

Every guest emitted the required marker:

```text
STACKFORT_QUALIFICATION staged-update-transaction=passed
```

The wrapper command was:

```powershell
.\infra\host-tests\Test-StackfortUpdateTransactionHyperVVm.ps1 `
  -ImageId <debian-13|ubuntu-26.04|rocky-10> -VmName <guest>
```

## Scope

This record closes the staged transaction, migration, health-check, and
rollback roadmap item. The normal updater still accepts only complete immutable
GitHub Releases whose archive digest and repository-scoped, exact-tag
provenance verify locally; no local release seam was added for this test.
End-to-end upgrades from every published supported prior release remain the
next, separate Phase 6 roadmap item.
