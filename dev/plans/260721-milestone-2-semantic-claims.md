# Admit evidence-backed semantic claims

- Status: completed
- Implemented: admission slice in PR #11 (`56981fc`)
- Implemented: durability slice in PR #13 (`265938e`)
- Implemented: [behavior-preserving durability cleanup](260721-semantic-durability-cleanup.md)
- Implemented: remote Gateway adapter, explicit CLI path, and offline acceptance
- Live finding: the first approved request reached Vercel but Hobby-plan ZDR
  enforcement returned 403 before generation
- Live finding: after disabling that request, Cerebras rejected JSON Schema
  array-size keywords that its strict structured-output subset does not support
- Live finding: the OpenAI client encoded `json.Number` schema bounds as JSON
  strings; wire-level regression coverage now requires numeric bounds to remain
  numbers
- Live finding: current Chat Completions responses place Gateway routing and
  generation metadata on the assistant message rather than the top-level
  completion; the fake Gateway and decoder now follow the live response shape
- Live finding: the first meaningful candidate used actor language rejected by
  neutral claim admission; prompt v2 now gives an impersonal example and an
  explicit free-text rule without weakening local validation
- Accepted follow-up: failed local admission records fixed, specific evidence,
  fact-reference, attribution, provenance, duplicate, value, and outcome
  categories without retaining rejected model prose
- Live finding: prompt v2 then asserted structured provenance that did not match
  all support; prompt v3 requires null actor/origin unless every cited record
  shares the exact value
- Accepted follow-up: prompt guidance remained unreliable, so the V0 output
  schema requires null actor/origin while the domain keeps the richer fields
  for later extractors that can establish them
- Live finding: prompt v5 uses a positive grammar that makes technical
  artifacts or observed behavior the subject of every claim instead of relying
  only on a forbidden-actor list
- Live finding: the first v5 batch assigned an outcome to a claim type that
  cannot carry one; prompt v6 now states the local outcome rules directly
- Live finding: prompt v6 marked an outcome observed without a matching
  deterministic result fact; prompt v7 states when result-bearing claims may
  use observed status
- Live finding: a larger v7 slice still produced a result-bearing claim without
  a matching deterministic result fact; prompt v8 omits failed-attempt and
  verification claims when that stronger evidence is absent
- Accepted follow-up: make retention and training requests explicit optional
  route choices, retain sanitized operational failure categories, and keep
  unsupported schema constraints in local candidate admission
- Live validation: an approved partial real-session selection completed with
  three evidence-backed claims, and an exact rerun reused the stored result
  without another model call
- Live conformance: the public-data check passed against the pinned Cerebras
  route, production schema, and temperature-zero request with no candidates
- Evaluation harness: the digest-pinned 12-case corpus, production-path runner,
  human review sidecar, and offline scorer are implemented
- Evaluation baseline: the approved 12-case run and complete human review are
  recorded; 7 batches passed admission, 5 failed admission, and 1 of 14
  admitted claims was unsupported
- Verification checkpoint: V9 applies the smallest one-pass correction without
  weakening admission or adding a second model stage
- Comparison: the one approved unchanged-corpus V9 run admitted 11 of 12
  batches; all 10 admitted claims were supported in human review, 9 were
  useful, and 1 was weak
- Decision: close the checkpoint without a second verification pass; retain
  omitted decision and lesson claims as known recall limits for downstream
  evidence
- V2 expansion: the one approved 20-case run admitted 18 batches; all 20
  admitted claims were supported, while root-cause, decision, lesson, and
  implementation recall remained incomplete
- Roadmap: [V0 Milestone 2](../../docs/roadmap.md#v0-milestone-2-validated-semantic-claims)

## Goal

Complete the second real Noema workflow: a user selects one completed fact
analysis, explicitly approves a bounded remote request, and Noema turns its
deterministic facts plus digest-locked Sessions evidence into validated semantic
claims and durable knowledge events.

The milestone is complete when:

- one manual command loads an existing completed fact analysis, re-reads only
  its recorded Sessions revision, and refuses a changed document digest;
- the exact bounded, privacy-filtered input is sent through a provider-neutral
  structured-generation port only after explicit `--allow-remote` approval;
- model output remains a candidate until local schema, identity, evidence,
  privacy, contradiction, and deterministic-consistency checks admit it;
- a completed semantic `AnalysisRun`, admitted claims, `claim.admitted` events,
  and one subscriber-independent `analysis.completed` event commit atomically;
- an exact unchanged semantic configuration reuses the prior analysis without
  another model request or duplicate claim or event; and
- claims and their source evidence are inspectable without creating Content
  Scout jobs, summaries, knowledge units, embeddings, or agent artifacts.

## Implemented baseline

PR #11 established the local admission boundary. The durability slice now
persists that boundary without adding a remote model call. Do not rebuild it in
the remaining slice.

- `internal/domain/claim.go` owns the small claim vocabulary, untrusted
  candidates, admitted claims, requested routes, and model execution metadata.
- `AnalysisRun` already records ordered semantic input fact IDs, admitted claim
  IDs, and optional model metadata without changing fact analyses.
- `internal/evidence/reference.go` builds and validates shared digest-locked
  Sessions evidence references.
- `internal/application/semantic_input.go` builds one bounded contiguous entry
  selection with ordered facts, omissions, coverage, evidence IDs, and fixed
  byte and count limits.
- `internal/application/privacy.go` applies deterministic preflight and
  postflight blocking and redaction.
- `SemanticAnalyzer` assembles the provider-neutral generation request, calls
  the injected `SemanticGenerator`, and admits candidates through local schema,
  evidence, privacy, contradiction, attribution, and fact-consistency checks.
- The application semantic boundary, not a provider adapter, owns the
  versioned strict JSON Schema for candidate output. It is carried by
  `SemanticGenerationRequest`; adapters only translate it.
- The durability slice closed one narrow admission gap before persistence:
  generated `Actor` and `Origin` are strings constrained to the existing fixed
  admitted vocabularies and must match supporting evidence. Postflight privacy
  also scans them alongside statement, subject, and scope as defense in depth;
  it does not replace or broaden those admission rules.
- Routine tests use fake generation. Semantic claims, failures, and events are
  durable and inspectable. No concrete Gateway adapter, remote request, or
  semantic-analysis creation command exists yet.

The remaining remote slice must preserve the existing evidence, privacy,
identity, durability, and admission rules.

## Locked remaining decisions

### Claim ownership and identity

An admitted V0 claim belongs to exactly one semantic processing identity. Add
the semantic processing key to the claim fingerprint input and keep
`Claim.AnalysisRunID` as its single producing run.

- An exact unchanged request has the same processing key and reuses the
  completed analysis before generation, so it creates no new claims or events.
- A different entry range, input digest, fact set, prompt, schema, extractor,
  model route, or privacy policy has a different processing key and may produce
  a separate claim even when wording and cited evidence match.
- `claim.admitted` is emitted once for each newly persisted analysis-scoped
  claim.
- Cross-run semantic deduplication or consolidation remains a later,
  evidence-gated feature. Do not add a run-to-claim join table or global claim
  ownership model in V0.

### Completed and failed processing identities

The unique `analysis_runs.processing_key` is reserved for completed analyses.
A failed semantic run leaves that base column `NULL`, matching the existing
fact-analysis failure pattern. Semantic failure details may retain the
attempted processing key in a non-unique field for inspection.

CLI arguments and route-file parsing and allowlist validation happen before the
fact analysis is loaded. Their failures, and a missing fact-analysis ID, remain
operational errors. Analysis recording begins after the requested completed
fact analysis has been loaded, giving the run a truthful source identity and
stage. Once the fact analysis is known, digest-unavailable, preflight,
generation, postflight, admission, and other bounded processing failures create
an inspectable failed run when persistence succeeds. A failed attempt never
prevents a later manual retry with the same configuration.

Failed semantic details store only values that were actually established
before the failure. Preserve SQL `NULL` as unavailable rather than decoding it
to an empty string, zero value, or empty list. The existing bounded
`AnalysisRun.Error` category explains which operation failed; do not add a
second failure state machine.

- The validated requested route is present for every recorded semantic
  attempt.
- Ordered input fact IDs are `NULL` until bounded input construction succeeds;
  after that they are present, including a valid empty fact-ID list. The final
  semantic selection remains `NULL` until privacy filtering and post-filter
  bounds succeed.
- The privacy report is `NULL` until preflight produces one. A deterministic
  privacy block retains its bounded report when one was produced.
- The input digest is `NULL` until filtered bounded input is fingerprinted. The
  attempted processing key is `NULL` until the complete processing identity is
  computed.
- Model execution metadata is `NULL` until generation returns validated route
  metadata. It is present for later postflight or admission failures, but a
  generation failure must not invent it.
- Ordered claim IDs are `NULL` for every failed run because no claim is
  persisted. They are present for every completed run, where `[]` truthfully
  means the model returned no admissible candidates.

Inspection must preserve this distinction between unavailable and known-empty
values. A completed semantic analysis requires every detail above except the
failure-only attempted processing key.

### V0 execution serialization

V0 runs at most one semantic generation at a time per SQLite database. After
local preparation computes the processing key, the application acquires an
immediate SQLite write transaction, rechecks completed reuse inside it, and
holds it through generation and the final success or failure commit. The store
exposes only a narrow transaction-scoped semantic attempt handle; it never
imports or calls the generator.

This intentionally trades write concurrency for a small, inspectable guarantee:
two concurrent commands cannot invoke the generator at the same time, and a
successful first command makes a waiting caller reuse without a second model
call. WAL readers remain available. A concurrent caller either waits or fails
with a bounded persistence-busy error before generation. A failed first attempt
commits its failed run with a `NULL` base processing key before releasing the
transaction, allowing a waiting caller to make a new, separately inspectable
attempt. Do not add reservations, leases, stale-lock recovery, or distributed
coordination for V0.

### Event ownership

The semantic transaction creates:

- one `claim.admitted` event per newly stored claim, with the claim as subject
  type `claim` and its supporting and contradicting evidence references in
  `Event.Evidence`;
- one `analysis.completed` event with the analysis as subject type `analysis`
  and ordered claim IDs in its bounded payload, including when the claim list
  is empty.

Add `SubjectType` to the domain `Event` envelope. Both payloads include an
explicit schema version and no transcript body. New event fingerprints include
event type, subject type, subject identity, and payload schema. Existing
foundation event IDs and fingerprints remain unchanged during migration. No
subscription or job is created in Milestone 2.

### Remote route

The initial route remains `semantic-v1`: Vercel AI Gateway,
`openai/gpt-oss-120b`, and one pinned `cerebras` provider. It is not silently
constructed from application constants. The remote composition root resolves
it from one explicit JSON route file supplied to the command, validates that
file against the sole V0 allowlisted profile, and only then constructs the
adapter. No route file means no configured route and therefore no remote
request. The API key remains separate and comes only from
`AI_GATEWAY_API_KEY`.

The V0 file has one strict shape; unknown route fields or aliases fail:

```json
{
  "routes": {
    "semantic-v1": {
      "gateway": "vercel-ai-gateway",
      "baseUrl": "https://ai-gateway.vercel.sh/v1",
      "model": "openai/gpt-oss-120b",
      "temperature": 0,
      "providerAllowlist": ["cerebras"],
      "providerOrder": ["cerebras"],
      "requiredCapabilities": ["strict-json-schema"],
      "zeroDataRetention": false,
      "disallowPromptTraining": false,
      "timeoutMilliseconds": 60000,
      "maxOutputTokens": 4096,
      "maxRetries": 0,
      "routeVersion": "route-v1",
      "privacyPolicyVersion": "deterministic-privacy-v1"
    }
  }
}
```

The composition root selects `semantic-v1`; it exposes no arbitrary model or
provider override. The validated configuration and its stable digest cross the
application boundary. The digest is part of the semantic processing identity,
and the sanitized configuration is retained for inspection so the timeout,
output limit, retry, capability, routing, and privacy policy remain explicit.
The key is never included in that digest or persisted.

V0 never falls back to another provider. Retention and training booleans are
explicit, recorded route choices rather than mandatory controls; neither may
be changed by a command-line override. Gateway model rewrites are unsupported:
the adapter must
obtain the actual resolved canonical model and provider from trusted response
metadata and require both to match the requested route before any candidate can
be admitted. A future comparison model requires a separately named, explicitly
allowlisted route whose capabilities and privacy policy have been reviewed.

## Changes

### 1. Durability application workflow — implemented

Keep the current admission logic and add one narrow persisted workflow around
it rather than extending the foundation `Scanner` or `Distiller`.

- Load a completed fact analysis by ID and require the `facts` stage.
- Re-read its canonical Sessions identity and require the recorded document
  digest before generation.
- Extract one preparation path from the current analyzer as needed so route
  validation, bounded selection, privacy filtering, input digest, ordered input
  fact IDs, semantic selection, and processing key are computed once before
  generation. Do not duplicate input construction in a coordinator.
- Keep the application route input provider-neutral. For the fake-generator
  durability path, inject the validated `semantic-v1` request route and stable
  configuration digest that the later remote composition root will resolve.
- Define the versioned strict candidate JSON Schema at the application semantic
  boundary and carry its name, version, strict disposition, canonical JSON, and
  digest in `SemanticGenerationRequest`. Include the schema digest in request
  bounds and semantic processing identity. Carry the admitted schema identity
  (name, version, strict disposition, and digest) into semantic details; the
  canonical JSON remains application-owned and rebuildable. The domain keeps
  the bounded actor and origin vocabularies, but the V0 remote schema requires
  both fields to be null because the first live model could not reliably make
  evidence-supported provenance assertions. The fake generator must be able
  to inspect the schema without importing any provider type. Local evidence-
  matching validation remains authoritative after decoding and leaves room
  for a later extractor to populate those fields safely.
- Acquire the transaction-scoped semantic attempt handle, then find and return
  an exact completed semantic analysis before calling the generator.
- On a miss, call the existing `SemanticGenerator`, run the existing postflight
  and claim admission, and include the processing key in each claim identity.
- Extend postflight privacy to every generated string field: statement,
  subject, scope, actor, and origin. A protected value in any candidate fails
  the complete analysis before a claim or event is persisted.
- Build the semantic events only after every candidate has been admitted.
  Populate the explicit `claim` or `analysis` subject type and validate it
  alongside subject identity before persistence.
- Commit the run, semantic details, claims, and events through the same
  transaction-scoped attempt. Release it only after success or failure commits.
- Record bounded failed runs according to the locked failure rules. Persistence
  failure must not claim an inspectable analysis ID unless its failure record
  committed. Carry progress-aware optional details without replacing
  unavailable values with sentinels.

The slice continues to inject a fake generator. It carries an already validated
route value and digest but adds no route-file loader, remote adapter, or model
SDK.

### 2. SQLite semantic projection — implemented

Add `internal/adapters/sqlite/migrations/003_semantic_claims.sql` and one focused
semantic store file. Leave the replayed `002_fact_analysis.sql` unchanged.

- Add `semantic_analysis_details`, keyed to `analysis_runs`, with a required
  schema identity (name, version, strict disposition, and digest), sanitized
  validated route configuration and its digest; nullable ordered input fact
  IDs, ordered claim IDs, input digest, exact semantic selection, privacy
  report, and model execution metadata including optional cost when available;
  plus a nullable non-unique attempted processing key. Apply the locked presence
  rules with database checks where practical, and validate them in the store
  before commit and after load.
- Add `claims` with one producing analysis and ordinal, unique ID and
  fingerprint, the admitted fields, ordered supporting and contradicting
  evidence, ordered supporting fact IDs, semantic versions, requested and
  resolved route metadata, and creation time.
- Keep the existing `events` rows and foreign keys intact. Add one additive
  `event_subject_types` table keyed by event ID so every admitted event has an
  explicit subject type without rebuilding the foundation table. Backfill
  existing `observation.created` events as `observation` and `scan.completed`
  events as `scan`; fail migration validation if a retained foundation event
  cannot be assigned a known type. Do not rewrite existing IDs or fingerprints.
- Extend the domain event and foundation event construction/store path to write
  `observation` and `scan` subject types for new foundation events. Insert the
  event row and subject-type row in the same transaction. New fingerprints
  include subject type; encode event schema version in each payload.
- Add narrow methods to find a completed semantic analysis by processing key,
  load one by ID, commit its run/details/claims/events atomically, and record an
  inspectable semantic failure. Add the narrow immediate-transaction attempt
  handle used to serialize the reuse check, generation, and final commit; keep
  generator invocation in the application layer.
- Preserve claim and event order and verify stored IDs, fingerprints, producer
  IDs, and ordered detail IDs when loading.

Prove migration from an existing Milestone 1 database, atomic rollback, exact
reuse, empty-claim completion, stable event identities, successful retry after
a failed attempt, one generator call across concurrent identical requests, and
zero job, agent-run, content-idea, summary, or knowledge-unit rows. Persist and
inspect one early preparation failure with unavailable details and one
post-generation failure with route/model metadata but no claim IDs. The
migration fixture includes existing observation and scan events plus a job;
prove subject-type backfill preserves their IDs, fingerprints, and foreign-key
relationships, and prove an unrecognized retained foundation event fails
closed.

### 3. Semantic inspection and resolution — implemented

Extend `noema analyses show <analysis-id> [--resolve]` without adding the remote
analysis command yet.

- Inspect the stored run stage and load either a fact or semantic analysis.
- Show ordered semantic claims plus input digest, selection, privacy, model
  metadata, the schema identity, and the sanitized route configuration with its
  digest, including optional request cost when available.
- For failed semantic runs, show the bounded run error and omit unavailable
  detail values while preserving known-empty lists such as a completed
  `claimIds: []`.
- For `--resolve`, collect supporting and contradicting claim evidence and
  reuse one shared bounded resolver path with the existing digest, coordinate,
  and content-hash checks.
- Keep resolved text transient; never write it back to SQLite.

The offline durability acceptance test creates semantic analyses through the
application workflow with fake Sessions execution and fake generation, then
inspects them through the public command.

### 4. Vercel AI Gateway adapter — remote slice

Add `internal/adapters/aigateway/` as the concrete implementation of the
existing `SemanticGenerator` boundary.

- Use the official OpenAI Go SDK against Vercel's OpenAI-compatible Chat
  Completions endpoint, keeping SDK request and response types inside the
  adapter.
- Add one small strict JSON route loader at the composition boundary. It
  resolves `semantic-v1` from the supplied file, rejects missing routes,
  unknown fields, multi-provider values, mismatched allowlist and order, and
  every profile outside the exact V0 routing and execution allowlist. It
  accepts either explicit retention or training choice and returns a validated
  provider-neutral configuration plus stable digest.
- Accept only that injected validated route configuration and already filtered
  bounded input. Read the API key from injected configuration, never the route
  file, a persisted record, or CLI output.
- Keep the Gateway base URL exclusively in the validated route configuration.
  The adapter accepts an injected HTTP client so tests can intercept requests
  to that locked URL; production composition always supplies the normal HTTP
  transport. Expose no CLI, environment, or production constructor override
  for the base URL.
- Send the requested model, versioned system and user messages, strict JSON
  Schema supplied by the application, output-token limit, and `stream: false`.
  The adapter must not define semantic candidate fields or maintain its own
  copy of the schema.
- Pin the sole provider through Gateway `only` and `order`, send the configured
  zero-retention and no-training choices, disable SDK retries, and use a finite
  HTTP timeout. Derive these values from the validated route configuration
  rather than redefining them inside the adapter.
- Require exactly one response choice with Chat Completions finish reason
  `stop`, then decode its non-refusal structured response plus bounded usage,
  latency, request identity, and provider metadata. Missing finish metadata,
  output-token truncation, content filtering, tool calls, or any other finish
  reason fails generation before candidate decoding. Require the resolved
  provider and resolved canonical model to match the pinned provider and
  requested model. Missing, malformed, or mismatched resolved identity fails
  the attempt; do not accept a transparent Gateway model rewrite.
- Extend `ModelExecutionMetadata` with optional `CostUSD`. Decode Gateway
  `cost` only as a bounded, non-negative decimal string rather than a binary
  float. Preserve it on the semantic run when present; absence is valid, while
  malformed cost metadata fails the attempt. Do not copy cost into each claim.
- Return fixed operational categories for authentication, permission,
  rate-limit, rejected-request, recognized schema, context-limit, and
  content-policy rejection, upstream, timeout, transport, and invalid response
  failures without prompts, response bodies, authorization values, or raw
  provider errors. Unknown generator failures retain the generic category.

Adapter tests use the injected HTTP transport to reach `httptest` while the
request still targets the locked production URL. They cover
missing or mismatched resolved provider and model metadata, cover present,
absent, and malformed cost, prove the transmitted strict schema and declared
version match the application request, reject a schema-valid response with a
truncated or otherwise non-`stop` finish reason, and make no external request.
Route loader tests cover no configured route, all retention and training
boolean combinations, the accepted profile, unknown fields, and each changed
V0 routing or execution field. Decoded candidate output still passes through
the existing local admission validator.

### 5. Manual remote command and documentation — remote slice

Add:

```text
noema analyze claims <fact-analysis-id> --allow-remote \
  [--first-entry <n> --last-entry <n>] \
  --route-config <path> \
  [--database <path>]
```

- Reject the command before creating the remote adapter when `--allow-remote`
  or `AI_GATEWAY_API_KEY` is absent, or when `--route-config` does not resolve
  a valid `semantic-v1` route.
- Require entry bounds as a pair. An omitted range is allowed only when the
  complete retained snapshot fits every fixed input budget; otherwise fail and
  require an explicit range.
- Select the `semantic-v1` alias in the composition root and obtain Gateway
  URL, timeout, output limit, retry, capabilities, model, provider order, and
  privacy settings only from the validated route file. Reject model, provider,
  or route override flags rather than treating them as comparison knobs.
- Keep command construction behind one unexported dependency/factory seam so
  command tests can inject the intercepting HTTP client after the exact route
  profile validates. The production entry point always uses the real adapter
  factory and normal transport.
- Document what may leave the machine, partial-range semantics, preflight and
  postflight behavior, provider pinning, requested privacy controls, and stored
  model metadata. State that requested Gateway controls are not local proof of
  provider behavior.

### 6. Offline milestone acceptance — remote slice

Use a fake Sessions executable, fake Gateway server reached only through the
test transport seam, and temporary SQLite database to prove the complete manual
path:

- create a fact analysis, run semantic analysis, inspect observed and inferred
  claims, and resolve every claim reference against the matching digest;
- repeat the exact command and prove no second Gateway call or duplicate
  claim/event;
- prove at the processing-identity test seam that a versioned, allowlisted
  route-configuration change creates a new key while reusing the same fact
  analysis; production still exposes only the locked `semantic-v1` profile;
- reject unknown, out-of-selection, contradictory, protected, and
  fact-inconsistent candidates; accept an empty candidate set;
- fail closed on a changed digest, over-budget input, and missing remote
  approval before calling the Gateway; and
- create semantic claims and events but zero jobs, agent runs, content ideas,
  summaries, or knowledge units.

## Delivery order

1. **Admission slice — implemented:** semantic domain types, shared evidence
   references, bounded input, privacy policy, local claim validation, and fake
   generation.
2. **Durability slice — implemented:** processing identity before generation, SQLite
   claims/details, atomic semantic events, exact reuse, failure recording,
   semantic inspection, and digest-locked resolution.
3. **Durability cleanup — implemented:** make preparation state private, split the
   semantic store by responsibility, and share event fingerprint construction
   without changing any durable or observable contract.
4. **Remote slice — implemented:** Vercel adapter, explicit CLI
   approval/configuration, documentation, and offline end-to-end acceptance.
   Run one real approved session only after every local gate passes.

Keep one plan and one integration owner because selection, processing identity,
claim validation, model metadata, and atomic events form one admission
contract. Do not introduce a generic model framework or redesign the event
system.

## Verify

For the durability slice:

- run focused application tests for prepare-before-generate, exact reuse,
  processing-scoped claim identity, failures, empty results, generated-field
  postflight coverage, injected route/configuration identity,
  application-owned output schema identity and V0 null actor/origin fields, serialized
  concurrent generation, and event construction;
- run SQLite tests for migration, atomicity, ordering, ID verification, reuse,
  event subject-type compatibility, durable schema and route identity, early-
  and late-phase failure presence rules, failure retry, and forbidden downstream
  rows;
- run CLI tests for fact and semantic inspection plus digest-locked resolution;
- run the offline durability acceptance flow and `make check`.

For the remote slice:

- run focused route-loader and Gateway adapter tests with `httptest`, including
  missing or weakened configuration, resolved route identity, and optional cost
  metadata;
- run the offline command acceptance flow and `make check`;
- with explicit user approval and a configured key, run one bounded analysis
  against an already-indexed real session. Inspect support quality, weak or
  false claims, privacy blocks, omissions, and whether evidence justifies a
  second model pass or optional summary. Do not store private evaluation notes
  in the repository.

## Boundaries

- Do not modify Sessions, invoke `sessions index`, read its SQLite database,
  parse provider histories, persist a complete transcript, or resolve against
  a changed digest.
- Do not reuse the foundation `Observation` or `Distiller` as a semantic claim,
  and do not modify the earlier fact analysis when creating a semantic run.
- Do not add Content Scout fields, subscriptions, jobs, worker generalization,
  artifacts, summaries, knowledge units, episodes, embeddings, full-text
  search, scheduling, retries, fallback providers, Inngest, Cloudflare, or a
  public plugin/configuration system beyond the one strict route file required
  for the remote command.
- Do not claim that local validation proves semantic entailment, that a valid
  evidence ID makes a claim true, or that a requested privacy control proves an
  upstream provider's behavior.
- Stop and revise the plan if the pinned provider cannot satisfy structured
  output plus the configured Gateway privacy choices; if the Gateway cannot
  expose a trustworthy resolved canonical model or prevent or detect rewrites;
  or if a real approved session cannot fit a meaningful explicit entry range
  within the bounded input contract.
