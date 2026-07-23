# Pin semantic generation sampling

- Status: implemented; live acceptance remains part of the conformance plan

## Goal

Make the existing `semantic-v1` route explicitly request temperature zero so
semantic extraction relies less on provider defaults and produces more focused,
repeatable candidates. Temperature zero reduces sampling variation; it does not
guarantee identical output.

The current Cerebras Chat Completions contract accepts temperature values from
zero through two, and its published `gpt-oss-120b` capabilities include
temperature. The official OpenAI Go client exposes the same top-level request
field. Seed remains out of this change: Cerebras describes it as best effort,
and support through the pinned Vercel Gateway route has not yet been established
by Noema's conformance check.

Adding temperature changes the canonical sanitized route and its digest, so the
existing semantic processing key already treats the first run as new work and
reuses exact later runs. It needs no domain field, persistence migration, CLI
override, or generic generation-parameter model.

References:

- <https://inference-docs.cerebras.ai/api-reference/chat-completions>
- <https://inference-docs.cerebras.ai/api-reference/models/public-models>
- <https://vercel.com/docs/ai-gateway/models-and-providers>

## Changes

1. `internal/adapters/aigateway/route.go:routeProfile` and `acceptedProfile` —
   add an explicit pointer-backed `temperature` value and accept only numeric
   zero in the reviewed `semantic-v1` profile. A pointer keeps omitted/null
   distinct from explicit zero. Keep the current route alias and version: this
   completes the active V0 profile, while `buildValidatedRoute` already places
   the field in the sanitized configuration and configuration digest.

2. `internal/adapters/aigateway/generator.go:Generator.Generate` — set the
   route's temperature on `openai.ChatCompletionNewParams`. Do not set seed,
   `top_p`, penalties, reasoning controls, or accept arbitrary parameter maps.

3. `internal/adapters/aigateway/route_test.go` — accept explicit zero and reject
   missing, null, nonnumeric, or nonzero temperature before adapter
   construction. Preserve the existing proof that equivalent file formatting
   produces the same canonical route identity.

4. `internal/adapters/aigateway/generator_test.go` — assert the intercepted wire
   request contains numeric `"temperature": 0` and no seed. Update the strict
   route fixtures in `cmd/noema/remote_semantic_test.go` and
   `config/semantic-route.example.json`. Existing processing-identity tests
   already prove a changed sanitized route digest creates a different semantic
   processing key without changing the evidence input digest.

5. `README.md`, the model-gateway section of `docs/architecture.md`, and
   `dev/plans/260721-milestone-2-semantic-claims.md` — record the pinned sampling
   choice, its inclusion in route identity, the copied-route compatibility
   impact, and the fact that temperature zero is not a determinism guarantee.

## Verify

- Run `go test ./internal/adapters/aigateway ./cmd/noema`.
- After the live conformance command exists, run it once with explicit approval
  to prove Vercel and Cerebras accept numeric temperature zero on the pinned
  route.
- Run `make check`.

## Boundaries

- Previously copied route files without `temperature` must fail closed until
  the explicit field is added; existing stored analyses remain inspectable.
- Do not add seed until the pinned Gateway path advertises or proves it. Do not
  add a generic generation-parameter system, route matrix, model change,
  retries, or automatic repair.
