# Implementation plans

Plans in this directory describe accepted work that is ready for review or
implementation. They are temporary execution artifacts, not a second roadmap.

After a plan is completed or superseded, first move any lasting product
decision, architecture boundary, operating instruction, or result into its
authoritative document. Then remove the plan from this directory. Git history
retains the implementation record.

## Active

- [260728-event-boundary.md](260728-event-boundary.md) — remove the pre-V1
  agent scaffold and finish Noema at an atomic domain-event and transactional
  outbox boundary with one generic manual publisher.

## Queued

- [260722-incremental-evidence-windows.md](260722-incremental-evidence-windows.md) —
  after Noema V0, external Content Scout feedback, and the knowledge-unit
  checkpoint, plan one growing session into deterministic windows and avoid
  repeated semantic calls for unchanged work.
