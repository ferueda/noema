# Semantic-claim evaluation corpus

`corpus-v1.json` is the single reviewed synthetic corpus for the V0 semantic
claim baseline. It contains no private Sessions data and is accepted only when
its exact content digest matches the value compiled into
`cmd/noema-semantic-eval`.

The live runner invokes each case once through Noema's production semantic
preflight, prompt, schema, privacy rules, Gateway adapter, and local claim
admission. Machine expectations check bounded structural outcomes. They do not
judge whether natural-language claims are actually supported or useful.

The runner writes two local files outside the repository:

- An immutable machine report with admitted claims and bounded execution
  metadata.
- A review template whose claim and case-criterion decisions begin as
  `unreviewed`.

Review every admitted claim against its synthetic evidence:

- Evidence support: `supported`, `partly-supported`, or `unsupported`.
- Usefulness: `useful`, `weak`, or `not-useful`.

Review every case criterion:

- `pass`: the criterion is fully satisfied.
- `partial`: the result is mixed or incomplete.
- `fail`: the criterion is violated.

Notes are optional and bounded. The offline score command rejects stale corpus,
claim, case, or criterion identities and reports missing decisions as
unreviewed. It does not treat missing reviews as failures or use a model judge.

Do not replace, extend, or regenerate this corpus merely to improve a score.
Changes require a new reviewed corpus version and digest.
