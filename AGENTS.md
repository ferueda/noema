# AGENTS.md

Use clear, simple language. Avoid jargon and unnecessary complexity.

Before planning or changing Noema, read:

- `docs/project-intent.md`
- `docs/architecture.md`
- `docs/roadmap.md`

The project intent owns product direction and hard boundaries. The architecture
owns system boundaries and accepted component responsibilities. The roadmap
owns milestone sequence and decision gates. Do not weaken these sources or
implement a superseded plan implicitly in an implementation plan or code
change.

Contributor guidance:

- Development and commands: `docs/contributing/development.md`
- Testing and proof layers: `docs/contributing/testing.md`
- Contributor index: `docs/contributing/index.md`

## Core rules

- Keep Noema standalone. Harness, Factory, Inngest, Cloudflare, Sessions, model
  providers, and workflow engines are integrations, not owners of the domain.
- Treat source data as read-only evidence.
- Consume Sessions through its versioned CLI JSON or JSONL contract. Do not
  import its internals or read its SQLite database directly.
- Keep canonical source evidence separate from Noema's rebuildable
  interpretations.
- Never resolve stored evidence coordinates against a Sessions document whose
  digest differs from the recorded digest. Fail closed when that revision is no
  longer available.
- Use separate domain types and validation paths for deterministic facts and
  semantic claims. They may share a persistence projection while V0 is small.
- Keep deterministic facts literal and uncertainty-aware. Repeatable parsing
  does not turn narrative assertions into observed outcomes.
- Treat summaries as rebuildable views over admitted facts and claims. Never
  make a summary the only retained representation or use it as source evidence.
- Keep evidence admission, fact extraction, and semantic claims independent of
  downstream uses. Content fields, coding assessments, and workflow proposals
  belong to external consumers, not the knowledge pipeline.
- Preserve evidence references for derived observations and events.
- Keep raw private evidence local by default. Do not send it to a remote model
  or service without an explicit product decision and user control.
- End Noema core at the durable domain-event and transactional-outbox boundary.
  A generic publisher adapter may deliver outbox records, but it must not know
  which consumers exist or whether they ran successfully.
- Keep consumer names, subscriptions, instructions, models, tools, prompts,
  output schemas, jobs, leases, retries, runs, receipts, and consumer-produced
  artifacts outside Noema's domain, application, and storage layers.
- Do not build a queue, worker, dispatcher, scheduler, or agent runtime inside
  Noema. Use an external event transport or workflow system when delivery,
  fan-out, retries, scheduling, or execution durability is required.
- Noema events are consumer-neutral. They carry bounded metadata and stable
  references to Noema-owned records, never a consumer-specific payload.
- If a downstream result should become Noema knowledge, ingest it through a
  normal public evidence boundary. Do not add a privileged consumer callback
  or direct write path.
- If a proposed change names Content Scout, Coding Evaluation, Eve, or another
  consumer below the publisher adapter or presentation layer, stop and move
  that work outside Noema.
- Do not make Factory work items, Inngest runs, issues, pull requests, or any
  other external artifact required for work to be understood.
- Prefer one thin end-to-end implementation over unused abstraction.
- Do not add a generic plugin system, graph database, cloud deployment,
  scheduler, or vector database before a proven use case requires it.
- Keep fixtures, examples, and documentation free of private transcript
  content, repository names, local paths, and personal identifiers.
- Keep routine tests offline and isolated from user Sessions data, Noema data,
  credentials, and external services.
- Add regression coverage for bugs when it protects repeatable behavior.
- Before handoff, pull-request publication, or declaring work complete, run
  `make check`.
- For formatting failures, run `make fix`, inspect the diff, and rerun the
  relevant test followed by `make check`.

When a proposed change conflicts with the product intent or architecture, stop
and make the product decision explicit before implementing it.
