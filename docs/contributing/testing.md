# Testing

## Purpose

Tests let humans and agents change Noema while keeping evidence authority,
lineage, privacy, idempotency, event delivery, and typed artifacts visible. Use
the cheapest stable layer that proves the behavior.

## Principles

- Test public behavior and durable contracts rather than private call order.
- Keep routine tests deterministic, offline, and isolated from user state.
- Use generic Sessions JSON or JSONL fixtures. Never copy private transcripts,
  repository names, local paths, credentials, or personal identifiers into the
  repository.
- Assert forbidden side effects as well as expected results when testing
  privacy, source immutability, authority, or idempotency.
- Add broader integration coverage only when a cheaper layer cannot observe the
  boundary under test.
- Add regression coverage for bugs when it protects important repeatable
  behavior.

## Layers

| Layer | Use for | Location |
| --- | --- | --- |
| Domain and parser unit | Values, validation, fingerprints, literal fact extraction, and claim admission rules | Beside the owning Go package as `*_test.go` |
| Application | Analysis stages, event creation, subscription matching, workers, and failure behavior | `internal/application/` |
| Persistence | Migrations, transactions, idempotency, ordering, and artifact lineage | `internal/adapters/sqlite/` with temporary databases |
| Source contract | Versioned Sessions JSON/JSONL parsing, digest mismatch, bounds, omissions, and unsupported schemas | Adapter tests with generic fixtures and fake command execution |
| CLI and integration | Argument handling, structured output, separate-process roles, and SQLite-only handoff | `cmd/noema/` and `internal/integration/` |
| Live external | Explicit proof against a real Sessions or model provider boundary | Separate manual command added only when required |

Prefer an existing layer and directory before inventing a new test structure.
One acceptance criterion should not be repeated at every layer unless each test
protects a distinct failure mode.

## Fixture and state safety

- Create SQLite databases and filesystem state under a test-owned temporary
  directory.
- Use fake clocks, deterministic identifiers, and fake provider or process
  boundaries where behavior depends on them.
- Never invoke `sessions index`, open the Sessions SQLite database, or mutate
  provider histories from a routine test.
- Do not require network access, model credentials, or a populated user data
  directory for `make test` or `make check`.
- Clean temporary state on success. A diagnostic test may retain a bounded path
  on failure only when it reports that path clearly and contains no private data.

## Verification commands

During iteration, run the narrowest useful Go test, for example:

```sh
go test ./internal/application
go test ./internal/adapters/sqlite
go test ./internal/integration
```

- `make test` runs the fast Go suite without the race detector.
- `make check` is the final local gate and runs all tests with the race detector.
- Live checks are never implied by either command.

Before handoff, report tests added or changed, `make check`, and any explicit
checks skipped with the reason.

## Live semantic-route check

The explicit live canary is:

```sh
go run ./cmd/noema gateway check --allow-remote \
  --route-config ./config/semantic-route.example.json
```

It requires fresh authority, `AI_GATEWAY_API_KEY`, network access, and awareness
of the small model cost. It uses only fixed public synthetic input and makes no
Sessions or SQLite call. Run it manually when the production semantic route,
schema, prompt, SDK, or provider contract needs live confirmation.

Routine tests inject a fake generator and prove the command's approval gates,
fixed empty input, production contract reuse, safe output, and sanitized
failures. `make check` and CI must never invoke the live command or require its
credential.

## Live semantic evaluation

The semantic evaluation command is a development tool, not a Noema analysis
stage. It accepts only the reviewed V1 or V2 synthetic corpus at its compiled
content digest. V1 is the immutable 12-case baseline; V2 preserves V1 and adds
eight meaning-focused cases. It does not use Sessions, SQLite, events, or
private evidence.

After obtaining fresh approval for every request in the selected corpus, run,
for example, the 20-case V2 corpus:

```sh
go run ./cmd/noema-semantic-eval run \
  --corpus ./dev/evaluations/semantic-claims/corpus-v2.json \
  --allow-remote \
  --route-config ./config/semantic-route.example.json \
  --output /tmp/noema-semantic-eval.json \
  --review-output /tmp/noema-semantic-review.json
```

The command preflights all cases through the production semantic path before
constructing the Gateway adapter. It then runs the cases once, in order, and
writes an immutable machine report plus a separate review template. The report
measures valid and empty batches, structural expectations, safe failure
categories, token use, exact cost, and latency. Those checks cannot establish
whether generated prose is truly supported or useful.

Review every claim and case criterion using the rubric in
[`dev/evaluations/semantic-claims/README.md`](../../dev/evaluations/semantic-claims/README.md),
then score the edited sidecar offline:

```sh
go run ./cmd/noema-semantic-eval score \
  --report /tmp/noema-semantic-eval.json \
  --reviews /tmp/noema-semantic-review.json \
  --output /tmp/noema-semantic-score.json
```

The score reports review coverage separately from decisions. Missing reviews
remain `unreviewed`; they never become implicit failures or passes. Do not
commit transient reports, reviews, or scores. `make check` exercises this code
with fake generators and never runs the live corpus.
