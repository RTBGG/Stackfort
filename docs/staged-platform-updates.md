# Staged platform updates

Stackfort supports explicit administrator-triggered updates to a newer release
that the configured stable or beta channel has already accepted. Automatic
release checks are enabled by default; automatic installation is not.

## Administrator workflow

1. Open **Administration → Updates** and run **Check now** if needed.
2. Review the exact immutable GitHub Release and its channel.
3. Confirm the brief control-panel and hosted-service interruption.
4. Select **Stage and install update**.
5. Leave the page open or return later. The page reconnects after the API
   restart and displays the durable transaction state.

The request returns HTTP 202 before host mutation begins. A successful update
ends in `complete`. A failed safety gate normally ends in `rolled_back`, which
means the prior binaries, native packages, configuration, services, and panel
database snapshot were restored. `rollback_failed` requires operator review.

## Root operator commands

```console
sudo stackfort-updater status
sudo systemctl status 'stackfort-update@1.2.3.service'
sudo journalctl -u 'stackfort-update@1.2.3.service'
```

The updater state is deliberately separate from the panel database:

- journal: `/var/lib/stackfort-updater/update-state.json`
- verified releases: `/var/lib/stackfort-updater/releases/`
- database snapshots: `/var/lib/stackfort-updater/backups/`
- serializer lock: `/var/lib/stackfort-updater/update.lock`

These paths are root-owned and private. Do not edit the journal or provenance
records. Correct the underlying host problem and start the same target version
again to invoke fail-closed recovery. A different target is refused while
recovery is non-terminal.

The equivalent direct, explicit root command is:

```console
sudo stackfort-updater apply --version=1.2.3 --yes --format=text
```

It does not bypass release immutability, digest, attestation, version, locking,
staging, migration, health, or rollback checks.

## Transaction stages

| Stage | Purpose |
| --- | --- |
| `stop-services` | Quiesce the API, agent, and phpMyAdmin control runtime. |
| `backup-state` | Create a consistent SQLite snapshot after quiescence. |
| `native-packages` | Install the target's exact Coraza/NGINX and Vinyl packages. |
| `release-payload` | Activate the verified target binaries and web assets. |
| `system-configuration` | Reconcile root-owned units, policy, and NGINX configuration. |
| `database-migration` | Run checksum-locked migrations with the target API binary. |
| `start-services` | Restart native dependencies and Stackfort services. |
| `health-gate` | Verify the complete installed release before commit. |

Each stage transition is persisted before and after mutation. Any failure after
mutation invokes exact rollback under an independent five-minute context.

## Supply-chain boundary

The updater only queries exact tags in `RTBGG/Stackfort`. It requires the full
release inventory, GitHub SHA-256 asset digests, the matching checksum manifest,
a safe bounded archive, and a repository-scoped provenance bundle fetched from
GitHub's public attestation API by that archive digest. The compressed bundle is
size-bounded, decoded into a private file, and verified locally with
`gh attestation verify --bundle`. Verification pins `RTBGG/Stackfort`, the exact
release tag, the release workflow, and GitHub-hosted runners. It needs no GitHub
account token. The trusted GitHub CLI is shipped in the same inspected Stackfort
release rather than downloaded at update time.

See [ADR 0061](adr/0061-attested-health-gated-platform-updates.md) for the
security decision and [update channels and checks](update-channels-and-checks.md)
for release eligibility. The persistent transaction's three-distribution
[qualification record](../infra/host-tests/results/2026-09-02-staged-update-transaction-hyper-v.md)
covers success, health-gated rollback, database restoration, and interruption
recovery. Published-release-to-release matrices remain the next roadmap item.
