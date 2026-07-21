# Process one Sessions snapshot into deterministic facts

- Status: implemented
- Roadmap: [V0 Milestone 1](../../docs/roadmap.md#v0-milestone-1-canonical-evidence-and-deterministic-facts)

## Goal

Implement the first real Noema workflow: a user selects one canonical Sessions
identity, Noema reads and validates that retained snapshot through the public
Sessions schema 1 JSONL export, extracts only mechanically supported facts, and
stores an inspectable fact analysis without a model or remote request.

The milestone is complete when:

- `noema scan sessions <canonical-id>` processes `sessions export
  <canonical-id> --format jsonl --full` without invoking `sessions index` or
  reading Sessions storage directly;
- every admitted fact has a stable processing identity and exact source,
  document-digest, entry, and segment coordinates when applicable;
- an exact unchanged rerun reuses the completed analysis and creates no
  duplicate facts;
- a changed Sessions document digest creates a distinct analysis without
  overwriting earlier lineage;
- stored evidence resolves only against its recorded document digest and a
  mismatch returns `source-revision-unavailable`;
- omissions, unsupported structures, unknown outcomes, and failed analyses are
  visible through the CLI; and
- no event, job, agent, gateway, or Content Scout configuration participates in
  evidence admission or fact identity.

The current Sessions export does not expose workspace or provider paths. Record
only metadata present in the public contract; do not recover excluded metadata
from Sessions internals or infer it from transcript text.

## Current inventory

| Area | Keep | Change in this milestone | Leave for later |
| --- | --- | --- | --- |
| CLI and composition | `cmd/noema`, explicit SQLite path, fail-closed real commands | Wire the explicit-session scan and fact-analysis inspection commands | Real worker composition remains Milestone 3 |
| Source boundary | The `Source` seam proved replaceable input | Add a real Sessions CLI reader for schema 1 JSONL; the accepted workflow does not use time-range `ScanRequest` | More sources and multi-session discovery |
| Knowledge model | Stable IDs, fingerprints, evidence lineage | Add `EvidenceRevision`, complete Sessions coordinates, `AnalysisRun`, and a separate deterministic `Fact` type | Semantic claims, summaries, knowledge units, and episodes |
| Application flow | Synchronous producer and atomic store boundary | Add an explicit-session fact analyzer with exact-run reuse and recorded failure states | Claim extraction, events, subscriptions, and agents |
| SQLite | Embedded additive migrations and transaction pattern | Add analysis and fact persistence without rewriting the original migration | Generic artifacts and queue cutover in Milestone 3 |
| Runtime spine | Existing event, job, worker, and Content Scout-shaped fake proof | Do not call it from the Milestone 1 path | Replace its Content Scout-specific ports in Milestone 3 |
| Tests | Fake boundaries, temporary SQLite, generic integration fixtures | Add Sessions contract, extractor, persistence, resolution, and CLI coverage | Live private-session evaluation is an explicit manual gate |

The seven foundation tables and Content Scout-shaped worker types may remain as
temporary scaffold. Milestone 1 must not broaden its scope by generalizing the
worker or migrating old artifact rows. The new analysis path must use narrow
ports and must not create `scan.completed`, jobs, runs, or content ideas.

## Changes

1. `internal/domain/types.go` and focused files under `internal/domain/` — add
   the use-case-neutral Milestone 1 records and validation rules:

   - `EvidenceRevision` records the Sessions canonical ID, source kind and
     instance, native ID, schema version, trust disposition, digest scheme and
     value, adapter version, source state, freshness, capture/source times,
     lineage coverage, and only other metadata present in the export contract.
   - Extend `EvidenceRef` with the digest scheme, optional segment ordinal,
     entry kind, actor, origin and confidence, content-hash scheme, and direct
     tool linkage supplied by Sessions. Existing scaffold users may leave new
     optional fields absent.
   - `AnalysisRun` records stage `facts`, requested canonical identity, admitted
     revision and full-snapshot selection, coverage and omissions, extractor
     suite and schema versions, ordered fact IDs, status, bounded failure
     details, timestamps, and a stable processing key for completed work.
   - `Fact` is distinct from `Observation` and has a kind, versioned structured
     value, outcome (`success`, `failure`, `unknown`, or not applicable),
     extractor name/version, parse-rule identity, exact evidence, fingerprint,
     and creation time. It has no model confidence, prompt, audience, hook, or
     agent fields. Fact values persist parsed literals and structured summaries,
     never a tool-result body or arbitrary output block. A command, error line,
     URL, path, or other review text is a selected value capped at 2 KiB of raw
     UTF-8 and records its original and emitted byte counts, content hash, and
     truncation state. Facts such as test results store parsed counts, status,
     and exit code rather than the supporting output. Milestone 1 leaves
     `EvidenceRef.Excerpt` empty. One parse rule may retain at most one selected
     text value from a source segment. Across one analysis, retain at most 128
     text-bearing fact values and 64 KiB of emitted raw UTF-8. When either limit
     is reached, keep supported structured values, hashes, and coordinates but
     omit further review text deterministically in canonical evidence and rule
     order. Record omitted text-fact counts and original bytes on the
     `AnalysisRun`.

   Fingerprint canonical data rather than Go map iteration order. A fact ID
   depends on its revision, canonical value, rule/version, and ordered evidence;
   the analysis processing key depends on the revision, full selection, and
   fact-extractor configuration. Neither includes Content Scout or gateway
   configuration.

2. `internal/adapters/sessions/` — implement a read-only `Reader` around an
   injected command runner. Invoke the configured `sessions` executable
   directly, never through a shell, with exactly `export <canonical-id>
   --format jsonl --full`. Treat stderr and transcript content as untrusted;
   return bounded, sanitized operational errors and never execute content.

   Decode the public, closed Sessions structured-output schema 1 record union:
   require one leading `export/session` record, then ordered relation and entry
   records; reject unknown fields, unsupported schema or disposition, wrong
   command or record order, repeated or mismatched identities/digests, invalid
   ordinals, and malformed timestamps or hashes. Mirror only the minimum public
   DTO fields required by this plan; do not import Sessions code. Require
   selection mode `full` with no presentation truncation. Preserve canonical
   omitted segments and unknown lineage as limitations; full means the complete
   export-eligible retained snapshot, not complete provider capture.

   Return a provider-neutral in-memory document containing the admitted
   revision, selection/coverage, ordered entries and segments, and omissions.
   It is transient and is never serialized wholesale into Noema SQLite. Cover
   the adapter with generic JSONL fixtures and a fake command runner, including
   command failure, absent session, unsupported schema, identity/digest drift,
   malformed ordering, truncation, omitted segments, and a valid empty session.

3. `internal/application/` plus a small deterministic extractor implementation
   — add an explicit-session `FactAnalyzer` and narrow source, extractor, and
   store ports rather than extending the old time-range scanner contract.

   The extractor first emits structural facts that Sessions supplies directly:
   tool calls, tool results, their linkage, and available tool identity. Add
   named, versioned exact parsers for command invocations, recognized test
   commands and result summaries, observed exit/outcome markers, and explicit
   error output. A rule may add file, package, or URL facts only when its parser
   can identify them literally. Unsupported or ambiguous text yields no fact or
   an explicit unknown outcome; narrative statements such as “tests should
   pass” never become successful results.

   `FactAnalyzer.Run` reads and validates the export, computes the processing
   key, reuses an exact completed analysis, extracts and validates facts, and
   commits the completed run and new facts atomically. It records a failed
   analysis with the requested identity and any safely known revision metadata
   when source admission, extraction, or validation fails and the separate
   failure-write transaction succeeds. If the completed-analysis transaction or
   the failure write itself cannot commit, return a bounded sanitized persistence
   error without claiming an analysis ID and leave that transaction unchanged.
   A concurrent completed processing-key conflict loads and returns the existing
   analysis as reused rather than recording a failure. Do not retain the complete
   export, create events or jobs, or call the existing `Distiller`. Stored
   failure details are sanitized operational categories capped at 1 KiB; they
   never include transcript text or raw command stderr.

   Add a resolver application boundary that re-reads the selected Sessions
   identity and resolves stored coordinates only after the returned digest
   exactly matches the recorded digest. Return the stable
   `source-revision-unavailable` error on mismatch while leaving prior facts and
   lineage unchanged and inspectable.

4. `internal/adapters/sqlite/migrations/002_fact_analysis.sql` and
   `internal/adapters/sqlite/store.go` — add `analysis_runs` and `facts` as
   additive, idempotent tables. Keep migration `001_initial.sql` and its
   foundation data intact; no destructive migration or data backfill is needed
   for an unreleased scaffold.

   Persist revision, selection, configuration, omissions, structured fact
   values, and evidence references as validated JSON where a separate relation
   would add no Milestone 1 query. Enforce unique completed processing keys and
   fact fingerprints. Provide narrow methods to find a completed analysis,
   commit a run and facts in one transaction, record a bounded failure, and load
   an analysis with its facts in stored order. Prove that reopening an existing
   foundation database applies the additive migration and preserves its tables.
   Add a persistence test showing an oversized source segment cannot enter
   either a structured fact value or evidence excerpt wholesale, and that many
   small matches cannot exceed the per-analysis text count or byte budget.

5. `cmd/noema/main.go`, `cmd/noema/main_test.go`, and `README.md` — replace the
   fail-closed scan placeholder with:

   ```text
   noema scan sessions <canonical-id> [--database <path>]
   noema analyses show <analysis-id> [--resolve] [--database <path>]
   ```

   The scan prints a structured analysis result with reuse status, coverage,
   omissions, fact count, and analysis ID. `analyses show` reads the stored run
   and ordered facts; `--resolve` explicitly re-reads Sessions and includes only
   the referenced segments after the digest check. Resolved text is transient
   presentation capped at 8 KiB per segment with original/emitted byte counts
   and truncation state; it is not written back to SQLite. A recorded failure
   names its analysis ID in the CLI error so it can be inspected. Neither
   command accepts or requires `--allow-remote`. A persistence failure that
   prevents the failure record from committing returns a sanitized error without
   an analysis ID; the CLI must not imply that it can be inspected later.

   Document that the user runs `sessions index` separately, what `--full`
   means, what Noema stores, how revision mismatch behaves, and that the
   Milestone 1 path makes no remote call. Keep `jobs list`, `ideas list`, and the
   fail-closed worker visible as foundation behavior rather than implying that
   Content Scout works.

6. `internal/integration/` — add one generic end-to-end proof using a fake
   Sessions executable and temporary SQLite database: scan one explicit
   canonical ID, inspect literal structural and parsed facts, repeat unchanged
   with no duplicate facts, return a different revision as a distinct analysis,
   and verify that resolving the first analysis against the new digest fails
   closed. Add a malformed-export case that records a bounded failure, returns
   its analysis ID from `scan`, and displays it through `analyses show`. Assert
   the Milestone 1 transactions create no event, job, agent run, or content
   idea. Preserve the existing runtime-spine proof unless a small test-only
   adaptation is required by the extended evidence type.

## Delivery order

Implement the milestone in three independently testable slices:

1. **Contract slice:** domain revision/reference types and the strict Sessions
   reader with generic fixtures. This proves the highest-risk external boundary
   before persistence or extraction grows around it.
2. **Knowledge slice:** fact and analysis types, deterministic rules, SQLite
   migration, and the atomic analyzer. The adapter and SQLite work can proceed
   in parallel only after the domain shapes and processing identity are fixed.
3. **Product slice:** CLI composition, stored inspection, digest-locked
   resolution, README, and the end-to-end acceptance test.

Do not split source parsing, fact identity, and persistence across independent
long-lived plans: their ordering and idempotency rules must be reviewed as one
Milestone 1 contract. If several agents implement the work, integrate after
each slice and keep one owner responsible for the domain and fingerprint rules.

## Verify

- Run focused adapter, extractor, SQLite, application, CLI, and integration
  tests while implementing.
- In the end-to-end test, resolve every fact reference to its exact entry and
  segment in the matching generic export and assert that the fact-only
  transaction adds no downstream runtime records. Also prove that malformed
  source evidence produces an inspectable failed analysis with bounded,
  transcript-free failure details.
- Run `make check`.
- With explicit user approval, optionally run the local command against one
  already-indexed real session and inspect the resulting analysis. This manual
  check reads private evidence locally and is not part of CI or the required
  automated gate.

## Boundaries

- Do not add model or gateway code, privacy classification for remote calls,
  semantic claims, summaries, events, subscriptions, agents, embeddings,
  scheduling, or cloud services.
- Do not read Sessions SQLite, import Sessions code, parse provider histories,
  invoke `sessions index`, persist a full transcript, or silently resolve
  coordinates against a changed document.
- Do not generalize the worker, job payload, agent result, or artifact envelope
  in this milestone; the roadmap assigns that cutover to Milestone 3.
- Stop and revise the plan if the installed Sessions structured-output schema
  differs from schema 1 or cannot provide the digest and coordinates required
  by the accepted roadmap.
