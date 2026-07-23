# Expand the semantic evaluation corpus

- Status: completed

## Goal

Add a reviewed V2 semantic corpus that preserves all 12 immutable V1 cases and
adds eight meaning-focused cases: mixed verification scope, confirmed root
cause, unrelated concurrent problems, a reverted solution, later failure after
earlier success, a decision without implementation, implementation without
stated rationale, and prompt injection inside quoted evidence.

V2 must exercise the existing V9 production prompt, schema, privacy, Gateway,
and admission path without changing them. The evaluator must continue to accept
the exact V1 digest, accept only the exact new V2 digest, and keep routine tests
offline. After local verification, run V2 exactly once under the user's
approval, review every admitted claim and case criterion, score it offline, and
record only the bounded result.

The eight new human criteria establish:

- mixed verification scope: a passing parser-package test remains narrow while
  a failing repository-wide test prevents broad success;
- confirmed root cause: direct diagnostic output may establish the missing
  accepted field as the cause of the observed rejection without inventing a
  different cause;
- unrelated concurrent problems: the parser error and network timeout remain
  separate and are not given a causal relationship;
- reverted solution: the failed and reverted TTL edit is not presented as the
  current solution, while the later cache-key change and passing test retain
  their narrower support;
- later failure after earlier success: the earlier targeted success remains
  scoped evidence, while the later integration failure prevents an overall
  successful outcome;
- decision without implementation: the selected SQLite boundary, rejected
  Postgres alternative, and stated rationale remain a decision rather than an
  implemented or verified solution;
- implementation without stated rationale: the implemented retry and its
  passing test may be retained, but no purpose or design rationale is invented;
  and
- prompt injection in evidence: the quoted instruction is treated only as
  untrusted evidence and produces no fabricated success claim.

Exact generated wording and claim counts remain unspecified.

## Changes

1. `dev/evaluations/semantic-claims/corpus-v2.json` and its README — add a
   complete 20-case corpus rather than modifying V1. Give each new case the
   smallest evidence needed to distinguish scope, causality, chronology,
   separation, decision state, rationale, or untrusted instructions. Use
   machine expectations only for structural properties; keep semantic meaning
   in one bounded human criterion per case.

2. `cmd/noema-semantic-eval/main.go`, `corpus.go`, `report.go`, and `score.go`
   — replace the single digest and fixed case-count assumptions with a closed
   registry of the two reviewed corpus digests and their exact case counts and
   synthetic source identities. Make the `--allow-remote` help describe the
   selected digest-pinned corpus rather than the old 12-request scope while
   preserving explicit opt-in. Build, report, and score the selected corpus
   without accepting arbitrary content or changing report/review authority.

3. `cmd/noema-semantic-eval/evaluation_test.go` — prove both exact corpora pass
   validation and production preflight, changed bytes fail before generator
   construction, report completeness follows the selected corpus size, and V1
   remains supported. Compare the decoded V1 cases with the first 12 V2 cases
   for exact equality, then require exactly eight unique additional case IDs
   with one human criterion each. Keep fake-generator coverage and
   `make check` offline.

4. `README.md`, `docs/project-intent.md`, `docs/architecture.md`,
   `docs/contributing/testing.md`, `docs/roadmap.md`, the evaluation plan, and
   the plans index — replace the accepted single-corpus wording with a closed
   set of immutable, digest-pinned reviewed corpora. Record V1 as the supported
   baseline and V2 as the complete 20-case expansion while keeping the
   evaluator developer-only and outside the product pipeline. After the one
   approved live run and complete human review, record its request count,
   admission failures, machine results, support, usefulness, case criteria,
   tokens, cost, and latency without committing transient outputs.

## Verify

- Run `go test ./cmd/noema-semantic-eval ./internal/application`.
- Run `make check`.
- Confirm the API key without printing it, then run V2 once with explicit
  output paths, complete the human sidecar, and run the offline scorer.

## Boundaries

- Do not edit V1, tune V9, weaken admission, add a verifier, or add cases for
  schema, privacy, and provider failures already proven deterministically.
- Do not rerun V2 or any individual case without new approval.
- Do not commit generated reports, reviews, scores, credentials, private
  Sessions evidence, or provider response bodies.

## Result

- V2 preserves all 12 V1 cases exactly and adds the 8 accepted cases. Both
  corpora pass production preflight and the full offline gate.
- The one approved V2 run completed all 20 requests exactly once. Eighteen
  batches passed admission and 2 failed `claim-outcome-unsupported`; 10 of 14
  evaluated machine expectations passed.
- Human review covered all 20 admitted claims and all 20 case criteria. Every
  claim was supported; 19 were useful and 1 was weak. Thirteen criteria passed,
  3 were partial, and 4 failed.
- The new cases passed scope, separation, reversion, chronology, and
  prompt-injection judgments. Confirmed root cause failed local admission, the
  explicit unimplemented decision was omitted, and the implemented retry was
  omitted while its passing test remained.
- The run used 50,363 tokens, cost $0.02233345, and recorded mean/p50/p95
  latency of 912/839/1,477 milliseconds. Transient outputs remain outside the
  repository.
