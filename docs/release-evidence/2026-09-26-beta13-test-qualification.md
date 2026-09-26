# Beta.13 unpublished test-qualification authorization

On 2026-09-26 RTBGG explicitly approved integrating PR #17 into main, adding
mechanical candidate metadata and setting `v0.1.0-beta.13` to the already-built
fix commit, answering: "Ja, für die unveröffentlichte Testqualifikation vorbereiten".

This applies to source `991f7df6b27093588a69d0dbea2e3eab7a7ceaae`,
[build 36235431432](https://github.com/RTBGG/Stackfort/actions/runs/36235431432),
attempt 1, artifact `10904191959`, and release archive SHA-256
`b56104eebc6f535d88d9b3de17bcf95efa32c97dc88997b1e992619725c2ea91`.

This is **not publication approval**. Existing publication gates and the public
Beta.12 one-line default remain unchanged. Do not create readiness approval
evidence, claim upgrade support, republish immutable assets, move an existing
tag, or repair/retry the operator's stopped Beta.12 installation under this
instruction.

See the [candidate verification and remaining tests](../../infra/host-tests/results/2026-09-26-beta13-candidate-qualification.md).
