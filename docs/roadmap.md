# Roadmap

- Status: accepted product roadmap
- Date: 2026-07-22
- Updated: 2026-07-28

## Purpose and authority

This roadmap turns Noema's accepted product intent and architecture into an
ordered path. It owns milestone sequence, completion gates, and the conditions
that justify later work.

- [Project intent](project-intent.md) owns purpose, priorities, hard boundaries,
  and unsafe assumptions.
- [Architecture](architecture.md) owns system boundaries and component
  responsibilities.
- This roadmap owns sequence and decision gates.
- `dev/plans/` contains temporary, executable plans for one milestone at a
  time.

## Locked product model

```text
Sessions evidence plane
  canonical coding-agent history
  structural normalization
  provenance and retrieval
          ↓
Noema
  deterministic facts
  semantic claims
  optional summaries
  durable domain events
  transactional outbox
          ║
          ║ Noema core ends
          ║
          ↓
External transport
          ↓
Independent consumers
```

The following decisions are fixed:

- Sessions owns canonical coding-session evidence. Noema consumes only its
  public, versioned CLI JSON or JSONL contract.
- Noema does not parse provider formats, read the Sessions database, or create
  another complete transcript archive.
- Canonical evidence is authoritative for what was recorded. Noema facts,
  claims, summaries, and events are derived, versioned, and rebuildable while
  the referenced evidence remains available.
- Code extracts observable facts before a model interprets meaning.
- Facts and claims remain separate authority classes and validation paths.
- Every derived record retains exact source coordinates.
- Source identity plus digest identifies an evidence revision; bounds and
  coverage belong to the consuming analysis.
- Stored coordinates resolve only against the recorded document digest.
- Every processing attempt records stage, evidence, scope, versions, outputs,
  and status in an `AnalysisRun`.
- Summaries are optional projections over admitted facts and claims.
- Evidence admission, extraction, summaries, and events remain
  use-case-neutral.
- Noema commits domain events and outbox records atomically with the state
  changes they describe.
- A generic publisher may deliver an event, but Noema owns no subscribers,
  agents, consumer jobs, workers, retries, execution runs, receipts, or
  consumer outputs.
- Content Scout, coding coaching, and workflow improvement are external
  consumers. Adding or changing one requires no Noema code.
- Go, SQLite, manual execution, and human inspection remain the V0 Noema
  operating model.
- V0 starts with one explicitly selected canonical session.

## Foundation: runtime spine

- Status: proof completed; scaffold removed
- Evidence: PR #5

The foundation proved:

- a Go CLI and composition root;
- durable SQLite records across process boundaries;
- atomic producer writes;
- stage-specific fingerprints and unchanged-run reuse;
- inspectable terminal failures; and
- an end-to-end fake proof.

It also introduced scans, observations, jobs, agent runs, and content ideas.
The event-boundary cutover removed those types, tables, commands, and tests.
Pre-V1 databases are rejected with an instruction to create a clean database;
there is no compatibility reader, backfill, dual write, or mixed-schema path.

The lesson retained from the scaffold is atomic durable handoff. The
producer/worker and agent-runtime design is not retained.

## V0 Milestone 1: canonical evidence and deterministic facts

- Status: implemented

### Goal

Understand one explicitly selected Sessions session without a model call.

### Scope

- Accept one canonical Sessions identity supplied by the user.
- Invoke `sessions export '<id>' --format jsonl --full`.
- Validate schema, trust disposition, identity, digest, coordinates, bounds,
  omissions, and coverage.
- Reconstruct ordered entries transiently without storing a transcript copy.
- Persist source and processing metadata needed to explain the attempt.
- Preserve complete useful evidence coordinates.
- Deterministically identify supported tool calls, results, commands, tests,
  outcomes, errors, files, packages, URLs, metadata, and verification.
- Store facts with structured values, parse rules, versions, and exact evidence.
- Inspect admitted metadata, facts, omissions, and failures locally.

### Gate

- No model, gateway configuration, or remote request is involved.
- Exact unchanged input creates no duplicate facts.
- Every stored fact has valid Sessions evidence.
- Resolution requires the recorded document digest and fails closed otherwise.
- Partial or unsupported evidence is never presented as complete.
- Narrative assertions cannot become observed successful outcomes.
- Changed canonical documents create new derived identities.
- Fixtures exercise the public Sessions contract without private data.

### Not included

- Semantic interpretation.
- Domain events.
- Multi-session discovery.
- Raw transcript retention.

## V0 Milestone 2: validated semantic claims

- Status: complete

### Goal

Turn bounded canonical evidence and deterministic facts into admitted,
evidence-backed meaning.

### Initial vocabulary

- problem
- symptom
- hypothesis
- failed attempt
- root cause
- decision
- solution
- verification
- lesson

Each claim records statement, status, confidence, supporting and contradicting
evidence, optional facts, scope, attribution when relevant, and complete
processing identity.

### Scope

- Add a provider-neutral structured-generation boundary.
- Require explicit remote opt-in, a pinned route, bounded inputs, deterministic
  privacy filtering, and recorded retention and training choices.
- Treat model output as untrusted candidates.
- Validate schema, evidence, confidence, status, contradiction, privacy, and
  consistency with stronger deterministic facts.
- Store admitted claims, processing identity, safe failures, and
  consumer-neutral domain events.
- Record a separate semantic `AnalysisRun`.
- Keep summaries optional and rebuildable.
- Commit the semantic analysis, claims, granular events, and
  `analysis.completed` event atomically.

### Gate

- Every admitted claim passes local schema, evidence, privacy, contradiction,
  and deterministic-consistency checks.
- Reviewed fixtures and approved sessions measure semantic support quality.
- Facts remain distinguishable from model interpretations.
- Protected content is blocked before remote transmission and after output.
- Failures expose safe categories without retaining provider or model prose.
- Changed semantic configuration can rerun without reindexing.
- Claim extraction contains no content, personal-development, or workflow
  recommendation fields.

### Evaluation checkpoint

The initial 12-case V8 baseline admitted 14 claims. Human review found 10
supported, three partly supported, and one unsupported. That result activated
the existing verification checkpoint.

The smallest response was a V9 one-pass prompt correction. On the same 12
cases, it admitted 10 claims; all were supported and nine were useful. No
unsupported or stronger-evidence-contradicted claim survived, so the checkpoint
closed without a second model call.

The immutable V2 corpus preserves those 12 cases and adds eight for scope,
causality, chronology, separation, decision state, rationale, and prompt
injection. Its approved run completed all 20 requests. Eighteen batches passed
local admission. Human review judged all 20 admitted claims supported and 19
useful.

The known limitation is conservative recall: explicit decisions, a reusable
lesson, a confirmed root cause, and one implemented change were missed or
rejected. Carry those limits into real downstream evaluation. Do not weaken
admission or add a second verifier without new evidence.

### Deferred checkpoints

- Add a knowledge-unit projection only if claims prove too granular.
- Add a session or phase summary only if it materially improves inspection or
  bounded retrieval.
- Add a second model verification pass only if unsupported or contradicted
  claims survive local validation again.

## V0 Milestone 3: durable event publication boundary

- Status: implemented

### Goal

Finish Noema at a reliable, consumer-neutral event boundary.

### Scope

- Define the small V1 `DomainEvent` and `OutboxRecord` contracts.
- Persist append-only events and outbox records in the same SQLite transaction
  as the derived state change.
- Keep event identity independent of transport and consumers.
- Add local list/show inspection for events and outbox status.
- Add one generic publisher application port.
- Add a fake publisher and one visible local JSONL adapter.
- Add an explicit one-shot publication command.
- Mark an outbox record delivered only after a bounded acknowledgement.
- Preserve a safe failure category and leave failed records eligible for an
  explicit later attempt.
- Remove the pre-V1 scan, observation, job, agent-run, and content-idea runtime
  instead of migrating it.

### Gate

- The knowledge change, event, and outbox record are atomically visible.
- Every event references valid Noema-owned state.
- Events contain bounded metadata and references, not transcript bodies.
- No event or outbox type contains a consumer, agent, prompt, model, tool,
  subscription, job, execution, receipt, or output field.
- Exact unchanged processing creates no duplicate events.
- Repeating publication after an acknowledged delivery creates no duplicate
  delivery.
- A failed attempt leaves the event and outbox record inspectable and safe to
  retry explicitly.
- Publication is at least once. If delivery may have succeeded but the local
  acknowledgement did not commit, a later attempt may repeat the same stable
  event ID for external deduplication.
- Delivery means the transport acknowledged the event; no command claims that a
  consumer ran or succeeded.
- Routine tests are offline and use no credential or hosted service.
- No daemon, scheduler, lease manager, automatic retry loop, or hosted queue is
  introduced.

### Transport checkpoint

Start with the smallest visible adapter. Choose Inngest, Cloudflare, or another
hosted transport only after the local outbox proves:

- which events consumers need;
- what metadata is sufficient;
- what delivery guarantees matter; and
- that manual publication is the actual limit.

## V0 completion gate

V0 is complete when:

- One explicitly selected real session can traverse facts and semantic claims.
- Its derived state emits inspectable consumer-neutral events.
- The same transaction retains publication intent.
- A generic manual publisher can deliver an event idempotently.
- Safety, evidence, claims, events, and publication state can be inspected
  without reading Noema internals.
- Noema contains no agent or consumer execution model.

Useful downstream output is not a Noema V0 gate. It is the next product
experiment against this boundary.

## First downstream experiment: Content Scout

- Status: external follow-up after Milestone 3

### Goal

Prove that Noema knowledge can produce useful, safe content ideas without
coupling the knowledge system to the consumer.

### Ownership

Content Scout owns:

- its event trigger or Inngest function;
- any Noema read client or event-to-input preparation;
- privacy and disclosure policy for public content;
- Eve, AI SDK, or another model runtime;
- prompt, schema, model route, Flex-tier choice, tools, and retries;
- execution state and receipts;
- content ideas and feedback; and
- human review before publication.

Noema owns none of those.

### First proof

- Subscribe to `analysis.completed`.
- Resolve the referenced analysis through a bounded public Noema read surface.
- Produce zero to five ideas.
- Show how each strong idea could work as a short post, thread, or article.
- Preserve claim and evidence references in consumer-owned records.
- Keep all results as proposals.

An empty result is valid. The consumer does not publish content automatically.

### Gate

- Changing the Content Scout model, prompt, runtime, or deployment requires no
  Noema change.
- Disabling Content Scout does not change Noema event identity or processing.
- Every retained idea traces to admitted knowledge.
- Private details are generalized or blocked.
- Human review identifies at least some ideas worth developing.

## Later phases, gated by use

The expected order is:

1. **Content feedback.** Record keep, reject, and reasons in Content Scout.
2. **Knowledge units when needed.** Consolidate claims only if individual
   claims are too granular or lessons recur.
3. **Incremental session windows.** Process only new or changed windows in one
   growing session.
4. **Multi-session analysis.** Add manifest-backed evidence sets, bounded
   ranges, correlation, and revisable episodes.
5. **Coding coach.** Build an independent consumer that identifies development
   patterns and concrete learning goals.
6. **Draft generation.** Add complete content only after idea-selection
   feedback exists.
7. **Second source.** Add Git, tests, notes, or another source when Sessions
   lacks decisive evidence.
8. **Workflow improvement.** Build an independent consumer for evidence-backed
   proposals about skills, instructions, tools, configuration, and tests.
9. **Full-text derived retrieval.** Index Noema-owned facts, claims, and
   summaries when exact and metadata queries become limiting.
10. **Semantic retrieval.** Add embeddings only when measured queries show a
    gap.
11. **Scheduled ingestion.** Automate Noema scans only when useful manual runs
    are regularly missed.
12. **Hosted event transport.** Adopt Inngest or Cloudflare when the manual
    publisher is the limiting factor.

### Incremental session windows

This phase plans one current revision into ordered, contiguous windows. It:

1. keeps related conversational and tool context together;
2. stores bounds, reasons, provisional state, and content fingerprints but no
   transcript body;
3. fingerprints content without the whole-document digest so unchanged windows
   can be recognized after an append;
4. previews locally and requires explicit approval for a bounded number of new
   semantic attempts;
5. reuses only when complete processing identity matches;
6. treats the trailing window as provisional; and
7. stops on the first failed window while retaining completed work.

Cross-revision reuse never rebinds old evidence to a new digest. The phase
remains manual and single-session. It adds no manifest, scheduler, retry
service, or model-based range selector.

### Multi-session analysis

This phase adds one inventory-first operation:

1. Select one atomic transcript-free Sessions manifest.
2. Persist one explicit `EvidenceSet` with filters, capture scope, ordered
   revision identities, counts, and outcomes.
3. Match revisions against exact completed processing identities.
4. Hydrate only misses and validate identity and digest.
5. Mark unavailable or mismatched hydration incomplete without substitution.
6. Keep fact and claim processing per revision.

Manifest completeness, capture scope, hydration outcomes, and per-document
coverage remain distinct. Source roots and lineage do not become episodes.

### Coding coach

The coding coach starts only after multi-session selection and attribution are
trustworthy. It uses the same published Noema knowledge rather than adding an
evaluation-specific extractor.

Its consumer-owned assessment includes:

- evaluated scope and evidence coverage;
- growth areas or development patterns;
- observed behavior, likely impact, recurrence, and confidence;
- supporting and contradicting evidence;
- user, agent, environment, mixed, or unknown attribution; and
- a concrete learning goal, exercise, and success criterion.

It does not score identity, personality, or general ability. A single failure
is not a weakness. An empty assessment is valid.

## V0 non-goals

- A Noema-owned raw transcript archive.
- Direct Codex or Cursor parsing.
- Broad or ambient scanning.
- A knowledge-unit layer before evidence shows a need.
- A second model verification pass by default.
- Full drafts or autonomous publication.
- Any Noema-owned agent, subscription, consumer job, queue, dispatcher, run,
  receipt, or consumer artifact.
- Coding assessments or workflow proposals inside Noema.
- A daemon, scheduler, automatic outbox retries, leases, or replay service.
- PostgreSQL, a graph database, embeddings, or a vector database.
- A hosted event transport, Cloudflare deployment, web UI, or public plugin
  system.

These are not missing V0 work. Each has a named trigger above or belongs to an
independent consumer.

## Preserved product hypotheses

- **Work Graph / Flight Recorder.** Reconstruct revisable work episodes with
  goals, scope changes, decisions, failures, validation, and a cited resume
  card.
- **Improvement Inbox.** Present evidence-backed create or fix proposals.
  Content, coding growth, and workflow improvement remain separate consumers.
- **Deterministic Agent Eval Lab.** Compare prompts, models, tools, and
  configuration in replayable environments. This evaluates agent systems; it
  is not Noema's knowledge core.
- **Context X-Ray.** Explain which instructions, skills, tools, hooks, and model
  configuration influenced a run.
- **Recovery supervisor.** Suggest recovery from blocked work only after the
  evidence model and authority design are trustworthy.

These are hypotheses, not Noema implementation promises.
