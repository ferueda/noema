# Project intent

- Status: accepted product baseline
- Date: 2026-07-22
- Updated: 2026-07-28

## Purpose

Noema is a local-first derived-knowledge and event system for personal work.
Sessions is its initial canonical evidence plane for coding-agent history.
Noema begins where source capture and normalization end: it extracts
deterministic facts, admits evidence-backed semantic claims, builds optional
summaries, records meaningful changes as durable domain events, and makes those
events available to independent consumers.

The long-term product combines two ideas:

1. A derived-knowledge pipeline that consumes canonical evidence, extracts
   deterministic facts and structured semantic meaning, and supports
   evidence-backed retrieval over Noema-owned records.
2. A consumer-neutral event boundary that records meaningful changes and can
   publish them to an external transport.

Noema owns these pipelines. It does not own the agents or services that react
to their output.

Content Scout is the first planned external consumer. It can use Noema events
and public queries to propose evidence-backed content ideas. A later coding
coach can use the same boundary to identify development patterns and suggest
learning goals. A workflow-improvement consumer can propose changes to skills
or tools. None of those use cases may add its vocabulary, execution model, or
state to Noema's core.

The evidence and knowledge pipeline stays use-case-neutral. It records what
happened and what can be supported about it; it does not extract hooks,
audiences, content formats, personal weaknesses, learning recommendations, or
workflow fixes.

Noema is also a learning project. It should make evidence admission,
extraction, lineage, event creation, publication, and replay visible enough to
study and change. Experiments with Go, model providers, retrieval, Inngest,
Cloudflare, Eve, or another agent framework are welcome on the correct side of
the boundary. An experiment enters Noema only when it solves a Noema-owned
problem.

## Audience

The initial user is an individual software developer who does substantial work
with AI coding tools and wants that work to produce reusable knowledge and
useful signals for independent tools.

The first downstream content audience is developers actively using AI tools.
Topics include everyday AI-assisted coding, Codex usage, software development
with AI, practical techniques, mistakes, experiments, and lessons from real
work.

Noema may later serve other kinds of individual knowledge work. Multi-user and
company-wide use are not initial requirements.

## Product outcome

Noema should make this boundary possible:

```text
Provider histories
        ↓
Canonical source evidence
        ↓
Deterministic facts
        ↓
Validated semantic claims
        ↓
Optional summary projections
        ↓
Durable domain event + outbox record
        ║
        ║ Noema core ends here
        ║
        ↓
Generic publisher adapter
        ↓
External transport or workflow system
        ↓
Independent consumers and human review
```

Canonical evidence records what a source system observed. Deterministic facts
and semantic claims record what Noema derived. They remain separate authority
classes and carry source coordinates back to the evidence.

An `AnalysisRun` records the exact evidence revision and selection, processing
versions, coverage, and admitted fact and claim identities for one attempt.
Summaries are optional, rebuildable views over those records. They help people
and consumers understand a scope, but they never replace the facts, claims, or
evidence that support them.

A `DomainEvent` reports that Noema-owned state changed. The event and its
outbox record commit in the same local transaction as that change. Publication
may happen later through a generic adapter. Noema considers publication
successful when the configured transport accepts the event; it does not know
which consumers subscribe, whether they ran, what model they used, or what
they produced.

Adding a new downstream consumer should require no Noema code change. The
consumer owns its triggers, execution runtime, model and tools, retries,
scheduling, output schema, persistence, and review workflow. It may be written
in any language or run on any platform.

Adding a new source should normally require a source adapter that provides an
equivalent stable, canonical evidence contract. It should not require changing
downstream consumers to understand that source's native format. A provider
already normalized by Sessions is not a separate Noema source integration.

## What we optimize for

1. **Evidence before inference.** Deterministic facts are extracted before
   semantic interpretation. Every admitted claim remains traceable to bounded
   canonical evidence.
2. **Local privacy and control.** Private work remains local by default. The
   user chooses when analysis runs and what may leave the machine.
3. **Reusable knowledge.** Facts, claims, summaries, and events describe the
   work rather than one downstream use.
4. **A hard consumer boundary.** Noema publishes what changed without owning
   subscriptions or execution.
5. **Workflow independence.** Noema can understand work whether it happened in
   a plain agent session, an Inngest workflow, a pull request, or another tool.
6. **Incremental and replayable processing.** Unchanged evidence is not
   repeatedly analyzed. Extractors can be rerun from retained canonical
   evidence, and events can be republished from the local outbox.
7. **Honest uncertainty.** Semantic claims distinguish observed, inferred, and
   uncertain conclusions; preserve confidence and contradictions; and can be
   corrected.
8. **Simple beginnings.** Prove one uncertain boundary at a time, starting with
   one explicitly selected session and one manual publisher.
9. **Inspectable mechanics.** Prefer explicit stored stages, versioned inputs,
   and understandable process boundaries.

## What we do not optimize for

- Ingesting every possible source in the first release.
- Company-wide search, team collaboration, accounts, permissions, or billing.
- Cloud deployment, sync, or remote storage before the local product is useful.
- Owning agent or consumer execution.
- Automatic content publishing or automatic workflow changes.
- A formal semantic ontology, graph database, or third-party plugin platform.
- Vector search as the default answer to every retrieval problem.
- Compatibility with Factory work items or any other workflow-specific record.
- Replacing the source systems where work already happens.
- Replacing Sessions as the canonical library for coding-agent history.
- Production-scale throughput before personal-scale behavior is proven.

## Hard boundaries

### Source ownership

- Provider systems own their raw histories. Source histories are read-only
  inputs.
- Sessions is the canonical evidence plane for coding-agent history. It owns
  provider discovery, parsing, structural extraction, normalization,
  validation, retention, revision inventory, and transcript retrieval.
- Noema consumes Sessions only through its versioned CLI JSON or JSONL
  contract. It does not import Sessions internals, open its SQLite database, or
  parse Codex or Cursor histories directly.
- Noema does not invoke `sessions index` implicitly. Indexing remains an
  explicit Sessions operation.
- Noema does not duplicate complete transcripts or create a second raw-history
  or transcript-search archive. It stores source coordinates, digests, content
  hashes, processing metadata, its own derived records, and only the minimum
  bounded admitted excerpt required for review.
- Noema never resolves stored coordinates against a different Sessions document
  digest. If the referenced revision is no longer retained, resolution fails
  closed rather than substituting evidence from the latest snapshot.
- A future explicit multi-session operation may select one transcript-free,
  ordered Sessions manifest cohort before hydrating individual documents.
  Noema owns that operation and its processing outcomes; it does not treat the
  manifest as a transcript archive, historical pin, or proof that provider
  capture was complete.

### Derived state

- Noema owns evidence selections, analysis runs, deterministic facts, semantic
  claims, optional summaries, revisable episodes, domain events, and event
  publication state.
- A deterministic fact is still a derived record. Canonical source evidence
  remains the authority for what was captured.
- Noema's interpretations are rebuildable while the referenced canonical
  evidence version remains available.
- Every `AnalysisRun` records its evidence revision, selection, coverage,
  processing configuration, admitted outputs, and completion or failure state.
- A summary is a versioned projection over admitted facts and claims, not a new
  authority class or the only stored representation of an analysis.
- Model output is untrusted until its schema, evidence references, confidence,
  contradictions, and deterministic consistency are validated.
- Deterministic and semantic extraction remain independent of downstream
  output schemas.
- An episode is a Noema hypothesis, not a source session, Factory work item,
  Inngest run, issue, pull request, or consumer execution.
- External records can support or relate to an episode but cannot be required
  for one to exist.

### Event boundary

- Noema core ends after atomically committing the changed derived records, a
  consumer-neutral domain event, and its transactional outbox record.
- A publisher adapter may read pending outbox records and hand them to an
  external transport. It knows event schemas and transport acknowledgements,
  not consumer identities or behavior.
- Noema does not own subscriptions, agent definitions, prompts, model routes,
  tools, consumer output schemas, jobs, leases, retries, schedules, execution
  runs, receipts, or consumer-produced artifacts.
- Noema does not implement an agent dispatcher, worker engine, or general
  workflow queue. Inngest, Cloudflare, Eve channels, or another external
  system may own delivery, fan-out, retry, scheduling, and execution.
- Domain events contain bounded metadata and stable references to Noema-owned
  records. They do not contain raw transcripts or a payload prepared for one
  consumer.
- Adding, removing, or upgrading a consumer does not change event identity,
  rerun evidence processing, or require a Noema deployment.
- Consumer failure does not change the truth of the Noema event that woke it.
- A consumer may query Noema through a future bounded public read interface or
  receive a consumer-owned projection prepared outside Noema core.
- If a consumer result should become Noema knowledge, it returns through a
  normal public source/evidence ingestion boundary. It never writes Noema
  domain tables directly and does not use a privileged result callback.

This section is an architectural gate. A proposal that adds a consumer name,
subscription rule, execution configuration, or output type to Noema's domain,
application, or persistence layers conflicts with the product intent.

### Privacy and trust

- Transcript text and other imported evidence are untrusted data, not
  instructions for Noema or downstream consumers.
- Events carry evidence references and bounded metadata, not raw transcript
  bodies.
- Raw private evidence stays local by default.
- Every remote semantic run requires explicit user control and records whether
  upstream zero-retention and no-training controls were requested. Those
  controls may be disabled for early experiments; disabling them never makes
  remote processing implicit or equivalent to local processing.
- Public work may be described specifically. Private work is generalized
  unless the user explicitly approves the details.
- Secrets, tokens, private URLs, local paths, personal data, security details,
  private repository names, client information, internal issue identifiers,
  and unpublished plans are excluded from public outputs.
- Other people's words and raw transcript text are not quoted without explicit
  review.
- Personal coding assessments remain private by default and require explicit
  human review before any external action.

### Consumer authority

- Downstream outputs are proposals, not Noema facts.
- Consumers receive no implicit authority to publish content, modify a source
  system, change a workflow, edit instructions, or write Noema state.
- A model response is never treated as source evidence merely because a model
  produced it.
- Assessments about the user must be bounded to observed coding behavior,
  preserve counterevidence, and distinguish user, agent, environment, mixed,
  and unknown attribution.
- Consumer execution and publication policy belong to the consumer project or
  its workflow platform, not Noema.

### Technology independence

- Go and SQLite are the first implementation choices, not domain concepts.
- Inngest may deliver Noema events and run consumers. It is an external
  transport and workflow engine, not a Noema domain dependency.
- Eve may implement one consumer's model loop, tools, or channels. Noema never
  calls Eve and stores no Eve execution state.
- Cloudflare services may later transport events or host selected derived data
  and consumers. They are not the initial source of truth.
- Model providers and gateways sit behind explicit boundaries for Noema-owned
  semantic extraction.

## First useful outcome

V0 reaches the first useful Noema outcome through three small, independently
inspectable milestones. It starts with one explicitly selected Sessions
session before broad time-range or corpus scans.

### Milestone 1: canonical evidence and deterministic facts

1. Export the complete export-eligible content of one already-indexed retained
   session snapshot locally through the Sessions CLI.
2. Validate its schema, trust disposition, identity, digest, coordinates,
   bounds, omissions, and available coverage.
3. Mechanically extract supported facts such as tool calls and results,
   commands, errors, test invocations and directly supported outcomes, files,
   and repository metadata.
4. Store those facts with exact evidence references and make them inspectable
   locally.

This milestone makes no model call. Code extracts only literal observations
that the canonical structure and supported parsers can establish. Ambiguous
outcomes remain unknown, and source capture omissions remain visible.

### Milestone 2: semantic claims

1. Give bounded canonical evidence and deterministic facts to a
   provider-neutral structured-generation boundary.
2. Extract a small initial claim vocabulary: problem, symptom, hypothesis,
   failed attempt, root cause, decision, solution, verification, and lesson.
3. Reject claims with invalid evidence references or failures in schema,
   privacy, contradiction, and deterministic-consistency checks.
4. Preserve whether each claim is observed, inferred, or uncertain, together
   with confidence and contradictory evidence.
5. Store admitted claims and consumer-neutral domain events atomically.

The semantic evaluator remains a development tool, not a product stage. The
reviewed V2 corpus showed no unsupported admitted claim, while conservative
misses remain for root cause, decisions, lessons, and one implemented change.
Do not weaken deterministic admission merely to improve recall.

### Milestone 3: durable event publication boundary

1. Add an append-only transactional outbox beside the existing domain events.
2. Commit each event and outbox record atomically with the Noema state change
   it describes.
3. Add local inspection and a manual, generic publication command.
4. Mark an outbox record delivered only after the configured transport
   acknowledges the event.
5. Make exact reruns idempotent, never republish a committed delivery, and use
   stable event IDs so external systems can deduplicate a delivery repeated
   after an uncertain failure.
6. Prove the boundary with a fake publisher and public synthetic events before
   choosing a hosted transport.

This milestone produces no agent job, run, or artifact. Content Scout is a
separate downstream experiment after the boundary works.

## Non-goals for V0

- Broad time-range, corpus-wide, or automatic scans before the explicit-session
  path is useful.
- A Noema-owned raw transcript archive or duplicate Sessions search library.
- Provider-specific Codex or Cursor parsing.
- A separate knowledge-unit layer before real claims show that one is needed.
- Automatic or scheduled scans.
- A background daemon.
- More than one source adapter.
- Any Noema-owned agent, subscription, job, worker, scheduler, or artifact
  runtime.
- A web interface.
- Full content draft generation or publication.
- Workflow or skill modification.
- Embeddings, vector search, retrieval fusion, or reranking.
- A hosted event transport, Inngest deployment, or Cloudflare deployment.
- A public extension API.

## Unsafe assumptions

- One session represents one work episode.
- A session lineage root always represents one goal.
- Work has a structured work item or explicit completion event.
- An Inngest run, issue, pull request, or artifact is always present.
- Source timestamps, titles, authorship, lineage, or workspace metadata are
  complete and correct.
- A user-looking message was authored directly by the user.
- A model summary is faithful or safe to publish.
- A failed attempt, correction, missing test, or repeated command proves a user
  knowledge gap.
- Missing evidence that a skill was used proves the user lacks that skill.
- A structured Sessions field is equivalent to a semantic conclusion.
- An assistant statement that a command or test succeeded is stronger than the
  recorded tool result.
- A claim is supported merely because a model returned a valid evidence
  identifier.
- Sessions retains every raw provider revision or can always reproduce an
  earlier canonical snapshot.
- A Sessions manifest pins the listed document bodies or makes an older body
  addressable by digest.
- An empty retained-library cohort proves that no applicable provider sessions
  were missed.
- Repeated text represents independent evidence.
- Similarity represents truth, usefulness, or novelty.
- Source text is secret-free because it was returned as structured output.
- Publishing an event means a consumer received or completed it.
- Transactional outbox publication provides exactly-once delivery. A crash
  after external acceptance but before the local acknowledgement commit can
  cause a duplicate with the same event ID.
- Replaying an event is safe for a consumer with external side effects.
- A remote service has the same privacy boundary as local processing.
- OpenAI-compatible APIs behave identically across models and providers.
- A model gateway always routes the same model to the same inference provider.
- Gateway defaults provide sufficient retention, training, or provider
  restrictions for private evidence.
- A model, event transport, workflow engine, or storage provider will remain
  permanent.

## Revisit triggers

Consider additional components only after evidence shows a need:

- Expand from one selected session to ranges only after the evidence and claim
  outputs are useful and inspectable.
- Add knowledge units when individual claims prove too granular for retrieval
  or downstream consumers.
- Add a second semantic verification pass when evaluation shows unsupported or
  contradicted claims surviving deterministic validation.
- Revisit canonical revision retention with Sessions only when a concrete
  reproducibility need cannot be met by its retained snapshots.
- Add embeddings when metadata and full-text retrieval repeatedly miss
  conceptually related evidence.
- Add a second source when downstream quality is limited by missing context,
  not weak extraction.
- Choose a hosted event transport only after the local outbox and manual
  publisher prove the event contract.
- Build Content Scout outside Noema after consumer-neutral publication works.
- Build a coding coach after multi-session scopes and attribution are
  trustworthy.
- Add scheduled Noema ingestion when useful manual scans are being missed.
- Add Cloudflare-hosted storage only when remote access or selected sync
  provides clear value and has an explicit privacy design.
- Let each consumer add drafting, ranking, feedback, or autonomous behavior
  only after its own product evidence and authority design justify it.

## Decision authority

This document owns Noema's product direction, priorities, non-goals, hard
boundaries, and unsafe assumptions. `docs/architecture.md` owns the accepted
system structure that serves this intent. If a plan or implementation conflicts
with the event boundary above, this document wins.
