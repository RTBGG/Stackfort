# Public release readiness contract v1

The tag-publishing workflow is **blocked by default**. This directory contains
policy, not release authorization. There is intentionally no readiness evidence
file until the checks and real human decisions below have been completed.
Manual release workflow dispatch still builds and attests an unpublished
candidate; it neither runs this publication gate nor claims it passed. A tag run
promotes those **exact retained bytes without rebuilding**, creates an unpublished
tag-signed qualification artifact, and only then evaluates publication evidence.
See the [two-phase promotion contract](PROMOTION.md).

This gate supplements the [release checklist](../../docs/release-checklist.md),
[security release gates](../../docs/security.md#6-security-release-gates) and
[upgrade matrix](../../docs/upgrade-matrix.md). A successful CI run is not an
independent security review. The validator checks the presence, consistency and
integrity of **recorded** decisions; maintainers must verify any claimed reviewer's
identity, actual independence, findings and authority. RTBGG explicitly authorized
an **experimental-beta alternative on 2026-09-12** without independent review,
strictly for fresh disposable test servers, not production or important data.
That policy authorization is not candidate-specific approval or a passed test.
Do not generate approvals, empty reports or synthetic host evidence to open it.
On the same date, RTBGG explicitly approved **complete operating-system
reinstallation as the experimental beta's only removal method**, instead of
requiring an unavailable in-place uninstaller. This irreversibly removes all
server data, configuration and services. A real exact-candidate reprovisioning
test remains mandatory; passive package removal or snapshot rollback is not one.
Reviewed releases retain the `active-uninstall` requirement. See the
[removal scope and qualification procedure](../../docs/experimental-beta-removal.md).

## Candidate and reviewed evidence

1. Finalize the candidate source and advertised installation scope. The checked-in
   [policy](readiness-policy.json) explicitly limits fresh-default **one-line**
   installation to Debian 13 amd64 for the first native beta. Ubuntu/Rocky native
   conversion remains unadvertised until qualified. Historical prepared-quota
   installation on all three operating systems is a separate route, not proof of
   fresh-root installation. Future scope changes require reviewed policy and
   public documentation updates before a new candidate is built.
2. Run CI/security and build the candidate at one immutable source commit.
   Qualify the exact archive, including its own installer, on every policy cell.
   Existing evidence for a different installer or archive cannot be reused.
3. Record either a genuinely completed independent review or the authorized
   experimental-beta class with an explicit **not-performed** disclosure. Record
   community-support/version/deployment limits and RTBGG's candidate-specific
   publication approval. Publish the support policy in
   the candidate's `SECURITY.md`; its exact SHA-256 is required below.
4. Commit the real reports and canonical `evidence/<version>.json` to `main`
   **after** the tested candidate, as with upgrade evidence. The qualification
   tag remains on the tested candidate commit, not this later evidence commit;
   rerun its blocked promotion job. A candidate input change
   requires a rebuild and new qualification, not edited old hashes.
5. The tag workflow pins one `main` commit, requires it to descend from the
   candidate, and reads the evidence and reports only at that commit. Its policy
   must be byte-identical to the candidate's policy. It independently fetches the
   referenced GitHub run/job metadata and validates the exact promoted archive
   digest and retained build identity. Missing evidence may intentionally block
   the first tag run after its tag-signed qualification artifact has been uploaded.
6. Only successful verification creates `dist/release-readiness.json`, later
   included in release provenance. This is a verification receipt, not a claim
   that the program itself conducted an independent audit.

The executable contract is
[`scripts/verify-release-readiness.mjs`](../../scripts/verify-release-readiness.mjs);
the [tests](../../scripts/verify-release-readiness.test.mjs) use **synthetic
in-memory and isolated temporary fixtures**, not usable project approvals. All evidence objects
reject missing/unknown fields. Policy/evidence must use canonical JSON:
`JSON.stringify(document, null, 2) + '\n'`. This also rejects duplicate keys.
Input JSON is bounded to 1 MiB and report files to 4 MiB.

## Evidence object fields

Every field listed below is mandatory. Hashes are lowercase SHA-256, source and
evidence commits are full lowercase 40-character Git IDs, and timestamps use
`YYYY-MM-DDTHH:MM:SSZ`. A `report` is exactly `{ "path": ..., "sha256": ... }`;
it points to a real `.md` or `.json` file below `infra/host-tests/results/` or
`docs/release-evidence/`, not a URL or traversal. Reports are retrieved
from the same pinned evidence commit and checked against their hashes.

| Field | Required value or meaning |
| --- | --- |
| `schemaVersion` | `1` |
| `kind` | `release-readiness`; rehearsal results are rejected |
| `policy` | `stackfort-release-readiness-v1` |
| `releaseClass` | `reviewed-release` or the explicitly authorized `experimental-beta` |
| `candidate` | Exact `version`, `commit`, `archive`, `archiveSHA256` and retained `build` identity |
| `approvedBy`, `approvedAt` | Human GitHub identity and time of reviewed readiness approval |
| `installationResults` | Exactly one successful result per policy matrix cell |
| `workflowRuns` | Exact CI and security workflow path, positive integer `runId` and `attempt` |
| `independentReview` | Completed independent review, or explicit `not-performed` experimental disclosure; never an invented reviewer |
| `supportPolicy` | Explicit community-only approval, exact candidate, no guaranteed SLA/fixes/end date, limits, candidate security-policy hash and report |
| `publicationDecision` | RTBGG's exact-candidate class/scope approval and reviewed decision report |

`candidate.archive` must be `stackfort-<version>-linux-amd64.tar.gz`. The version
must be canonical `X.Y.Z` or `X.Y.Z-beta.N`. The validator requires the expected
version, GitHub tag commit and retained archive SHA, not just internal agreement
between reports. It does not promote a beta to stable or fix bootstrap channel
selection; first-beta installers still need an explicitly supported beta route.
`candidate.build` contains exactly positive integer `runId`, `attempt`,
`artifactId`, and `artifactSHA256` (the downloaded GitHub artifact ZIP, not the
contained `.tar.gz`). These must match the independently verified promotion.

Each `installationResults` entry has exactly:

- `cell`: the exact five-field object from the policy's `installationMatrix`;
- `result`: `pass`;
- `sourceCommit`, `archiveSHA256`: the exact candidate;
- `completedAt`: no later than readiness approval;
- `report`: digest-bound real qualification report;
- `checks`: exactly `fresh-install`, `idempotent-rerun`, `normal-reboot`,
  `host-security`, `tenant-isolation`, `quota-enforcement`, `waf-cache`,
  `rootless-oci`, `failure-recovery`, plus the class-specific removal check:
  `active-uninstall` for `reviewed-release`, or `full-system-reprovision-removal`
  for `experimental-beta`.
- `removal`: present **only** for `experimental-beta`, with the exact fields
  below. A reviewed release cannot substitute this record for active uninstall.

The experimental `removal` record requires exactly:

| Field | Required evidence |
| --- | --- |
| `method`, `result` | `full-system-reprovision`, `pass` |
| `candidate` | The complete identical candidate object, including original build run/attempt/artifact ID and ZIP SHA |
| `targetBefore`, `targetAfter` | Identical nonempty, nonsecret stable target identities, such as the same Hyper-V VM ID; maximum 256 characters each |
| `installerMediaSHA256` | Exact SHA-256 of the authenticated distribution installation media used to completely reprovision that target's OS disk |
| `checks` | Exactly `active-candidate-installed`, `authenticated-distribution-installer`, `complete-os-disk-provisioning`, `fresh-os-boot`, `stackfort-state-absent`, `stackfort-services-absent`, `hosting-data-absent` |
| `completedAt` | Real completion time no later than the enclosing installation-result completion/approval |
| `report` | Digest-bound real report documenting the active exact candidate before reprovision, media authentication, full OS installation and clean post-reinstallation observations |

The enclosing `completedAt` therefore covers the complete qualification including
removal, not merely the earlier installation. The report must tie both phases to
the same target. Creating a different clean VM, merely removing a carrier package,
stopping services, reinstalling files over the old system or rolling back a
snapshot does not satisfy complete OS disk provisioning. The validator checks
recorded identities/checks/report integrity, not the truth of an invented test.
No actual removal result is supplied by the checked-in policy or unit fixtures.

For `reviewed-release`, `independentReview` has exactly `decision: "approved"`, `reviewer`,
`independentOfImplementation: true`, `completedAt`, `scopes`, and `report`.
The reviewer must differ from the readiness approver. Scope must include exactly
`authentication`, `agent-rpc`, `file-archive`, `phpmyadmin-handoff`, `updater` and
`native-installer`. The identity/declaration is a reviewable human assertion, not
cryptographic proof of independence or an automatically performed audit.

For `experimental-beta`, the version must have canonical `-beta.N` form and
`independentReview` contains exactly `decision: "not-performed"` and `disclosure`:

> No independent security review has been performed. Experimental beta for fresh disposable test servers only; not for production or important data.

This exact warning is carried into verification receipts and generated release
notes. A reviewer, approval or independent-audit claim is rejected in that branch.
All other technical installation, security CI and upgrade evidence requirements
remain. Only the explicitly experimental removal method differs.

`supportPolicy` has exactly `decision: "approved"`, `approvedBy` (the currently
recorded maintainer, RTBGG), `versions` (the one exact candidate version), `terms`
(the policy's community-only support object), nonempty `deploymentLimits`,
`securityPolicySHA256` (the candidate's `SECURITY.md`), and `report`.
The terms require `model: "community-only"`, `maintainer: "RTBGG"`, GitHub issue
and private-security-reporting channels, `guaranteedResponse: false`,
`guaranteedFixes: false`, and `supportEnds: null`. Possible future contributors
are volunteers, not a promised staffed service. Do not invent an SLA, support
window or fix commitment to satisfy validation; none is required or accepted.
For `experimental-beta`, `supportPolicy` additionally requires
`removalMethod: "full-system-reprovision"`; its report and deployment limits must
disclose destructive whole-system reinstallation and absence of in-place removal.
`publicationDecision` has exactly `decision: "approved"`, `approvedBy: "RTBGG"`,
the same `releaseClass`, `freshDisposableOnly: true`, `productionUseAllowed: false`,
`importantDataAllowed: false`, and `report`. For `experimental-beta`, it additionally
requires `removalMethod: "full-system-reprovision"`, so a candidate-specific approval
cannot omit that removal scope. These scope restrictions apply to
both currently supported classes; neither is a production release contract.
GitHub-like identity syntax does not independently verify authority. The general
2026-09-12 policy authorization is not this candidate-specific approval record.

No independent review has been completed and no professional paid audit is funded.
A genuinely independent voluntary/community review is welcome. Its approval must
not be fabricated from automated or agent checks. The explicitly authorized
experimental path accurately discloses that this review has not happened.

Verified receipts record the selected `removalMethod` and `removalDisclosure`;
the release workflow renders that validated disclosure into release notes.
The changed removal policy is a new candidate input. The earlier
`74feaf628e39e8a809bc9a8899fc0af0bb127ff0` build is rehearsal only for this contract;
do not reuse its archive, policy or evidence as if this change were included.

## Live workflow verification

`workflowRuns` contains `.github/workflows/ci.yml` and
`.github/workflows/security.yml`, each exactly once. The gate fetches current run
metadata and **all pages** of jobs for the recorded attempt from GitHub. It requires
the same repository and source repository, exact candidate commit and workflow,
`push` or `workflow_dispatch`, completed/successful state and the latest attempt.
An older passing attempt cannot conceal a failed or running rerun.

CI requires successful `Workflow hygiene`, `Go`, `Web` and `Reproducible artifacts`
jobs. Security requires successful `Secret scanning`, `Go vulnerability analysis`,
`CodeQL (go)` and `CodeQL (javascript-typescript)`. Only the PR-specific
`Dependency review` job may be skipped on these non-PR runs. Missing, duplicate,
unknown, stale or truncated jobs block publication; review this contract when
renaming or changing required jobs. Workflow results are fetched live, not trusted
from a checked-in `success: true` flag.

Network/API failure, absent evidence, changed report/policy bytes, absent review
without the explicit experimental branch, invented support guarantees or any
failed technical validation stops before publication.
The gate does not overwrite reviewed evidence, fabricate a record, publish a tag
or release, modify repository settings, or bypass any upgrade-matrix requirement.
