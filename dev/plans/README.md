# Implementation plans

Plans in this directory describe accepted work that is ready for review or
implementation.

## Active

- [260721-milestone-2-semantic-claims.md](260721-milestone-2-semantic-claims.md) —
  the remote claim path and live route conformance are implemented; reviewed
  semantic evaluation remains before the milestone closes.
- [260722-semantic-evaluation-corpus.md](260722-semantic-evaluation-corpus.md) —
  establish a 12-case synthetic corpus with machine metrics and human review.

## Queued

- [260722-incremental-evidence-windows.md](260722-incremental-evidence-windows.md) —
  after V0 feedback and the knowledge-unit checkpoint, plan one growing session
  into deterministic windows and avoid repeated semantic calls for unchanged
  work.

## Superseded

- [260719-first-vertical-slice.md](260719-first-vertical-slice.md) — partially
  implemented the runtime foundation, then superseded when the remaining V0 was
  split into the evidence, semantic-claim, and Content Scout milestones in the
  [product roadmap](../../docs/roadmap.md).

## Completed

- [260722-live-semantic-route-conformance.md](260722-live-semantic-route-conformance.md) —
  adds and validates the public-data-only manual canary for the pinned Gateway
  route and production schema.
- [260722-pin-semantic-generation-settings.md](260722-pin-semantic-generation-settings.md) —
  pins temperature zero in the strict semantic route and reuse identity.
- [260721-semantic-durability-cleanup.md](260721-semantic-durability-cleanup.md) —
  simplified the durability internals without changing durable behavior.
- [260720-milestone-1-evidence-facts.md](260720-milestone-1-evidence-facts.md) —
  process one explicit Sessions snapshot into deterministic, inspectable facts
  without a model call.
