# Finish Noema at a consumer-neutral event boundary

- Status: approved 2026-07-28
- Supersedes: `260727-content-scout.md`
- Roadmap:
  [V0 Milestone 3: durable event publication boundary](../../docs/roadmap.md#v0-milestone-3-durable-event-publication-boundary)

## Goal

Remove the pre-V1 agent runtime and finish Noema at a durable domain-event and
transactional-outbox boundary. A state change, its event, and its publication
intent commit atomically. A generic one-shot publisher can hand the event to a
transport and record acknowledgement without knowing which consumers exist.

This plan deliberately does not build Content Scout, call Eve, match
subscriptions, create consumer jobs, run a worker, admit agent output, or store
consumer artifacts. Those responsibilities move to independent consumer
projects and an external workflow or event platform.

The cutover preserves implemented fact and semantic analysis behavior,
processing identities, evidence lineage, remote authority, privacy checks,
evaluation tools, and existing consumer-neutral events.

## Why the prior plan was superseded

The previous Content Scout plan placed too much downstream execution inside
Noema:

- Content Scout subscription matching;
- a SQLite agent-job queue;
- a Go dispatcher;
- a Noema-to-Eve HTTP adapter;
- agent configuration and execution contracts;
- run and receipt persistence; and
- content-idea artifact admission.

Making those contracts “generic” did not remove the coupling. Noema would
still own consumer identity, execution, retry, and output concerns already
handled by Inngest, Eve, or another consumer runtime. The accepted boundary is
now explicit in project intent and architecture.

## Delivery slices

```text
1. Domain event and outbox contracts
       ↓
2. Clean V1 persistence cutover
       ↓
3. Atomic semantic event + outbox commit
       ↓
4. Inspection and generic one-shot publisher
       ↓
5. Cleanup proof and documentation
```

Each slice must keep `make check` passing. The plan is intentionally serial
because the schema and contract cutover are small and the later slices depend
on the final event shape.

## Changes

1. **Define consumer-neutral event and outbox contracts.**

   Update `internal/domain/` and focused application ports:

   - Keep or rename the existing event value as `DomainEvent`.
   - Require event ID, fingerprint, type, subject type and ID, bounded payload,
     creation time, and schema version.
   - Keep evidence or record references only when they are part of the
     consumer-neutral event contract.
   - Add `OutboxRecord` with event ID, status, attempt count, safe last failure
     category, delivered time, and bounded acknowledgement identity.
   - Define strict pending and delivered invariants.
   - Use the stable event ID as the V0 publication identity.
   - Add validation and fingerprint tests.

   Reject any field for subscriptions, agents, prompts, models, tools, jobs,
   execution deadlines, runs, receipts, consumer outputs, or Content Scout.

2. **Replace the pre-V1 runtime schema with the clean event schema.**

   Update `internal/adapters/sqlite/migrations/`, `store.go`, and focused store
   files:

   - Preserve `analysis_runs`, `facts`, `semantic_analysis_details`, and
     `claims`.
   - Make `events` authoritative for subject type and schema version rather
     than using the temporary `event_subject_types` sidecar.
   - Add `event_outbox` with a foreign key to `events` and unique stable
     publication identity.
   - Remove `scans`, `evidence_chunks`, `observations`, `jobs`, `agent_runs`,
     `content_ideas`, and the event-subject sidecar from the clean schema.
   - Remove legacy SQLite read and write paths for those tables.
   - Keep the database a disposable pre-V1 cutover: no import, backfill,
     migration reader, dual write, or mixed-schema support.

   Rework migration tests against fresh databases. Explicitly document that
   existing pre-V1 local databases must be recreated.

3. **Commit semantic events and outbox records atomically.**

   Update `internal/application/semantic_persistence.go`,
   `semantic_workflow.go`, and `internal/adapters/sqlite/semantic_store.go`:

   - Build consumer-neutral knowledge events exactly once from the admitted
     semantic analysis.
   - Create one outbox record per event in the same `BEGIN IMMEDIATE`
     transaction that stores the analysis and claims.
   - Make an exact semantic rerun reuse the completed analysis, events, and
     outbox records without inserting duplicates.
   - Validate that every event subject points to state committed in the same or
     an earlier valid transaction.
   - Keep failed semantic attempts free of events and outbox records.
   - Keep event creation independent of publisher configuration.

   Do not add Content Scout matching or a consumer-prepared payload.

4. **Add inspection and one generic publication path.**

   Add focused application and SQLite ports plus CLI commands:

   ```text
   noema events list [--status pending|delivered] [--database <path>]
   noema events show <event-id> [--database <path>]
   noema events publish --once [--database <path>]
   ```

   The exact command names may be simplified while preserving these
   responsibilities:

   - inspect the oldest pending outbox event without changing state;
   - send one versioned event envelope through a generic `EventPublisher`;
   - record a delivered acknowledgement atomically;
   - leave a failed record pending with an incremented attempt count and one
     fixed safe failure category; and
   - return no-work, delivered, or failed status without remote body text.

   Begin with a fake publisher for tests and one visible local adapter such as
   JSONL or stdout. Do not choose Inngest or Cloudflare in this plan unless the
   local proof exposes a requirement that cannot be represented by the small
   port.

   Repeated publication after a successfully committed acknowledgement must be
   a no-op. Concurrent one-shot attempts must not intentionally publish the
   same pending record. Use the smallest SQLite transaction mechanism needed
   for that proof; do not add a daemon, lease service, or retry scheduler.

   Publication is at least once, not exactly once. A crash or timeout after the
   adapter accepted an event but before Noema committed the acknowledgement may
   cause the same stable event ID to be delivered again. Keep that ambiguity
   visible and require external deduplication; do not add distributed
   exactly-once machinery.

5. **Remove the agent scaffold and prove the boundary.**

   Delete:

   - `internal/application/scan.go`;
   - `internal/application/worker.go`;
   - agent registry and ports;
   - scan, observation, job, run, content-idea, and completion domain types;
   - SQLite job and content-idea operations;
   - `worker`, `jobs`, and `ideas` CLI commands; and
   - the fake source/agent spine tests that only prove the rejected runtime.

   Replace them with an offline integration test that:

   1. seeds or produces a valid fact analysis;
   2. completes a semantic analysis;
   3. closes and reopens SQLite;
   4. inspects the committed event and pending outbox record;
   5. publishes through a fake adapter;
   6. inspects the delivered acknowledgement; and
   7. proves exact reruns and repeated publication create no duplicate event or
      delivery.

   Keep all Sessions, fact, semantic, Gateway conformance, and evaluation tests
   passing.

6. **Update operator and contributor documentation.**

   Update `README.md` and `docs/contributing/` for:

   - the event inspection and one-shot publication commands;
   - disposable pre-V1 database recreation;
   - the difference between event creation, transport acknowledgement, and
     consumer completion;
   - offline routine checks versus any later live transport check; and
   - the rule that consumers use public events and future bounded reads rather
     than Noema SQLite.

   Keep Content Scout, Eve, Inngest functions, consumer credentials, and
   consumer output schemas out of Noema setup and tests.

## Verify

- Focused domain and application tests for event and outbox validation,
  fingerprinting, exact reuse, and failure categories.
- SQLite tests for atomic event/outbox insertion, fresh-schema validation,
  one-shot publication state changes, and concurrent attempts.
- CLI tests for list, show, no-work, delivered, and safe failed publication.
- Offline integration proof across fresh SQLite connections.
- Existing fact, semantic, Gateway conformance, and evaluation regression
  suites.
- `make check`.

No live transport, Eve process, consumer, credential, or private session is
required to complete this plan.

## Boundaries

- Do not add subscriptions, agent definitions, consumer jobs, queue leases,
  worker loops, dispatch, agent execution, run receipts, or consumer artifacts.
- Do not add Content Scout fields or configuration to event payloads.
- Do not make Noema call Eve or another consumer runtime.
- Do not let a publisher report consumer completion.
- Do not add automatic retry, scheduling, a daemon, Inngest, Cloudflare, or a
  public plugin system.
- Do not add summaries, knowledge units, embeddings, manifests, multi-session
  analysis, or another source in this cutover.
- Do not weaken semantic processing identities, evidence checks, privacy
  behavior, or immutable event identity.
- Do not preserve backward compatibility with the pre-V1 agent scaffold.
