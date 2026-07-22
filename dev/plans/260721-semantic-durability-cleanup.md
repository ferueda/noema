# Simplify semantic durability internals

- Status: implemented
- Follows: Milestone 2 durability slice
- Precedes: Milestone 2 remote Gateway slice
- Parent plan: [Admit evidence-backed semantic claims](260721-milestone-2-semantic-claims.md)

## Goal

Reduce the review and maintenance cost of the semantic durability code before
adding remote model execution. Preserve every implemented behavior: exact
reuse, one-at-a-time V0 generation, atomic claims and events, progressive
failure details, durable identity validation, inspection, and digest-locked
evidence resolution.

The cleanup is complete when the application preparation boundary is private,
the SQLite semantic store is split by responsibility, and event fingerprint
construction has one canonical helper.
Existing database rows, fingerprints, IDs, CLI JSON, and processing behavior
must remain unchanged.

## Changes

1. `internal/application/semantic_analysis.go:PreparedSemanticAnalysis` and
   `SemanticAnalyzer.Prepare`, `GeneratePrepared`, and `AdmitPrepared` — make
   the preparation value and phase methods package-private because only the
   application workflow composes them. Preserve the current progressive,
   nullable fields so a failed preparation records only values it actually
   established. Keep the canonical complete-preparation validation before
   generation and again before admission: the first protects the remote-call
   boundary, while the second detects mutation by an injected generator before
   claims are admitted. Keep `SemanticAnalyzer.Run` and
   `SemanticWorkflow.Run` as the stable application entry points. Extend
   `internal/application/semantic_analysis_test.go` with a generator that
   mutates an aliased request value during `Generate`, returns otherwise valid
   output, and proves the second validation rejects admission before claims or
   events can commit.

2. `internal/application/semantic_workflow.go:semanticDetailsFromPreparation`
   — consume the package-private progressive value when recording failures;
   after successful preparation, that same value contains the complete inputs
   required by reuse, generation, and admission. Preserve the current
   unavailable-versus-known-empty rules and every existing bounded failure
   category; this refactor must not turn absent values into empty values or
   expose an analysis ID whose failure record did not commit.

3. `internal/adapters/sqlite/semantic_store.go` — split the current store into
   focused files in the same package: keep the public store and immediate
   transaction lifecycle in `semantic_store.go`, move SQL encoding and loading
   to `semantic_store_records.go`, and move durable record, claim, and event
   checks to `semantic_store_validation.go`. Preserve the
   `SemanticAnalysisStore` and `SemanticAnalysisAttempt` contracts, transaction
   boundaries, SQL statements, row ordering, migration 003, and load-time
   validation. This is a file-ownership cleanup, not a new repository layer.

4. Add `internal/application/event_identity.go` with one canonical
   `EventFingerprint(eventType, subjectType, subjectID string, payload
   map[string]any) (string, error)` function. It is a pure wrapper around the
   exact current fingerprint input: event type, subject type, subject identity,
   and payload in their existing serialized shape. It does not validate the
   event, include evidence or time, or derive an event ID. Use it from
   `Scanner.newEvent`, semantic event construction, and SQLite semantic event
   verification. Preserve the existing foundation and semantic event ID
   derivation rules and all retained fingerprints; do not rewrite or migrate
   events merely to make their ID formats uniform.

   Before replacing the three call sites, add fixed compatibility vectors in
   `internal/application/event_identity_test.go` for one foundation event and
   one semantic event. Hard-code the pre-cleanup fingerprint and ID values; do
   not calculate the expected values with `EventFingerprint` or either current
   constructor. The vectors must prove both existing ID rules remain unchanged:
   foundation events use `"evt_" + fingerprint[:32]`, while semantic events use
   `platform.DerivedID("evt_", fingerprint)`.

5. Update the existing application, SQLite, integration, and CLI tests beside
   the moved code. Consolidate repeated fixtures only within their current
   package when that makes the proof clearer. Keep distinct coverage for
   preparation progress, exact reuse, two-database-handle serialization,
   rollback, tamper rejection, migration compatibility, known-empty results,
   CLI inspection, and digest-locked resolution. Do not introduce a shared
   test-support package solely to reduce line count. Add one focused
   workflow-to-SQLite round trip under `internal/integration/` in which an
   analysis with zero selected input facts fails after preparation. Reload the
   failed run and require `Details.InputFactIDs` and `Run.InputFactIDs` to be
   non-nil empty lists while `Details.ClaimIDs` remains unavailable.

## Delivery order

1. Capture the pre-cleanup event compatibility vectors, add
   `EventFingerprint`, and cut over all three fingerprint call sites together.
2. Make the preparation value and phase methods package-private, updating their
   package-local callers and the two mutation-boundary tests atomically.
3. Split the SQLite store mechanically, then consolidate only test setup whose
   ownership remains obvious inside its current package.

## Verify

- Run `go test -race ./internal/application ./internal/adapters/sqlite ./internal/integration ./cmd/noema`.
- Run `make check`.

## Boundaries

- Do not change SQLite schemas, migrations, durable JSON shapes, processing
  keys, claim fingerprints, event fingerprints or IDs, CLI output, or public
  command behavior.
- Do not replace the claim projection with a JSON-backed record in this
  cleanup. Revisit storage shape only when real queries or migration pressure
  show that the current projection is a problem.
- Do not add the Gateway adapter, route loader, remote command, model SDK,
  summaries, knowledge units, subscriptions, jobs, or agents.
- Do not weaken validation or remove a distinct proof merely to reduce line
  count. Stop and revise the plan if a proposed simplification changes a
  durable identity or observable failure contract.
