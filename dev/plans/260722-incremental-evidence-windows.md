# Process growing sessions through deterministic evidence windows

- Status: approved; queued after V0 feedback and the knowledge-unit checkpoint
- Roadmap: [Incremental session windows](../../docs/roadmap.md#incremental-session-windows-milestone)

## Goal

Replace manual semantic entry-number discovery for one growing Sessions session
with a deterministic, local, inspectable window plan. A user can preview the
windows without a model call, approve a bounded number of remote executions,
and rerun after the session grows without resending unchanged completed work.

The first slice is intentionally between the roadmap's idea-feedback and
knowledge-unit decisions and multi-session analysis. It starts only after
Content Scout produces useful ideas, idea keep/reject decisions and reasons are
being recorded, and the knowledge-unit checkpoint has either been implemented
because evidence requires it or explicitly recorded as unnecessary. It proves
incremental per-session processing while preserving these constraints:

- Sessions remains the canonical evidence plane and is accessed only through
  its public full-export CLI contract.
- Fact extraction remains complete and revision-bound; it is local and may run
  again when the document digest changes.
- Semantic claims remain revision-bound `AnalysisRun` outputs with the current
  evidence, privacy, admission, event, and exact-reuse rules.
- A reused cross-revision window points to its prior completed semantic analysis;
  it does not rewrite evidence references onto the newer revision.
- No transcript body, summary, model-based range decision, manifest, scheduler,
  daemon, or multi-session abstraction enters this slice.

The milestone is complete when a two-revision generic fixture proves that
stable earlier windows cause no additional generator call, an appended or
rewritten window causes only the required new call, a changed semantic
configuration reruns an otherwise unchanged window, and every plan, reuse,
failure, bound, and coverage result is inspectable.

## Changes

1. `internal/domain/analysis.go` and a focused
   `internal/domain/evidence_window.go` — add the `windows` analysis stage and
   small versioned values for a window plan and its ordered items. Each item
   records an ID, cross-revision fingerprint, inclusive entry bounds, entry and
   byte counts, boundary reason (`verified`, `capacity`, or
   `end-of-revision`), provisional state, coverage, and deterministic reason
   codes. The plan records its input fact-analysis ID and optional prior plan
   ID. Keep semantic execution links separate from the window value so planning
   remains local and immutable.

   Derive the plan processing key from the exact evidence revision, complete
   fact-analysis processing identity, planner name/version/schema, semantic
   preflight and limit-policy version, reviewed route digest used for sizing,
   and ordered window descriptors. Derive each window fingerprint from the canonical source
   identity, planner identity, inclusive bounds, and ordered entry signatures:
   entry kind, actor, timestamp, relation and tool metadata, segment kind and
   origin metadata, content class, source type, content hash, and byte counts.
   These are the revision-independent source fields currently eligible for the
   semantic model input. Exclude the whole-document digest and raw text. This
   makes an unchanged prefix recognizable in a newer revision without claiming
   that its old evidence coordinates belong to the new revision.

2. `internal/application/semantic_analysis.go` and
   `internal/application/evidence_window.go` — expose or reuse one read-only,
   versioned semantic preflight that performs every local transformation and
   limit check before `SemanticGenerator.Generate`: source and route validation,
   `BuildSemanticInput`, deterministic privacy filtering and post-filter bounds,
   output schema and prompt assembly, and complete generation-request envelope
   sizing. It returns only the safe selection, size, privacy-category counts,
   contract versions, and success or fixed failure category needed by planning;
   it must not expose or persist filtered evidence text, instructions, schema
   JSON, or the outbound request. Preflight needs the reviewed route but no API
   key, remote approval, adapter, or network request.

   Implement one deterministic
   `EvidenceWindowPlanner` over a completed fact analysis and its matching
   digest-locked `EvidenceDocument`. Start at the first human entry and omit
   leading source/system material unless relation closure requires it. Grow a
   window across consecutive entries and human follow-ups. Close only at a
   boundary where no tool relation crosses the cut, preferring the first such
   boundary after a supported successful `test-result`; otherwise close before
   adding the next safe block that would exceed the existing semantic input
   limits. Close the final window at end of revision and mark it provisional.

   Use the shared complete semantic preflight while growing a window rather than
   copying its constants. The same route and preflight contract therefore size
   preview and execution, including privacy substitutions and the complete
   prompt, schema, and request envelope. Close the previous safe block when
   adding another produces a size failure. A single related block that still
   fails the complete size preflight becomes an inspectable
   `manual-range-required` window; a privacy-blocked block becomes
   `privacy-blocked`; neither is sent by the automatic executor. Invalid route,
   schema, or preparation contracts fail the plan rather than becoming
   per-window results. A document without a human entry produces a valid empty
   plan. Add application tests for verified, capacity, provisional,
   relation-closure, privacy-blocked, complete-request over-limit, and empty
   plans.

3. `internal/application/window_workflow.go` — add a local planning workflow
   that loads one completed fact analysis, re-exports and digest-validates its
   exact Sessions revision, computes the plan, and stores or reuses the exact
   completed window-plan analysis. Compare the ordered items with the latest
   prior completed plan for the same canonical session and record content
   dispositions `new`, `changed`, or `unchanged`; removed prior windows remain
   in their immutable old plan and create no work.

   Add a separate execution workflow that accepts a stored plan, the reviewed
   semantic route, explicit remote approval, and a required positive maximum
   window count. Before any request, re-read the plan's exact revision and fail
   with `source-revision-unavailable` if the session changed after preview, and
   reject a route digest that differs from the local preview. Re-run the shared
   complete semantic preflight before dispatch so changed preparation code or
   limits fail before generation instead of using a stale plan. Run eligible
   windows sequentially through the existing `SemanticWorkflow` and existing
   `EntryBounds`; do not copy generation, privacy, admission, event, or
   persistence logic.

   A completed semantic analysis is reusable across revisions only through a
   window-execution key containing the window fingerprint, Sessions adapter
   version, fact extractor identity, semantic input schema/builder and
   limit-policy version, semantic extractor and prompt identity, candidate
   schema digest, privacy policy, and sanitized route digest. This key must
   cover every revision-independent field or configuration that can affect
   `BuildSemanticInput`; adding such an input requires changing the applicable
   fingerprint or version. Reuse returns the prior semantic analysis ID and
   emits no event. A new execution retains the existing revision-bound semantic
   processing key and normal
   `analysis.completed` event. Stop on the first failed window; prior completed
   links remain committed so an exact rerun resumes at the failed item.
   The required maximum counts only new semantic attempts that may invoke the
   generator. Walk items in canonical order, skip reusable and deferred items
   without consuming the limit, and stop when the attempt limit is exhausted or
   the first attempted window fails.

4. `internal/adapters/sqlite/migrations/004_evidence_windows.sql` and focused
   window-store files — persist window-plan details and ordered window items as
   a projection beside `analysis_runs`, plus append-only links from a plan item
   and window-execution key to its completed semantic analysis. Use the existing
   transaction, deterministic-ID, and load-time validation patterns. Index the
   canonical source plus plan completion order for prior-plan lookup and the
   window-execution key for reuse. Do not store entry or segment text, outbound
   model input, generated candidates, or copied claims in these tables.

   Persistence tests must prove exact plan reuse, ordered round trips, rejection
   of broken bounds/fingerprints/analysis links, cross-revision lookup only for
   the same canonical source, configuration-sensitive execution reuse, and
   atomic visibility of each completed link. Existing fact and semantic tables
   and IDs remain unchanged.

5. `cmd/noema/` — add explicit commands without changing the manual
   `analyze claims` path:

   ```text
   noema windows plan <fact-analysis-id> --route-config <path> [--database <path>]
   noema analyses show <window-plan-id> [--database <path>]
   noema analyze windows <window-plan-id> --allow-remote \
     --route-config <path> --max-windows <n> [--database <path>]
   ```

   Planning loads the route only for local preflight; it requires no API key or
   remote approval and constructs no Gateway adapter. It prints the plan ID,
   reuse flag, revision, sanitized route digest, prior plan when present, and
   ordered bounds, boundary reasons, provisional flags, coverage, and
   `new`/`changed`/`unchanged` dispositions. Execution prints processed, reused,
   failed, deferred, and remaining counts plus the ordered semantic analysis
   IDs. It must require `--allow-remote`, the existing API-key and reviewed-route
   checks, and an explicit positive `--max-windows`; `manual-range-required`
   and `privacy-blocked` items remain deferred. The execution route digest must
   match the plan. The flag limits new semantic attempts that may invoke the
   generator; reused and deferred items do not count. Extend analysis
   inspection for the `windows` stage rather than adding a second show command.

6. `cmd/noema/` integration tests with generic fake Sessions revisions and an
   injected counting semantic generator — prove the complete public behavior:
   the first revision plans and executes ordered windows; the second revision
   preserves a verified closed window, changes the prior provisional window,
   and appends a new window; only changed/new windows invoke the generator; an
   exact rerun invokes it zero times; a changed prompt or route reruns the
   otherwise unchanged windows; a source change between preview and execution
   makes zero remote calls; `--max-windows` counts only attempted generation,
   skips reused and deferred items for free, and leaves bounded inspectable work;
   an automatically eligible boundary passes privacy-filtered and full-request
   preflight while an indivisible oversized or privacy-blocked block is deferred;
   a changed preview/execution route digest makes zero remote calls;
   changing content class, source type, semantic-input builder, or limit-policy
   versions prevents reuse while document-digest-only changes do not; and a
   mid-batch failure resumes without duplicating completed claims or events.

7. `README.md`, `docs/contributing/testing.md`, and the existing architecture
   and roadmap sections — document the two-step manual workflow, its cost and
   privacy controls, provisional-window behavior, the distinction between
   content reuse and evidence rebinding, and the routine fake-source test seam.
   Do not describe this as ambient ingestion or multi-session support.

## Verify

- `go test ./internal/application ./internal/adapters/sqlite ./cmd/noema`
- `make check`

## Boundaries

- Do not implement this plan before the V0 Content Scout gate shows useful
  one-session outputs, idea decisions and reasons are being recorded, and the
  knowledge-unit checkpoint has been implemented or explicitly closed as
  unnecessary.
- Do not add `sessions manifest`, an `EvidenceSet`, time-range discovery,
  project inference, scheduling, retries, leases, a background process, or a
  remote workflow engine.
- Do not use a model, embeddings, full-text search, or private-content keyword
  scoring to choose windows.
- Do not aggregate several window analyses into a summary, knowledge unit,
  episode, or session-level agent event in this slice. Existing per-analysis
  events and downstream deduplication behavior remain explicit.
- Do not mutate, supersede, or re-anchor prior claims. A prior Sessions revision
  becoming unavailable remains an honest lineage limitation rather than a
  reason to apply old coordinates to a newer document.
