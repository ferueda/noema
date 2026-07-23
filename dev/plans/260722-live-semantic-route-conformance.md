# Add a live semantic-route conformance check

- Status: completed
- Live validation: 2026-07-23 — Cerebras resolved `openai/gpt-oss-120b`,
  accepted the production strict schema and temperature-zero route, returned
  zero candidates, and created no repository or database state
- Live metadata: 980 total tokens, 1,477 ms latency, $0.0003754

## Goal

Add one explicit, low-cost command that checks the current Vercel AI Gateway
and Cerebras route with Noema's real Go adapter and production semantic output
schema before private evidence is involved:

```sh
go run ./cmd/noema gateway check \
  --allow-remote \
  --route-config ./config/semantic-route.example.json
```

The check must prove authentication, pinned routing, SDK request encoding,
provider acceptance of the current strict schema, response metadata location,
resolved provider/model validation, and empty-envelope decoding. It uses only
fixed public synthetic input with no evidence from which a claim can be formed,
creates no Noema records, and reports only bounded model metadata or an existing
sanitized failure category. Success requires exactly an empty `claims` array;
the check does not claim to validate semantic support for non-empty output.

The command is a manual operational check. It is never part of `make check`,
CI, or ordinary development setup. Running it requires explicit authority,
`AI_GATEWAY_API_KEY`, the reviewed route file, network access, and awareness
that it incurs a small model cost.

## Changes

1. `internal/application/semantic_conformance.go` — add a narrow conformance
   operation that builds a fixed public `SemanticModelInput`, uses the exact
   `semanticInstructions` and `semanticClaimOutputSchema()` owned by the
   production semantic path, calls the injected `SemanticGenerator`, and
   validates resolved model metadata through the existing application rule.
   Require the decoded candidate list to be empty because the fixed input
   contains no evidence; treat any candidate as the existing sanitized invalid-
   response failure without exposing its fields. This prevents a merely
   decodable, invalid candidate from making the canary pass without adding a
   general JSON Schema validator. Semantic support and non-empty claim admission
   belong to the evaluation plan. Return schema identity, a zero candidate
   count, and bounded `ModelExecutionMetadata`, never candidate prose. Before
   applying `validateSemanticModelExecution`, populate `RequestedRoute` from the
   validated route and `PromptVersion` from `SemanticPromptVersion`, exactly as
   `SemanticAnalyzer.generatePrepared` does. Convert generator and metadata
   failures to the existing private allowlisted generation categories inside
   the application operation.

2. `cmd/noema/gateway.go`, `cmd/noema/analyze.go:commandDependencies`, and
   `cmd/noema/main.go:runWithDependencies` — add `gateway check` while reusing
   the existing generator factory and `aigateway.LoadRoute`. Reject the command
   before adapter construction unless `--allow-remote`, a non-empty
   `AI_GATEWAY_API_KEY`, and `--route-config` are present. Do not open SQLite,
   invoke Sessions, accept model/provider overrides, or write a file. Emit JSON
   containing the success flag, schema identity, route digest, sanitized route
   configuration, resolved provider/model, request identity, usage, latency,
   cost, and candidate count. On failure, return only the sanitized category.

3. `internal/application/semantic_conformance_test.go` and
   `cmd/noema/gateway_test.go` — prove the request reuses the current production
   prompt and schema identity, contains only fixed generic input, and exposes no
   candidate text. Prove an empty decoded envelope succeeds and a decodable
   non-empty candidate fails with the sanitized invalid-response category. At
   the application seam, prove mismatched resolved provider or model metadata
   also fails with that safe category after requested route and prompt metadata
   are attached. At
   the CLI seam, use the existing injected generator pattern to prove
   approval/key/route gates run before construction, success contains no key or
   prompt, and a categorized provider failure exposes no supplied private
   detail. Existing Gateway wire tests remain responsible for exact provider
   options, numeric schema encoding, and nested response metadata.

4. `README.md`, `docs/contributing/development.md`, and
   `docs/contributing/testing.md` — document the exact manual command, what it
   proves, its credential/network/cost requirements, and that it does not test
   semantic quality or provider privacy guarantees. Add a short operational
   note to `docs/architecture.md` without presenting conformance as a knowledge
   stage.

## Verify

- Run `go test ./internal/application ./internal/adapters/aigateway ./cmd/noema`.
- Run `make check`; it must not execute the live command or require credentials.
- With fresh explicit user approval, run the command once and verify the output
  identifies `cerebras`, `openai/gpt-oss-120b`, and the current schema while
  creating no database or tracked file.

## Boundaries

- Do not use a curl script, toy schema, or second provider client; those would
  not detect SDK wire or production-schema regressions.
- Do not load a real session, admit claims, judge claim text, persist a run, add
  fallback or retry behavior, create a provider-probe framework, or put network
  work in CI.
