# Beta.14 unpublished test-qualification authorization

On 2026-09-26 RTBGG answered **"Ja und bitte auch für den öffentlichen
One-Liner vorbereiten."** to this request:

> Darf ich nach erfolgreichen Prüfungen PR #18 nach `main` übernehmen, die
> tatsächlichen Build-Metadaten festhalten und `v0.1.0-beta.14` auf den gebauten
> Commit setzen? Das liefert den taggebundenen Herkunftsnachweis für den echten
> Installationstest; Beta.14 wird dadurch noch nicht veröffentlicht und der
> öffentliche One-Liner bleibt bei Beta.13.

This authorizes integrating [PR #18](https://github.com/RTBGG/Stackfort/pull/18),
recording the actual candidate identity, tagging the already-built source and
preparing the public one-line installation path after qualification. It is
**not final publication approval** and does not authorize an in-place upgrade.

The fixed source is `3681c2fb2bf5053ac2e6eafc6727d62ec1c1ffda`,
[build 36257102618](https://github.com/RTBGG/Stackfort/actions/runs/36257102618),
attempt 1, artifact `10911073741`, with release archive SHA-256
`7e6a35ba4223fd8dbb8c485f56eacde43289fe4d5da175f11a0a46538410fe67`.

Keep the public selector on Beta.13 until candidate-specific technical checks,
scope/publication approval and verification of published immutable assets are
complete. Do not fabricate readiness evidence, move an existing tag, rebuild
under an existing candidate identity or alter the operator's working Beta.13 VPS.

See the [candidate verification and remaining tests](../../infra/host-tests/results/2026-09-26-beta14-candidate-qualification.md).
