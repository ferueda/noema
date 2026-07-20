# Architecture

- Status: accepted design baseline
- Date: 2026-07-19
- Scope: local-first foundation and first vertical slice

## Executive summary

Noema combines a knowledge pipeline with an event-driven agent runtime.

The knowledge pipeline ingests evidence from the tools where work happens,
distills it into a provider-neutral model, stores metadata and provenance, and
offers narrow retrieval operations.

The event pipeline records meaningful changes, enqueues subscribed agents, and
stores their typed outputs. Events trigger agents; they do not carry all of the
evidence an agent may need. Agents use evidence references to retrieve bounded
context from the knowledge layer.

The first implementation is a standalone Go application with a Noema-owned
SQLite database. Sessions is the first source. Content Scout is the first
agent. A manual scan is the first execution mode.

## Design influences

Noema draws from two public designs:

- Drew Bredvick's event-driven agent pattern: ingest sources, normalize work
  into events, publish to a queue, and let focused agents subscribe.
- Cerebras's knowledge-base design: meet data where it lives, distill
  unstructured sources into a shared shape, retain rich metadata, combine
  retrieval methods, and give agents narrow evidence tools.

References:

- <https://x.com/dbredvick/status/2078086524470464577>
- <https://www.cerebras.ai/blog/how-we-built-our-knowledge-base>

These are influences, not specifications. Noema adapts them for private,
personal-scale work and explicit human control.

## System context

```text
Canonical sources
  Sessions initially
  GitHub / Linear / Inngest / files later
          │
          ▼
┌──────────────────────────────────────────────────────────────┐
│ Noema                                                        │
│                                                              │
│ Source adapters → Ingestion → Distillation                   │
│                                  │                           │
│                                  ▼                           │
│                    Normalized knowledge store                │
│                    metadata + evidence refs + search         │
│                                  │                           │
│                         durable domain events                │
│                                  │                           │
│                                  ▼                           │
│                         Queue / subscriptions                │
│                                  │                           │
│                                  ▼                           │
│                         Focused agent runtime                │
│                                  │                           │
│                                  ▼                           │
│                    Ideas, proposals, and drafts              │
└──────────────────────────────────────────────────────────────┘
          │
          ▼
Human review and explicit decisions
```

Noema does not own canonical provider histories. It owns the interpretations,
events, agent execution records, and artifacts it derives from them.

## Core flow

### 1. Observe

A source adapter lists evidence within an explicit scope and returns stable
source identity, content digests, metadata, and bounded source documents.

For Sessions, Noema invokes supported structured CLI commands. Noema does not
open provider files, invoke provider adapters, or read the Sessions database.

### 2. Distill

Distillers transform source documents into provider-neutral observations.
Examples include:

- A goal was stated or changed.
- A decision was made.
- A useful explanation emerged.
- A problem was encountered or solved.
- An experiment produced a result.
- A manual step was repeated.
- An artifact was produced.

Distillation preserves uncertainty. It does not claim that every observation
is true merely because it appeared in a transcript.

### 3. Relate

The episode builder groups observations that appear to belong to the same
continuing effort. It can use session lineage, goal continuity, repository,
workspace, time proximity, artifact references, and workflow run identifiers.

An episode is a revisable hypothesis. Grouping may be unknown, and later user
actions may merge or split episodes.

### 4. Record events

Changes to normalized knowledge produce domain events such as:

```text
observation.created
episode.created
episode.updated
insight.observed
decision.recorded
failure.observed
experiment.completed
artifact.produced
scan.completed
```

Knowledge changes and their events commit in one SQLite transaction. This
prevents an updated projection from existing without the event that describes
the change.

### 5. Enqueue subscriptions

Each focused agent declares the event types it handles. New events create
durable jobs for matching subscriptions.

The local queue provides at-least-once execution. Consumers must be
idempotent. A stable key based on the event, agent, and agent version prevents
duplicate outputs from retries.

### 6. Retrieve evidence

An agent receives an event envelope and narrow retrieval tools. It follows
evidence references to request only the context it needs.

Initial retrieval uses:

- Exact identity and relationship lookups.
- Metadata filters.
- Time and project scope.

Noema does not duplicate Sessions's raw transcript search. Its search index
covers the higher-level observations and artifacts it owns.

Full-text search over normalized Noema knowledge is the next retrieval strategy.
Embeddings and fusion come later behind the same boundary.

### 7. Produce a typed artifact

An agent returns a validated, typed artifact such as a content idea, workflow
proposal, or draft. Noema stores the artifact, its evidence references, agent
and model versions, input event, and run outcome.

Creating an artifact may publish another internal event. External side effects
require a separate explicit approval path.

## Component ownership

| Component | Owns | Does not own |
| --- | --- | --- |
| Source adapter | Source discovery and supported reads | Domain policy, agent behavior, storage |
| Ingestion service | Scan scope, change detection, checkpoints | Source parsing beyond the adapter |
| Distiller | Evidence-to-observation extraction | Canonical evidence or publication decisions |
| Episode builder | Revisable grouping and confidence | Workflow-specific identity |
| Knowledge store | Derived records, metadata, relationships, evidence refs | Canonical source content |
| Retrieval service | Bounded evidence queries and result contracts | Agent orchestration |
| Event store | Durable domain events and replay order | Full evidence bodies |
| Queue | Subscription jobs, leases, attempts, retry state | Agent reasoning |
| Agent runtime | Model invocation, tool access, validation, run records | Domain storage details or external authority |
| Presentation | Commands and review views | Ingestion, extraction, or model policy |
| Composition root | Concrete adapters and provider wiring | Domain behavior |

Dependencies point inward toward domain and application contracts. Only the
composition root knows concrete storage, source, queue, and model
implementations.

## Core concepts

The first implementation needs a small model. Names may change during planning,
but their responsibilities should remain separate.

### Source document

A bounded representation returned by a source adapter:

- Source kind and instance
- Stable native identity
- Document digest
- Capture time and source time when known
- Source metadata
- Bounded content or a supported way to request it

The persistent source-document record does not need to copy the document body.
For Sessions, it initially stores identity, digest, metadata, and processing
state. Transcript content can remain transient during distillation, with only
the minimum bounded excerpt retained when an artifact needs it for review.

### Evidence reference

A durable pointer from a Noema claim to source evidence:

- Source identity
- Document digest
- Entry, segment, or other source coordinates when available
- Content hash when available
- Bounded excerpt only when required for review

An evidence reference is not proof that the referenced claim is true. It shows
what evidence supported the interpretation.

### Observation

A normalized statement extracted from evidence:

- Kind
- Subject and summary
- Time range
- Confidence
- Evidence references
- Distiller and model version

### Episode

A revisable grouping of related observations:

- Noema-owned identity
- Goal and current state
- Time range
- Project or workspace scope
- Relationships to observations and artifacts
- Grouping confidence

Episodes do not require structured completion or a work item.

### Domain event

A small fact that something meaningful changed:

- Event ID and type
- Subject type and identity
- Occurrence and recording time
- Causation and correlation when known
- Bounded attributes
- Evidence references
- Schema version

Events reference evidence. They do not contain raw transcripts.

### Agent definition

A focused subscriber:

- Name and version
- Subscribed event types
- Allowed retrieval tools
- Instructions and output schema
- Model requirements
- Retry and idempotency policy

The first version may define agents in code. A public plugin format is not
required.

### Agent run

One attempt to process an event:

- Event, agent, model alias, requested model, and resolved provider identity
- Started and finished times
- Status and attempts
- Tool requests or bounded audit metadata
- Validated output or failure
- Prompt and output-schema versions
- Requested privacy and routing policy
- Cost, usage, latency, and provider request identity when available

### Agent artifact

A typed, reviewable result with its own lifecycle. Content ideas are the first
artifact type.

## Initial persistence

SQLite is both the first durable store and the first local queue. Conceptual
tables include:

```text
scan_runs
source_documents
observations
evidence_refs
episodes
episode_observations
artifact_links
events
subscriptions
jobs
agent_runs
content_ideas
content_idea_evidence
```

This is a responsibility map, not a committed migration. The implementation
plan should prefer the smallest schema that preserves these boundaries.

Canonical source state and Noema's derived state have different lifecycles.
Rebuilding Noema's knowledge or search projection must not modify Sessions or
another source.

## Sessions boundary

Sessions is the canonical local library for coding-agent history.

Noema:

- Requires the user to index Sessions explicitly outside Noema.
- Uses versioned JSON or JSONL commands.
- Validates the structured-output schema and trust disposition.
- Uses canonical source identity and document digests for incremental
  processing.
- Requests bounded evidence by default.
- Treats transcript instructions as untrusted history.
- Records whether each scan's source coverage was complete or partial.
- Uses retained evidence from a partial Sessions capture, but never treats
  missing data in an incomplete scope as proof that evidence was removed.

Noema does not:

- Import Sessions source modules.
- Read or write the Sessions SQLite database.
- Parse Cursor or Codex storage.
- Duplicate retained raw transcripts.
- Assume a session or lineage root is a complete work episode.

## Event and queue semantics

The SQLite event store and queue establish the behavior that a later queue
implementation must preserve:

- Events are append-only.
- Event schemas are versioned.
- An event becomes visible only after its related knowledge change commits.
- Subscription matching is deterministic.
- Jobs are durable and at-least-once.
- Agents are idempotent for an event and agent version.
- Failures and retry attempts remain inspectable.
- Replaying an event cannot grant more external authority.
- An agent upgrade can be evaluated against retained events without silently
  replacing old artifacts.

The experimental V0 stores the job and makes one synchronous attempt. A failed
job is terminal and inspectable. It does not yet claim at-least-once delivery,
retry safety, leases, or replay; those remain target queue semantics to add only
after the first agent path proves useful.

Inngest, Cloudflare Queues, or Cloudflare Workflows may later implement parts
of this execution model. Their run identifiers and status values remain
adapter details.

## Retrieval architecture

Noema follows a staged retrieval path:

1. Structured relationships and metadata.
2. Full-text search over normalized knowledge.
3. Embedding similarity when real queries show a semantic gap.
4. Fusion, deduplication, reranking, and context expansion when multiple
   retrievers exist.

Every retriever returns a shared evidence-result shape. Agents depend on that
shape rather than SQLite, a vector store, or a provider SDK.

Facets should come from meaningful metadata, not speculative columns. Initial
facets may include time range, project or workspace, observation kind, source
kind, artifact kind, and confidence.

## Agent runtime

The standalone application ultimately owns model invocation through a
provider-neutral interface. Existing Codex sessions may help prototype and
evaluate instructions, but production behavior does not require a human to
open Codex and copy evidence between commands.

The model boundary may point to a local or remote provider. A remote provider
must be explicitly configured. Noema must apply its deterministic privacy
filter before sending bounded evidence, and the user must be able to understand
which classes of data can leave the machine. Selecting the first provider does
not weaken the local-by-default product boundary.

The runtime:

1. Claims a subscribed job.
2. Loads the event envelope and agent definition.
3. Exposes only the allowed retrieval tools.
4. Invokes the configured model.
5. Validates the typed output.
6. Stores the run and artifact.
7. Publishes any resulting internal event.
8. Completes or retries the job.

A model provider is not allowed to become the domain boundary. Provider
responses are parsed and validated before entering application state.

## Model gateway

Noema owns a small structured-generation interface. Distillers and agents call
that interface using a task-level model alias, instructions, bounded input, and
an output schema. They do not import a provider SDK, use a provider model name,
or interpret a provider response directly.

The first remote implementation uses Vercel AI Gateway through its
OpenAI-compatible Chat Completions API. The adapter may use the official OpenAI
Go client, but its request and response types remain inside the adapter. Vercel
is an initial transport and routing choice, not part of Noema's domain model.

Model aliases resolve through configuration. A route includes:

- The gateway and canonical model identifier.
- An explicit provider allowlist and order.
- Required capabilities, including JSON Schema structured output.
- Privacy requirements such as zero data retention and no prompt training.
- Timeouts, token limits, and retry policy.

Initial evaluation routes use `openai/gpt-oss-120b` served by Cerebras for
distillation and `openai/gpt-5.4-mini` served by Azure for Content Scout. Both
request zero data retention and no prompt training. These are configurable
starting points, not permanent agent dependencies.

Noema does not rely on gateway defaults for provider selection. Evaluation runs
pin the provider so latency, cost, and output quality remain comparable.
Production routes may allow an explicit fallback set only when every route
satisfies the same capability and privacy policy.

The first experimental slice pins exactly one provider per route and rejects
multi-provider configuration. Comparison models are separate, explicitly
selected routes. Automatic fallback remains a later production option, not V0
behavior.

Every model result is validated against Noema's local output schema before it
can create an observation or agent artifact. OpenAI compatibility only
standardizes the transport; it does not prove that providers implement
parameters or schemas identically.

Remote model execution is opt-in. Before a request leaves the machine, Noema
applies its deterministic privacy filter and checks the configured route. If a
required retention, training, provider, or structured-output guarantee cannot
be requested, the call fails. V0 has no weaker-policy override. No configured
remote route means no remote request.

Agent runs record enough information to explain and compare model behavior:

- Model alias and requested canonical model.
- Gateway, resolved inference provider, and resolved provider model.
- Agent, instructions, and output-schema versions.
- Privacy and routing policy requested.
- Input and output token usage, latency, and cost when available.
- Gateway generation or provider request identity when available.
- Validation outcome and bounded failure details.

## Content Scout

Content Scout is the first agent and the acceptance test for the architecture.

The first experimental slice writes one `scan.completed` event after a scan's
new observations and granular domain events in the same atomic commit. The
event carries the scan identity and bounded observation identities, not their
evidence bodies. It is emitted only when the scan has at least one new
observation. This creates one Content Scout job and preserves the limit of five
ideas across the whole scan.

Later versions may also let Content Scout react to individual events that can
signal useful content, including:

```text
insight.observed
decision.recorded
failure.observed
experiment.completed
episode.updated
```

It looks for practical lessons about AI-assisted software development,
including tips, explanations, experiences, mistakes, experiments, and informed
opinions.

One scan returns at most five ideas ranked by strength. It does not force
variety between content styles and does not fill a quota with weak ideas.

Each idea contains:

- Concept and core lesson
- Intended audience benefit
- Possible hook
- Reason it may resonate
- Suitable short-post, thread, and article angles
- Evidence references
- Confidence and rank

The agent may return no ideas. It does not write a complete draft or publish
content.

Ideas have stable fingerprints. Overlapping scans do not repeat an unchanged
idea. New independent evidence may strengthen and resurface an existing idea.

## First vertical slice

The first implementation should prove one path through every required layer:

```text
Manual date-range scan
        ↓
Sessions structured output
        ↓
Incremental source-document records
        ↓
Observation and insight distillation
        ↓
SQLite knowledge records + evidence refs
        ↓
Durable domain events
        ↓
SQLite subscription jobs
        ↓
Content Scout model invocation
        ↓
Validated content-idea artifacts
        ↓
CLI review
```

The slice is complete when:

- The same unchanged range can be scanned again without duplicate knowledge,
  jobs, or ideas.
- Changed source evidence produces a new immutable chunk version, observations,
  and events.
- Content Scout receives bounded, traceable evidence through retrieval
  operations.
- A successful scan shows no more than five ranked idea cards.
- Every idea can be traced to its supporting Sessions evidence.
- Sensitive details are excluded or generalized according to the project
  intent.
- Empty results and agent failures are honest and inspectable.
- No source data is modified and no content is published.

## Growth path

After the first slice is useful:

1. Add idea decisions and selection history.
2. Add full-text retrieval over normalized observations.
3. Add Workflow Scout to test that agent subscriptions are general.
4. Add a second source to test that ingestion is general.
5. Add embeddings only when measured retrieval misses justify them.
6. Add draft generation after idea selection creates useful feedback.
7. Add scheduled or durable remote execution when manual local execution is
   the limiting factor.

Possible later mappings:

| Need | Possible implementation |
| --- | --- |
| Durable multi-step agents and approvals | Inngest or Cloudflare Workflows |
| Remote event delivery | Cloudflare Queues |
| Remote relational projections | Cloudflare D1 |
| Semantic retrieval | Local vector index or Cloudflare Vectorize |
| Large derived artifacts | Local files or Cloudflare R2 |

These are options, not commitments. Raw private source evidence does not move
to remote infrastructure without a separate privacy design.

## Accepted decisions

- Noema is separate from Harness and Sessions.
- Sessions is the first source and remains the canonical coding-session
  library.
- Go and SQLite are the first implementation stack.
- Noema owns derived knowledge, events, queue state, agent runs, and outputs.
- Work episodes are inferred Noema records, not Factory or workflow work items.
- Events trigger focused agents; evidence is retrieved separately.
- The local queue comes before a remote workflow engine.
- Structured and full-text retrieval come before embeddings.
- Content Scout is the first agent.
- The first execution mode is a manual date-range scan.
- The first interface is a CLI.
- The first artifact is an evidence-backed content idea, not a complete draft.
- Agent outputs require human review before external action.
- Noema owns a provider-neutral structured-generation interface.
- Vercel AI Gateway is the initial remote model-gateway adapter.
- The initial gateway transport is OpenAI-compatible Chat Completions.
- Models and inference providers are selected through explicit, configurable
  routes rather than agent code.
- Gateway output is validated locally before it enters Noema's derived state.
- The first Content Scout subscription uses one `scan.completed` event so a
  scan creates at most one Content Scout job.

## Deferred decisions

The implementation plan must either resolve these choices or keep them behind a
small boundary:

- Exact gateway authentication and configuration file shape.
- Exact Go SQLite driver.
- Schema migration tool.
- Structured-output validation library.
- Whether distillation and Content Scout use the same model.
- How evidence is previewed safely in the CLI.
- How the privacy filter combines deterministic rules and model review.
- The first command grammar and configuration-file format.
