# AGENTS.md

Use clear, simple language. Avoid jargon and unnecessary complexity.

Before planning or changing Noema, read:

- `docs/project-intent.md`
- `docs/architecture.md`

The project intent owns product direction and hard boundaries. The architecture
owns system boundaries and accepted component responsibilities. Do not weaken
either implicitly in an implementation plan or code change.

## Core rules

- Keep Noema standalone. Harness, Factory, Inngest, Cloudflare, Sessions, model
  providers, and workflow engines are integrations, not owners of the domain.
- Treat source data as read-only evidence.
- Consume Sessions through its versioned CLI JSON or JSONL contract. Do not
  import its internals or read its SQLite database directly.
- Keep canonical source evidence separate from Noema's rebuildable
  interpretations.
- Preserve evidence references for derived observations, events, and agent
  outputs.
- Keep raw private evidence local by default. Do not send it to a remote model
  or service without an explicit product decision and user control.
- Agents create reviewable artifacts. They do not publish content, edit source
  systems, or apply workflow changes without explicit approval.
- Do not make Factory work items, Inngest runs, issues, pull requests, or any
  other external artifact required for work to be understood.
- Prefer one thin end-to-end implementation over unused abstraction.
- Do not add a generic plugin system, graph database, cloud deployment,
  scheduler, or vector database before a proven use case requires it.
- Keep fixtures, examples, and documentation free of private transcript
  content, repository names, local paths, and personal identifiers.

When a proposed change conflicts with the product intent or architecture, stop
and make the product decision explicit before implementing it.
