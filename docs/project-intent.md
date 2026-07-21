# Project intent

- Status: accepted product baseline
- Date: 2026-07-20

## Purpose

Noema is a local-first derived-knowledge, event, and agent runtime for personal
work. Sessions is its initial canonical evidence plane for coding-agent
history. Noema begins where source capture and normalization end: it extracts
deterministic facts, admits evidence-backed semantic claims, records meaningful
changes as durable events, and lets small, focused agents turn those events
into useful, reviewable outputs.

The long-term product combines two ideas:

1. A derived-knowledge pipeline that consumes canonical evidence, extracts
   deterministic facts and structured semantic meaning, and supports
   evidence-backed retrieval over Noema-owned records.
2. An event pipeline that publishes normalized changes to a queue where
   focused agents can react independently.

Noema is the infrastructure for those pipelines. Content Scout is its first
agent and first proof that the infrastructure is useful.

The evidence and knowledge pipeline is use-case-neutral. It records what
happened and what can be supported about it; it does not extract hooks,
audiences, content formats, personal weaknesses, learning recommendations, or
workflow fixes. Focused agents derive those application-specific artifacts
from the same admitted facts and claims.

Noema is also a learning project. It should make the mechanics of evidence
admission, extraction, event delivery, agent execution, and replay visible
enough to study and change. Experiments with Go, model providers, retrieval,
Inngest, or Cloudflare are welcome behind the accepted boundaries, but an
experiment enters the product path only when it solves an observed need.

## Audience

The initial user is an individual software developer who does substantial work
with AI coding tools and wants that work to produce reusable knowledge,
content, workflow improvements, and evidence-backed guidance about their own
coding development.

The first content audience is developers actively using AI tools. Topics
include everyday AI-assisted coding, Codex usage, software development with AI,
practical techniques, mistakes, experiments, and lessons from real work.

Noema may later serve other kinds of individual knowledge work. Multi-user and
company-wide use are not initial requirements.

## Product outcome

Noema should make this loop possible:

```text
Provider histories
        ↓
Canonical source evidence
        ↓
Deterministic facts
        ↓
Validated semantic claims
        ↓
Durable domain events
        ↓
Focused agent subscriptions
        ↓
Reviewable typed artifacts
        ↓
Explicit human decisions
```

Canonical evidence records what a source system observed. Deterministic facts
and semantic claims record what Noema derived. They remain separate authority
classes and carry source coordinates back to the evidence.

An analysis records the exact evidence scope, processing versions, coverage,
and admitted fact and claim identities for one run. Summaries are optional,
rebuildable views over those records. They help people and agents understand a
scope, but they never replace the facts, claims, or evidence that support them.

Adding a new focused agent should normally require its event subscriptions,
evidence queries, instructions, and output schema. It should not require
rewriting ingestion or storage.

The generic runtime stores an agent artifact envelope and its lineage. Each
agent owns the typed payload inside that envelope. Content ideas, coding
assessments, and workflow proposals are sibling artifact types rather than core
knowledge concepts.

Adding a new source should normally require a source adapter that provides an
equivalent stable, canonical evidence contract. It should not require changing
agents to understand that source's native format. A provider already normalized
by Sessions is not a separate Noema source integration.

## What we optimize for

1. **Evidence before inference.** Deterministic facts are extracted before
   semantic interpretation. Every admitted claim and agent output remains
   traceable to bounded canonical evidence.
2. **Local privacy and control.** Private work remains local by default. The
   user chooses when analysis runs and what may leave the machine.
3. **Useful outputs.** The platform is judged by the quality of what its agents
   produce, not by the number of abstractions or integrations it contains.
4. **Small, focused agents.** Each agent reacts to a narrow set of events,
   retrieves only the evidence it needs, and writes a typed result.
5. **Workflow independence.** Noema can understand work whether it happened in
   a plain agent session, an Inngest workflow, a pull request, or another tool.
6. **Incremental and replayable processing.** Unchanged evidence is not
   repeatedly analyzed. Extractors can be rerun from retained canonical
   evidence, and changed agents can be evaluated against retained derived
   records.
7. **Honest uncertainty.** Semantic claims distinguish observed, inferred, and
   uncertain conclusions; preserve confidence and contradictions; and can be
   corrected.
8. **Simple beginnings.** Prove one uncertain boundary at a time, starting with
   one explicitly selected session while preserving the intended end-to-end
   architecture.
9. **Inspectable mechanics.** Prefer explicit stored stages, versioned inputs,
   and understandable process boundaries so the system teaches us how the
   architecture behaves.

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

- Provider systems own their raw histories. Source histories are read-only
  inputs.
- Sessions is the canonical evidence plane for coding-agent history. It owns
  provider discovery, parsing, structural extraction, normalization,
  validation, retention, and transcript retrieval.
- Noema consumes Sessions only through its versioned CLI JSON or JSONL
  contract. It does not import Sessions internals, open its SQLite database, or
  parse Codex or Cursor histories directly.
- Noema does not invoke `sessions index` implicitly. Indexing remains an
  explicit Sessions operation.
- Noema does not duplicate complete transcripts or create a second raw-history
  or transcript-search archive. It stores source coordinates, digests, content
  hashes, processing metadata, its own derived records, and only the minimum
  bounded admitted excerpt required for review.

### Derived state

- Noema owns deterministic facts and semantic claims, initially stored as typed
  observations, plus analyses, episodes, events, jobs, agent runs, typed agent
  artifacts, and explicit human decisions.
- A deterministic fact is still a derived record. Canonical source evidence
  remains the authority for what was captured.
- Noema's interpretations are rebuildable while the referenced canonical
  evidence version remains available.
- Every analysis records its selected evidence identities, scope, coverage,
  processing configuration, admitted outputs, and completion or failure state.
- A summary is a versioned projection over admitted facts and claims, not a new
  authority class or the only stored representation of an analysis.
- Model output is untrusted until its schema, evidence references, confidence,
  and contradictions are validated.
- Deterministic and semantic extraction remain independent of agent output
  schemas. A new artifact type cannot redefine facts or previously admitted
  claims implicitly.
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
- Personal coding assessments remain private local artifacts by default. They
  follow the same explicit remote-processing controls as other private derived
  work and are never published automatically.

### Agent authority

- Agent outputs are proposals or local artifacts.
- Agent execution is stateless across runs. Continuity lives in admitted
  evidence, derived records, events, jobs, run history, and artifacts rather
  than model memory or one long conversation.
- No agent publishes content, modifies a source system, changes a workflow, or
  edits its own instructions without explicit approval.
- A model response is never treated as evidence merely because a model produced
  it.
- Assessments about the user are bounded to observed coding behavior. They must
  preserve counterevidence and distinguish user, agent, environment, mixed, and
  unknown attribution rather than presenting every failure as a personal
  weakness.
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

V0 reaches the first useful outcome through three small, independently
inspectable milestones. It starts with one explicitly selected Sessions
session before broad time-range or corpus scans.

### Milestone 1: canonical evidence and deterministic facts

1. Export one already-indexed session through the Sessions CLI.
2. Validate its schema, trust disposition, identity, digest, coordinates,
   bounds, omissions, and available coverage.
3. Mechanically extract supported facts such as tool calls and results,
   commands, errors, tests and outcomes, files, and repository metadata.
4. Store those facts with exact evidence references and make them inspectable
   locally.

This milestone makes no model call. Code extracts only what the canonical
structure can establish.

### Milestone 2: semantic claims

1. Give bounded canonical evidence and deterministic facts to a
   provider-neutral structured-generation boundary.
2. Extract a small initial claim vocabulary: problem, symptom, hypothesis,
   failed attempt, root cause, decision, solution, verification, and lesson.
3. Reject claims with invalid or unsupported evidence references.
4. Preserve whether each claim is observed, inferred, or uncertain, together
   with confidence and contradictory evidence.
5. Store admitted claims and their durable events.

A second model verification pass is not required initially. Add it only if
evaluation shows that schema, evidence, and deterministic validation are
insufficient.

### Milestone 3: Content Scout

1. Enqueue Content Scout from admitted knowledge events.
2. Run the worker separately through the provider-neutral model boundary.
3. Store and review at most five ranked, evidence-backed content ideas. An
   empty result is valid.
4. Show how each suitable idea could become a short X post, a longer X thread,
   or an article.

Each stage stores its own versioned derived records so later stages can rerun
without returning to raw provider histories, while the referenced canonical
Sessions evidence remains available.

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

## Non-goals for V0

- Broad time-range, corpus-wide, or automatic scans before the explicit-session
  path is useful.
- A Noema-owned raw transcript archive or duplicate Sessions search library.
- Provider-specific Codex or Cursor parsing.
- A separate knowledge-unit layer before real claims show that one is needed.
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

- Expand from one selected session to ranges only after the evidence and claim
  outputs are useful and inspectable.
- Add knowledge units when individual claims prove too granular for retrieval
  or Content Scout.
- Add a second semantic verification pass when evaluation shows unsupported or
  contradicted claims surviving deterministic validation.
- Revisit canonical revision retention with Sessions only when a concrete
  reproducibility need cannot be met by its retained snapshots.
- Add embeddings when metadata and full-text retrieval repeatedly miss
  conceptually related evidence.
- Add a second source when Content Scout quality is limited by missing context,
  not by weak extraction.
- Add Workflow Scout when the source-to-event-to-agent path works without
  Content Scout-specific assumptions.
- Add Coding Evaluation after multi-session scopes and attribution are
  trustworthy. It produces reviewable growth areas and learning recommendations
  from supporting and contradicting evidence; it does not score identity,
  personality, or general ability.
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
