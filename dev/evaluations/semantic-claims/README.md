# Semantic-claim evaluation corpus

The evaluator accepts a closed set of reviewed synthetic corpora only when
their exact content digests match values compiled into
`cmd/noema-semantic-eval`:

- `corpus-v1.json` is the immutable 12-case V0 baseline.
- `corpus-v2.json` preserves all V1 cases exactly and adds eight cases for
  scope, causality, chronology, separation, decision state, rationale, and
  prompt injection inside quoted evidence.

Neither corpus contains private Sessions data.

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

Do not replace, extend, or regenerate a reviewed corpus merely to improve a
score. Changes require a new reviewed corpus file, review, and compiled digest.
