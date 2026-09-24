#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Candidate-specific exception authorized by RTBGG. Never rebuild or move a tag.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[[ "${GITHUB_EVENT_NAME:-}" == workflow_dispatch && "${GITHUB_REPOSITORY:-}" == RTBGG/Stackfort ]]
[[ "${GITHUB_REF:-}" == refs/heads/main && "${GITHUB_SHA:-}" =~ ^[0-9a-f]{40}$ ]]
[[ "$(git rev-parse HEAD)" == "$GITHUB_SHA" && "${PUBLISH_BETA11:-}" =~ ^(true|false)$ ]]
version=0.1.0-beta.11
commit=ec117da052b18e051870a224b89bee4b0ddf64e7
tag="v$version"
[[ "$(git rev-parse "refs/tags/$tag^{commit}")" == "$commit" ]]
git merge-base --is-ancestor "$commit" "$GITHUB_SHA"
[[ ! -e dist && ! -L dist ]]
workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT
api() { gh api -H 'X-GitHub-Api-Version: 2026-03-10' "$@"; }
artifacts() {
  api --paginate --slurp "repos/RTBGG/Stackfort/actions/runs/$1/artifacts?per_page=100" |
    jq -e 'if length > 0 and all(.[]; (.artifacts | type) == "array" and (.total_count | type) == "number")
      and ([.[].total_count] | unique | length) == 1
      then {total_count: .[0].total_count, artifacts: [.[].artifacts[]]}
      else error("invalid artifact pages") end'
}
# Resolve/check the real main context. No forged tag-event environment variables.
[[ "$(api repos/RTBGG/Stackfort/git/ref/heads/main --jq '.object.sha')" == "$GITHUB_SHA" ]]
api repos/RTBGG/Stackfort/actions/runs/35991617289 >"$workspace/build.json"
artifacts 35991617289 >"$workspace/build-artifacts.json"
node scripts/promote-release-candidate.mjs --mode verify \
  --promotion "packaging/releases/promotion/$version.json" --version "$version" --commit "$commit" \
  --run "$workspace/build.json" --artifacts "$workspace/build-artifacts.json" >"$workspace/promotion.json"
api repos/RTBGG/Stackfort/actions/runs/35993227841 >"$workspace/tag-run.json"
artifacts 35993227841 >"$workspace/tag-artifacts.json"
node scripts/verify-beta11-fresh-scope.mjs "$workspace/promotion.json" \
  docs/release-evidence/2026-09-24-beta11-fresh-install-scope.md \
  "$workspace/tag-run.json" "$workspace/tag-artifacts.json" >"$workspace/upgrade-support.json"
api repos/RTBGG/Stackfort/actions/artifacts/10804961581/zip >"$workspace/original.zip"
python3 scripts/extract-release-candidate.py "$workspace/promotion.json" "$workspace/original.zip" "$workspace/original"
api repos/RTBGG/Stackfort/actions/artifacts/10805515238/zip >"$workspace/tag.zip"
python3 scripts/extract-tag-qualified-candidate.py "$workspace/tag.zip" \
  fe4ad0a2656207e0af5e8696138d0b4e9c00c34d899fe319513653e5fe3e2c00 \
  "$workspace/original" "$workspace/promotion.json" dist >"$workspace/tag-extraction.json"
[[ "$(sha256sum dist/build-attestation.jsonl | cut -d ' ' -f 1)" == 16570b5f6f4f8e1bc35f5ac31c80b5194a924956e6337df67953236470a37d1f ]]
archive="dist/stackfort-$version-linux-amd64.tar.gz"
archive_digest="$(sha256sum "$archive" | cut -d ' ' -f 1)"
# Cryptographic verification is distinct from ZIP/inventory validation.
# https://cli.github.com/manual/gh_attestation_verify
gh attestation verify "$archive" --bundle dist/build-attestation.jsonl --repo RTBGG/Stackfort \
  --signer-workflow RTBGG/Stackfort/.github/workflows/release.yml \
  --source-ref "refs/tags/$tag" --source-digest "$commit" --signer-digest "$commit" \
  --deny-self-hosted-runners >/dev/null

# Run the ORIGINAL candidate's pure readiness validator and unchanged policy.
# Only the explicit predecessor exception is different; no technical check is waived.
git show "$commit:scripts/verify-release-readiness.mjs" >"$workspace/readiness.mjs"
git show "$commit:packaging/releases/readiness-policy.json" >"$workspace/policy.json"
git show "$commit:SECURITY.md" >"$workspace/SECURITY.md"
cmp packaging/releases/readiness-policy.json "$workspace/policy.json"
evidence="packaging/releases/evidence/$version.json"
jq -e --slurpfile promotion "$workspace/promotion.json" '.candidate == $promotion[0].candidate' "$evidence" >/dev/null
arguments=(--policy "$workspace/policy.json" --evidence "$evidence" --version "$version" --commit "$commit" --archive-sha256 "$archive_digest")
node "$workspace/readiness.mjs" --mode inventory "${arguments[@]}" >"$workspace/inventory.json"
index=0
while IFS= read -r report; do
  git show "$GITHUB_SHA:$report" >"$workspace/report-$index"
  index=$((index + 1))
done < <(jq -r '.reports[].path' "$workspace/inventory.json")
while IFS=$'\t' read -r run_id attempt; do
  api "repos/RTBGG/Stackfort/actions/runs/$run_id" >"$workspace/run-$run_id.json"
  api --paginate --slurp "repos/RTBGG/Stackfort/actions/runs/$run_id/attempts/$attempt/jobs?per_page=100" |
    jq -e 'if length > 0 and all(.[]; (.jobs | type) == "array" and (.total_count | type) == "number")
      and ([.[].total_count] | unique | length) == 1
      then {total_count: .[0].total_count, jobs: [.[].jobs[]]}
      else error("invalid job pages") end' >"$workspace/jobs-$run_id.json"
done < <(jq -r '.workflowRuns[] | [.runId, .attempt] | @tsv' "$workspace/inventory.json")
node "$workspace/readiness.mjs" --mode verify "${arguments[@]}" --evidence-commit "$GITHUB_SHA" \
  --input-directory "$workspace" --security-policy "$workspace/SECURITY.md" >dist/release-readiness.json
install -m 0644 "$workspace/upgrade-support.json" dist/upgrade-support.json
printf 'All unchanged technical gates passed for retained Beta.11. Upgrade support is explicitly absent.\n'
if [[ "$PUBLISH_BETA11" != true ]]; then
  printf 'Verification only: no public release or tag mutation.\n'
  exit 0
fi
# The server refuses duplicate tags; never delete, overwrite or retry with new bytes.
if gh release view "$tag" --repo RTBGG/Stackfort >/dev/null 2>&1; then
  printf 'Release already exists; refusing mutation.\n' >&2
  exit 1
fi
{
  jq -r '.independentReviewDisclosure' dist/release-readiness.json
  printf '\nFresh disposable Debian 13 amd64 test servers only, using the qualified ext4/GRUB GPT/UEFI or primary-MBR/BIOS profile. Not for production or important data.\n\n'
  jq -r '.disclosure' dist/upgrade-support.json
  printf '\nCommunity-only GitHub support; no guaranteed response, fixes, SLA or support period.\n'
  printf '\nHosting quotas share the root filesystem. Aggregate capacity admission and a guaranteed OS disk/inode reserve are not implemented. Exhaustion may require OS reinstallation.\n\n'
  jq -r '.removalDisclosure' dist/release-readiness.json
  printf '\nThe original packages, source tag and tag-origin attestation are unchanged. No updater fix from later main commits is included.\n'
} >"$workspace/notes.md"
gh release create "$tag" dist/* --repo RTBGG/Stackfort --verify-tag --prerelease --latest=false \
  --title 'Stackfort v0.1.0-beta.11 — experimental, fresh installs only' --notes-file "$workspace/notes.md"
gh release view "$tag" --repo RTBGG/Stackfort --json tagName,isDraft,isPrerelease,isImmutable |
  jq -e --arg tag "$tag" '.tagName == $tag and .isDraft == false and .isPrerelease == true and .isImmutable == true' >/dev/null
gh release verify "$tag" --repo RTBGG/Stackfort >/dev/null
