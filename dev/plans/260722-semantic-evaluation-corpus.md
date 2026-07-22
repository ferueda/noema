# Establish a reviewed semantic-claim evaluation corpus

- Status: approved

## Goal

Create a repeatable, explicitly invoked evaluation of Noema's real semantic
prompt, schema, privacy filter, Gateway adapter, and local claim admission over
12 generic coding-session cases. It should show whether a prompt/model route
produces valid batches, respects evidence and outcome rules, yields useful
claims, and stays within acceptable cost and latency. It is the reviewed generic
fixture evidence required by the Milestone 2 gate.

Machine checks can measure generation/admission success, empty results, safe
failure categories, evidence/outcome admission failures, tokens, cost, and
latency. Whether an admitted natural-language claim is actually supported and
useful remains a human judgment captured in a separate review sidecar. The
first run establishes a baseline; this plan does not invent an automatic release
threshold.

Evaluation is a development instrument, not a Noema analysis stage. Reports and
reviews remain explicit local files outside the repository by default. They do
not enter SQLite, events, artifacts, or source evidence.

## Changes

1. `dev/evaluations/semantic-claims/corpus-v1.json` and
   `dev/evaluations/semantic-claims/README.md` — add one versioned corpus and
   review rubric with 12 synthetic, non-private cases:

   1. insufficient evidence where empty output is preferred;
   2. an observed problem without a supported solution;
   3. a proposed fix without verification;
   4. a failed attempt backed by a failed command result;
   5. a fix backed by a passing test;
   6. an assistant success assertion contradicted by tool failure;
   7. linked success and failure results;
   8. a hypothesis without an established root cause;
   9. a decision with a recorded rejected alternative;
   10. a tool or environment failure that must not become user blame;
   11. a reusable lesson supported by several observed steps; and
   12. noisy unrelated entries where no claim is preferable.

   Each case contains a stable ID, short intent, minimal canonical-style entries
   and deterministic facts. Divide expectations into machine-checkable rules,
   such as `must-be-empty`, and bounded human case criteria, such as
   `no-unsupported-solution`, `tool-result-precedence`, or `no-user-blame`.
   Do not require exact generated prose or require a non-empty result merely to
   fill a quota. Keep repository fixtures free of private paths, repositories,
   people, credentials, and transcript text.

2. `cmd/noema-semantic-eval/corpus.go` — define this corpus's narrow versioned
   DTO and build stable `domain.EvidenceDocument` and `domain.FactAnalysis`
   values for `application.SemanticAnalyzer`. Validate the entire corpus before
   the first remote call: schema version, unique bounded case IDs, entry/segment
   order and relations, fact outcomes and coordinates, expectation
   combinations, privacy-policy compatibility, and fixed case/input size
   limits. Keep the expected digest of the finalized committed corpus in this
   evaluation command and require the loaded content to match it before adapter
   construction. The path may identify an exact copy elsewhere, but any changed
   content fails closed before a model call. Do not create a reusable fixture or
   evaluation framework.

3. `internal/application/semantic_analysis.go` — add one read-only preflight
   method that validates and prepares a `SemanticAnalysisRequest` through the
   exact production route, input-building, privacy-filtering, schema, and size
   rules without invoking a generator or exposing private preparation state.
   The evaluation runner must preflight all 12 built cases through this seam
   before constructing the Gateway adapter or making any remote request; each
   actual run still goes once through `SemanticAnalyzer.Run`, which remains the
   authority for generation and admission.

4. `cmd/noema-semantic-eval/run.go` — add a developer-only command:

   ```sh
   go run ./cmd/noema-semantic-eval run \
     --corpus ./dev/evaluations/semantic-claims/corpus-v1.json \
     --allow-remote \
     --route-config ./config/semantic-route.example.json \
     --output /tmp/noema-semantic-eval.json \
     --review-output /tmp/noema-semantic-review.json
   ```

   Require the same explicit approval, `AI_GATEWAY_API_KEY`, and strict route
   loader as production. Run cases sequentially and exactly once through the
   existing `SemanticAnalyzer` and `aigateway.Generator`; do not copy the
   production prompt, schema, privacy, response decoding, or admission rules.
   A small evaluation-only generator wrapper may retain decoded candidate count
   and model metadata for synthetic cases when admission later fails, but the
   report must not include raw provider bodies, hidden reasoning, credentials,
   or rejected provider messages. Stop after the first generation/protocol
   failure that is safely categorized as authentication, permission, rate
   limit, rejected request, rejected schema, upstream unavailable, transport
   failure, or unknown generation failure. Record and continue after context-
   too-large, content-rejected, timeout, invalid-response, privacy postflight,
   and local admission failures because those may be case-specific evaluation
   evidence. Every stopped run is incomplete; a complete baseline requires a
   result for all 12 cases.

5. `internal/application/semantic_workflow.go:semanticAdmissionFailureCategory`
   — expose the existing allowlisted admission category mapper and make both the
   production workflow and evaluation runner use it. If the conformance plan has
   not already exposed the generation mapper, expose that application-owned
   mapper in the same way. Keep rejected candidate prose inside the application
   boundary; the evaluation command must not duplicate error-string matching.

6. `cmd/noema-semantic-eval/report.go` — write a bounded, versioned JSON report
   only to the explicit output path. Include corpus digest, case order, prompt,
   schema, extractor, privacy, and sanitized route identities; requested and
   resolved provider/model; pinned generation controls; timestamps; per-case
   completion or sanitized failure stage; admitted claims and their cited
   synthetic evidence; candidate/admitted counts; usage, latency, exact decimal
   cost, and request identity when returned. Aggregate:

   - valid-batch rate, counting admitted empty batches as valid;
   - empty-batch and failure counts by safe category, including evidence and
     outcome admission failures;
   - pass/fail results for every machine-checkable case expectation, with an
     explicit expectation-coverage count;
   - total/average cost and cost-metadata coverage using exact decimal math;
   - mean, p50, and p95 latency; and
   - token totals and metadata coverage.

   An interrupted or fail-fast run remains explicitly incomplete and cannot be
   mistaken for a complete corpus result. The `run` command also writes the
   required review sidecar template to `--review-output` after the report. It
   binds the template to that report's corpus digest and lists every admitted
   claim fingerprint and every human case criterion with explicit `unreviewed`
   values; it never invents a review decision.

7. `cmd/noema-semantic-eval/score.go` — add the explicit offline command:

   ```sh
   go run ./cmd/noema-semantic-eval score \
     --report /tmp/noema-semantic-eval.json \
     --reviews /tmp/noema-semantic-review.json \
     --output /tmp/noema-semantic-score.json
   ```

   Accept the immutable run report and the edited
   separate JSON review sidecar keyed by corpus digest and admitted claim
   fingerprint. Allow evidence-support labels `supported`, `partly-supported`,
   or `unsupported`, and usefulness labels `useful`, `weak`, or `not-useful`,
   with an optional bounded note. Also require each human case criterion to be
   keyed by case and criterion ID and labeled `pass`, `partial`, or `fail` with
   an optional bounded note. Reject stale corpus identities, unknown claim
   fingerprints, case IDs, or criterion IDs; report missing decisions as
   unreviewed. Produce evidence support, usefulness, useful-case, claim-review
   coverage, case-expectation results, and expectation-review coverage without
   using a model judge or treating unreviewed items as failures.

8. `cmd/noema-semantic-eval/*_test.go` — keep routine tests offline with an
   injected fake generator. Cover corpus validation before generation, admitted
   and valid-empty batches, sanitized generation and admission failures,
   the exact global stop and case-level continue categories, exact decimal cost
   aggregation, latency/token metadata coverage, incomplete runs, and
   stale/partial human reviews. Prove every case passes the application-owned
   preflight before the first generator call. Prove a
   structurally valid corpus with the wrong digest makes no generator call and
   that evaluation reports use the same safe evidence and outcome categories as
   the production workflow. Prove every machine expectation receives a result,
   every human criterion appears in the review template, and missing case
   verdicts reduce coverage rather than silently passing. Prove `make check`
   never needs credentials or invokes the live corpus.

9. `docs/contributing/testing.md`, `docs/roadmap.md`, and the active Milestone 2
   plan — document the explicit run/review sequence, separate automatic and
   human measures, and record the reviewed baseline when the live corpus is
   actually run. Do not mark Milestone 2's reviewed-fixture gate complete merely
   because the harness code exists.

## Verify

- Run `go test ./cmd/noema-semantic-eval` with fake generators.
- Run `make check`; it must remain offline and credential-free.
- After pinned sampling and the live conformance check pass, obtain explicit
  approval for one live 12-case run, review every admitted claim, score the
  sidecar, and record the baseline summary without committing transient model
  output or private data.

## Boundaries

- Do not use Sessions, private sessions, SQLite, events, product analysis runs,
  model-as-judge scoring, repeated sampling, concurrency, a provider/model
  matrix, prompt optimization, automatic thresholds, a second verifier, or the
  later generic Agent Eval Lab.
- Do not preserve raw candidates from private or production inputs. The tool is
  limited by exact content digest to its committed synthetic corpus and refuses
  arbitrary or modified evidence files before adapter construction.
