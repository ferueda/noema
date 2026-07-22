# Implementation plans

Plans in this directory describe accepted work that is ready for review or
implementation.

## Active

- [260721-semantic-durability-cleanup.md](260721-semantic-durability-cleanup.md) —
  simplify the implemented durability internals without changing durable
  behavior, then begin remote model work.
- [260721-milestone-2-semantic-claims.md](260721-milestone-2-semantic-claims.md) —
  admission and durability are implemented; add the explicitly approved remote
  model request after the bounded cleanup above.

## Superseded

- [260719-first-vertical-slice.md](260719-first-vertical-slice.md) — partially
  implemented the runtime foundation, then superseded when the remaining V0 was
  split into the evidence, semantic-claim, and Content Scout milestones in the
  [product roadmap](../../docs/roadmap.md).

## Completed

- [260720-milestone-1-evidence-facts.md](260720-milestone-1-evidence-facts.md) —
  process one explicit Sessions snapshot into deterministic, inspectable facts
  without a model call.
