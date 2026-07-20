# Project intent

- Status: accepted product baseline
- Date: 2026-07-19

## Purpose

Noema is a local-first knowledge, event, and agent runtime for personal work.
It observes evidence from the tools where work happens, distills that evidence
into a consistent model, publishes meaningful changes as durable events, and
lets small, focused agents turn those events into useful, reviewable outputs.

The long-term product combines two ideas:

1. A knowledge pipeline that ingests varied sources, extracts structured
   meaning and metadata, and supports strong evidence retrieval.
2. An event pipeline that publishes normalized changes to a queue where
   focused agents can react independently.

Noema is the infrastructure for those pipelines. Content Scout is its first
agent and first proof that the infrastructure is useful.

## Audience

The initial user is an individual software developer who does substantial work
with AI coding tools and wants that work to produce reusable knowledge,
content, and workflow improvements.

The first content audience is developers actively using AI tools. Topics
include everyday AI-assisted coding, Codex usage, software development with AI,
practical techniques, mistakes, experiments, and lessons from real work.

Noema may later serve other kinds of individual knowledge work. Multi-user and
company-wide use are not initial requirements.

## Product outcome

Noema should make this loop possible:

```text
Evidence from work
        ↓
Normalized knowledge
        ↓
Durable domain events
        ↓
Focused agent subscriptions
        ↓
Reviewable ideas, proposals, and drafts
        ↓
Explicit human decisions
```

Adding a new focused agent should normally require its event subscriptions,
evidence queries, instructions, and output schema. It should not require
rewriting ingestion or storage.

Adding a new source should normally require a source adapter and source-specific
distillation. It should not require changing agents to understand that
provider's native format.

## What we optimize for

1. **Evidence before inference.** Derived knowledge and agent outputs remain
   traceable to bounded source evidence.
2. **Local privacy and control.** Private work remains local by default. The
   user chooses when analysis runs and what may leave the machine.
3. **Useful outputs.** The platform is judged by the quality of what its agents
   produce, not by the number of abstractions or integrations it contains.
4. **Small, focused agents.** Each agent reacts to a narrow set of events,
   retrieves only the evidence it needs, and writes a typed result.
5. **Workflow independence.** Noema can understand work whether it happened in
   a plain agent session, an Inngest workflow, a pull request, or another tool.
6. **Incremental and replayable processing.** Unchanged evidence is not
   repeatedly analyzed. Changed rules or agents can be evaluated against
   retained derived events.
7. **Honest uncertainty.** Inferred episodes, classifications, relationships,
   and recommendations carry confidence and can be corrected.
8. **Simple beginnings.** The first implementation uses the fewest components
   needed to exercise the full path from evidence to agent output.

## What we do not optimize for

- Ingesting every possible source in the first release.
- Company-wide search, team collaboration, accounts, permissions, or billing.
- Cloud deployment, sync, or remote storage before the local product is useful.
- Maximum agent autonomy.
- Automatic content publishing or automatic workflow changes.
- A formal semantic ontology, graph database, or third-party plugin platform.
- Vector search as the default answer to every retrieval problem.
- Compatibility with Factory work items or any other workflow-specific record.
- Replacing the source systems where work already happens.
- Replacing Sessions as the canonical library for coding-agent history.
- Production-scale throughput before personal-scale behavior is proven.

## Hard boundaries

### Source ownership

- Source systems remain the owners of their canonical evidence.
- Source histories are read-only inputs.
- Noema does not parse Codex or Cursor histories directly while Sessions owns
  that responsibility.
- Noema consumes Sessions through its versioned CLI JSON or JSONL output. It
  does not import Sessions internals or open the Sessions SQLite database.
- Noema does not invoke `sessions index` implicitly. Indexing remains an
  explicit Sessions operation.

### Derived state

- Noema owns its normalized observations, episodes, events, agent runs, and
  agent outputs.
- Noema's interpretations are rebuildable from canonical evidence.
- An episode is a Noema hypothesis, not a source session, Factory work item,
  Inngest run, issue, or pull request.
- External records can support or relate to an episode but cannot be required
  for one to exist.

### Privacy and trust

- Transcript text and other imported evidence are untrusted data, not
  instructions for Noema or its agents.
- Events carry evidence references and bounded metadata, not raw transcript
  bodies.
- Raw private evidence stays local by default.
- Public work may be described specifically. Private work is generalized
  unless the user explicitly approves the details.
- Secrets, tokens, private URLs, local paths, personal data, security details,
  private repository names, client information, internal issue identifiers,
  and unpublished plans are excluded from content outputs.
- Other people's words and raw agent transcript text are not quoted without
  explicit review.

### Agent authority

- Agent outputs are proposals or local artifacts.
- No agent publishes content, modifies a source system, changes a workflow, or
  edits its own instructions without explicit approval.
- A model response is never treated as evidence merely because a model produced
  it.
- Agent failures, retries, versions, inputs, evidence references, and outputs
  remain inspectable.

### Technology independence

- Go and SQLite are the first implementation choices, not domain concepts.
- Inngest may supply workflow evidence or execute agents later. It is not a
  required source or domain dependency.
- Cloudflare services may later host selected derived data or execution. They
  are not the initial source of truth.
- Model providers, embedding providers, queues, and workflow engines sit behind
  explicit boundaries.

## First useful outcome

The first vertical slice uses:

- Sessions as the only source.
- An explicit, manually supplied time range.
- A local SQLite database for derived state, events, jobs, and outputs.
- Content Scout as the only subscriber.
- A command-line interface as the initial presentation.

One manual scan should:

1. Read already-indexed Sessions evidence for the requested range.
2. Detect new or changed documents through stable source identity and digests.
3. Distill relevant evidence into normalized observations, including useful
   insights, decisions, problems, experiments, and lessons.
4. Store those observations with metadata and evidence references.
5. Publish durable domain events and enqueue matching Content Scout jobs.
6. Run Content Scout through a provider-neutral model boundary.
7. Return at most five strong, ranked content ideas. An empty result is valid.

Each content idea includes:

- A working concept.
- The core lesson.
- The intended developer audience and benefit.
- A possible hook.
- Why the idea may resonate.
- Supporting evidence references.
- Confidence and ranking information.
- An angle for each suitable format: short X post, longer X thread, and
  article.

V0 identifies ideas. It does not write complete drafts.

Ideas persist locally. An exact unchanged rerun does not create them again.
Cross-scan semantic deduplication, strengthening, and resurfacing are later
hypotheses; V0 may show similar ideas from different changed or overlapping
evidence.

## Non-goals for the first vertical slice

- Automatic or scheduled scans.
- A background daemon.
- More than one source adapter.
- More than one agent.
- A web interface.
- Full draft generation.
- Posting to X or publishing an article.
- Workflow or skill modification.
- Embeddings, vector search, retrieval fusion, or reranking.
- Inngest or Cloudflare deployment.
- A public extension API.

## Unsafe assumptions

- One session represents one work episode.
- A session lineage root always represents one goal.
- Work has a structured work item.
- Work has an explicit completion event.
- An Inngest run, issue, pull request, or artifact is always present.
- Source timestamps, titles, authorship, lineage, or workspace metadata are
  complete and correct.
- A user-looking message was authored directly by the user.
- A model summary is faithful or safe to publish.
- Repeated text represents independent evidence.
- Similarity represents truth, usefulness, or novelty.
- A popular-looking idea is aligned with the user's voice or goals.
- Source text is secret-free because it was returned as structured output.
- An event is safe to replay against an agent with external side effects.
- A remote service has the same privacy boundary as local processing.
- OpenAI-compatible APIs behave identically across models and providers.
- A model gateway always routes the same model to the same inference provider.
- Gateway defaults provide sufficient retention, training, or provider
  restrictions for private evidence.
- A privacy control is active merely because the gateway offers it.
- Automatic provider or model fallback is harmless for evaluation or private
  evidence.
- A model, queue, workflow engine, or storage provider will remain permanent.

## Revisit triggers

Consider additional components only after evidence shows a need:

- Add embeddings when metadata and full-text retrieval repeatedly miss
  conceptually related evidence.
- Add a second source when Content Scout quality is limited by missing context,
  not by weak extraction.
- Add Workflow Scout when the source-to-event-to-agent path works without
  Content Scout-specific assumptions.
- Add scheduled execution when manual scans are useful but are being missed.
- Add Inngest when durable multi-step execution, waiting, or retries exceed the
  value of the local queue.
- Add Cloudflare when remote access or selected sync provides clear value and
  has an explicit privacy design.
- Add draft generation after idea selection produces a useful acceptance and
  rejection history.

## Decision authority

This document owns Noema's product direction, priorities, non-goals, hard
boundaries, and unsafe assumptions. `docs/architecture.md` owns the accepted
system structure that serves this intent.
