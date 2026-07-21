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
