# Admit evidence-backed semantic claims

- Status: planned
- Roadmap: [V0 Milestone 2](../../docs/roadmap.md#v0-milestone-2-validated-semantic-claims)

## Goal

Implement the second real Noema workflow: a user selects one completed fact
analysis, explicitly approves a bounded remote request, and Noema turns its
deterministic facts plus digest-locked Sessions evidence into validated semantic
claims and durable knowledge events.

The milestone is complete when:

- one manual command loads an existing completed fact analysis, re-reads only
  its recorded Sessions revision, and refuses a changed document digest;
- the exact bounded, privacy-filtered input is sent through a provider-neutral
  structured-generation port only after explicit `--allow-remote` approval;
- the initial claim vocabulary is limited to problem, symptom, hypothesis,
  failed attempt, root cause, decision, solution, verification, and lesson;
- model output remains a candidate until local schema, identity, evidence,
  privacy, contradiction, and deterministic-consistency checks admit it;
- a completed semantic `AnalysisRun`, admitted claims, `claim.admitted` events,
  and one subscriber-independent `analysis.completed` event commit atomically;
- an exact unchanged semantic configuration reuses the prior analysis without
  another model request or duplicate claim or event; and
- claims and their source evidence are inspectable without creating Content
  Scout jobs, summaries, knowledge units, embeddings, or agent artifacts.

## Current inventory and decisions

| Area | Keep | Change in this milestone | Leave for later |
| --- | --- | --- | --- |
| Evidence and facts | Milestone 1 `EvidenceRevision`, `EvidenceRef`, `Fact`, digest-locked Sessions reader | Reuse one completed fact analysis as immutable semantic input | Multi-session selection and more sources |
| Analysis | `AnalysisRun` base identity, status, selection, omissions, and failure behavior | Add claim-stage inputs, outputs, and model execution metadata without changing the earlier fact run | Summary runs and episodes |
| Model boundary | No production implementation exists | Add one small structured-generation port and one Vercel AI Gateway adapter | Fallback routing and more gateways |
| Privacy | Local-by-default and explicit remote approval are documented | Add a deterministic preflight/postflight policy with blocking and redaction outcomes | Model privacy review and configurable policy language |
| Knowledge | Facts are durable and separate from the foundation `Observation` scaffold | Add distinct candidate/admitted claim types and validation | Knowledge units and semantic deduplication |
| Events | Foundation `events` table and append-only event shape | Persist stable claim and completed-analysis events in the semantic transaction | Subscription matching and jobs in Milestone 3 |
| CLI | `scan sessions` and `analyses show` | Add one manual claim command and semantic inspection | Automatic scans and workers |

Use a contiguous Sessions entry range as the remote unit. The command may omit
the range only when the complete retained snapshot fits the fixed V0 input
budget; otherwise it fails before any request and tells the user to select an
explicit range. This keeps the first selector understandable and avoids an
opaque sampling algorithm. The semantic run records partial coverage whenever
the range excludes entries.

The initial route is the accepted `semantic-v1` alias: Vercel AI Gateway,
`openai/gpt-oss-120b`, and provider `cerebras`. The API key comes only from
`AI_GATEWAY_API_KEY`. Model and provider are explicit command configuration so
comparison runs receive distinct processing identities; V0 requires exactly
one provider and never falls back. The adapter sends `only` and `order` with
that provider plus request-level zero-data-retention and no-prompt-training
controls. It fails rather than weakening those controls.

Use the current official OpenAI Go SDK against the Gateway Chat Completions
base URL. Keep SDK request and response types inside the adapter, disable SDK
retries for the one-attempt V0 command, set an explicit timeout, send a strict
JSON Schema response format, and use the SDK's supported extra-field mechanism
only for Vercel's documented `providerOptions`. Parse bounded gateway routing
metadata when present; missing or malformed required resolved-provider metadata
fails the attempt instead of treating the requested provider as proof of the
resolved provider.

## Changes

1. `internal/domain/analysis.go` and new focused semantic domain files — add
   separate semantic records without broadening deterministic `Fact`:

   - `ClaimCandidate` is the structured model output before admission. It uses
     only claim type, statement, status, confidence, supporting and
     contradicting evidence IDs, optional supporting fact IDs, an outcome for
     verification and failed-attempt claims, and optional supported actor,
     origin, subject/scope, and causal attribution.
   - `Claim` stores the admitted form with stable ID and fingerprint, semantic
     extractor/schema/prompt versions, requested and resolved model-route
     identity, exact supporting and contradicting `EvidenceRef` values,
     supporting fact IDs, and creation time. It has no audience, hook, content
     format, personal weakness, recommendation, or workflow action fields.
   - Extend `AnalysisRun` with ordered input fact IDs, admitted claim IDs, and
     optional model execution metadata. Fact runs keep these fields empty and
     remain unchanged. Model metadata records alias, gateway, requested model
     and provider, resolved provider/model when supplied, route/privacy/prompt
     versions, request identity, token usage, and latency when available.
   - Define small enums for the accepted claim types, status
     (`observed`, `inferred`, `uncertain`), and attribution (`user`, `agent`,
     `environment`, `mixed`, `unknown`). Unknown optional actor/origin/scope is
     omitted rather than invented.

   Move stable construction of a Sessions `EvidenceRef` from the fact extractor
   into one use-case-neutral helper so semantic input can cite conversational
   entries using the same digest, coordinate, content-hash, and ID rules. Prove
   existing fact evidence identities do not change.

2. `internal/application/` — add a narrow `SemanticAnalyzer`, semantic input
   builder, claim validator, privacy policy, and store/generator ports rather
   than extending the foundation `Scanner` or `Distiller`:

   - Load the requested completed fact analysis and require stage `facts`.
     Re-read its canonical Sessions identity, require the recorded digest, and
     reject an unavailable revision before building or sending input.
   - Build one chronological, contiguous entry selection. Include stable
     evidence IDs, roles, entry/segment coordinates, bounded text, related tool
     metadata, ordered facts, omissions, and coverage. Cap each selected text
     value, the evidence section, fact section, entry count, and complete
     serialized request. Reject an over-budget complete snapshot unless the
     user supplied a smaller valid range; never silently drop the middle or
     claim complete coverage after truncation.
   - Treat transcript text as quoted evidence, never instructions. The
     versioned prompt asks for supported claims only, requires supplied
     evidence/fact IDs, distinguishes fact from inference and uncertainty, and
     permits an empty claims array.
   - Before generation, run deterministic privacy preflight over every outbound
     text field. Block secret-like values such as private keys, authorization
     headers, common provider-token shapes, and JWTs. Redact supported local
     absolute paths, URL credentials, and local or private-network hosts to typed placeholders
     while preserving the original evidence coordinates locally. Record policy
     version, redaction counts, and blocked categories, but never the matched
     secret. Run the same protected-pattern check on every model-generated
     free-text field, including statement, subject, and scope, before admission;
     actor, origin, attribution, type, status, and outcome are closed enums and
     are protected by enum validation. Any protected generated value fails the
     complete semantic analysis before claims or events commit.
   - Compute the processing key from the evidence revision and semantic
     selection, ordered fact IDs, semantic schema/prompt/extractor versions,
     exact route, privacy policy, and input digest. Reuse an exact completed run
     before invoking the generator.
   - Validate model candidates locally: closed enums and fields, non-empty
     bounded statements, finite confidence in `[0,1]`, at least one supporting
     evidence ID per claim, and unique supporting and contradicting IDs. Every
     evidence ID in both sets must resolve to an `EvidenceRef` in the exact
     outbound selection; any unknown ID rejects the candidate. The supporting
     and contradicting sets must be disjoint, every supporting fact must exist
     in the input fact analysis, and every cited fact's evidence must fall
     within the allowed selection.
   - Treat optional actor, origin, and causal attribution conservatively. An
     asserted actor or origin must match at least one supporting `EvidenceRef`
     and must not conflict with any other known actor or origin in the
     supporting set; otherwise the field must be omitted and an asserted value
     rejects the candidate. For V0, causal attribution may only be `unknown` or
     omitted. A human-looking entry, even one whose canonical actor is human,
     does not prove the user caused a problem, decision, or outcome. Support for
     `user`, `agent`, `environment`, or `mixed` attribution requires a later
     explicit evidence rule rather than model confidence alone.
   - Apply this small deterministic-consistency rule set rather than trying to
     infer truth from arbitrary prose:
     - `failed-attempt` requires outcome `failure`; an observed failed attempt
       must cite a supporting `exit-code` or `test-result` fact with outcome
       `failure`. `error-output` may support an inferred or uncertain failed
       attempt, but it never establishes the observed outcome by itself.
     - `verification` requires outcome `success`, `failure`, or `unknown`. An
       observed success or failure must cite at least one `exit-code` or
       `test-result` fact with the same outcome. An observed unknown must cite an
       unknown result fact or both success and failure result facts.
     - `not-applicable` facts never establish an outcome. A test command with
       outcome `unknown` cannot establish success or failure. Assistant
       narration without a supported result fact cannot establish observed
       verification.
     - For an observed outcome claim, the validator examines every
       `exit-code` and `test-result` fact linked to the cited supporting entry,
       tool result, or tool call, not only the fact IDs selected by the model.
       Any linked result with the opposite or unknown outcome rejects a claimed
       success or failure, so omitting a stronger conflicting fact cannot make
       the claim admissible. Noema does not silently downgrade it. The model may
       instead return an inferred or uncertain claim and must place the
       conflicting evidence in the contradicting set. Non-outcome claim types
       remain subject to identity, evidence, privacy, and contradiction checks;
       local code does not pretend to prove that their natural-language
       statement follows from the text.
   - Build stable claims and events only after validation. Create one
     `claim.admitted` event per newly admitted claim and one
     `analysis.completed` event carrying the semantic analysis ID and ordered
     claim IDs. Events contain bounded metadata and evidence references, never
     transcript bodies. Empty claims still produce the completed-analysis
     event.
   - Commit the semantic run, claims, and events through one store call. Record
     pre-request failures locally. A request that may have reached the remote
     gateway records a bounded failed run with route metadata and never retries
     implicitly. Persistence failure must not claim an inspectable analysis ID
     unless its failure record committed.

3. `internal/adapters/aigateway/` — implement the provider-neutral generation
   port using Vercel's OpenAI-compatible Chat Completions endpoint:

   - Accept only a validated route and already filtered bounded input; obtain
     the API key from injected configuration, never a persisted record or CLI
     output.
   - Send the task model, versioned system/user messages, strict JSON Schema,
     output-token limit, `stream: false`, and Gateway provider controls. Pin one
     provider through both `only` and `order`; request zero data retention and
     no prompt training; configure no SDK retries and a finite HTTP timeout.
   - Decode one non-refusal structured response, usage, latency, request or
     generation ID, and Gateway `providerMetadata`. Require the resolved
     provider to match the sole requested provider and preserve the resolved
     provider model when present.
   - Convert SDK or Gateway failures into bounded operational categories. Never
     include request prompts, response bodies, authorization values, or raw
     provider errors in stored failures.

   Use an injected HTTP client/base URL in tests. Adapter tests assert the
   exact outbound privacy and routing fields, strict schema, retry policy,
   bounded response behavior, resolved-provider verification, refusal,
   malformed output, timeout, and sanitized errors. Routine tests make no
   network request and need no credentials.

4. `internal/adapters/sqlite/migrations/003_semantic_claims.sql` and a focused
   semantic store file — extend storage additively without altering the
   replayed `002` table definition:

   - Add `semantic_analysis_details` keyed to the existing `analysis_runs`
     record for ordered fact/claim IDs, input digest, semantic selection,
     privacy outcome, and model execution metadata. This avoids a destructive
     migration while keeping one base analysis-run identity.
   - Add `claims` with unique stable fingerprints, ordered evidence/fact ID
     JSON, semantic versions, and the producing analysis. Reuse the existing
     `events` table for knowledge events; encode schema version in each stable
     event payload until a later event migration is justified by Milestone 3.
   - Add narrow methods to load a fact analysis for semantic input, find and
     load a completed semantic analysis by processing key or ID, commit its
     run/details/claims/events atomically, and record an inspectable failure.
     Preserve claim and event order and verify stored IDs when loading.
   - Prove migration from a Milestone 1 database, exact reuse, atomic rollback,
     empty-claim completion, stable event identities, and no job/agent/content
     rows created by this transaction.

5. `cmd/noema/main.go`, CLI tests, and `README.md` — add the manual product
   path without combining it with the worker:

   ```text
   noema analyze claims <fact-analysis-id> --allow-remote \
     [--first-entry <n> --last-entry <n>] \
     [--model openai/gpt-oss-120b] [--provider cerebras] \
     [--database <path>]

   noema analyses show <analysis-id> [--resolve] [--database <path>]
   ```

   Reject the command before opening a remote route when `--allow-remote` or
   `AI_GATEWAY_API_KEY` is absent. Validate that entry bounds are supplied as a
   pair and are within the recorded snapshot. Keep the fixed alias, Gateway
   base URL, timeout, output limit, schema, prompt, route, and privacy versions
   in code for V0; model and sole provider flags are the only comparison knobs.
   Tests inject a fake generator and never read the real environment or network.

   Extend `analyses show` to display either fact or semantic analysis and its
   ordered outputs. `--resolve` follows claim evidence through the recorded
   digest using the existing bounded resolver behavior; it does not write the
   resolved text back to SQLite. Document exactly what can leave the machine,
   the preflight behavior, partial range semantics, route pinning, ZDR/no-
   training request, stored model metadata, and the fact that Gateway policy
   controls are requests whose enforcement is reported by the Gateway rather
   than a local proof of provider policy.

6. `cmd/noema/main_test.go` or `internal/integration/` — add one offline
   acceptance flow with a fake Sessions executable, fake structured generator,
   and temporary SQLite database:

   - create a fact analysis, run semantic analysis, inspect admitted observed
     and inferred claims, and resolve every claim reference against the matching
     Sessions digest;
   - repeat the exact semantic command and prove no second generation call or
     duplicate claim/event; change prompt or route configuration and prove a
     new semantic run reuses the same fact analysis without reindexing;
   - reject unknown, out-of-selection, contradictory, secret-bearing, and
     fact-inconsistent candidates, including error output paired with exit code
     zero; accept an empty candidate set;
   - fail closed on a changed Sessions digest, over-budget unbounded input, and
     missing remote approval before calling the generator; and
   - assert semantic completion creates claim and analysis events but zero
     jobs, agent runs, content ideas, summaries, or knowledge units.

## Delivery order

1. **Admission slice:** semantic domain types, shared evidence references,
   bounded contiguous input, privacy preflight, and local claim validation with
   fake generation. This proves the highest-risk authority and privacy boundary
   offline.
2. **Durability slice:** additive SQLite projection, atomic claims/events
   commit, exact-run reuse, semantic inspection, and digest-locked resolution.
3. **Remote slice:** Vercel adapter, explicit CLI approval/configuration,
   README, and the offline acceptance flow. Run one real approved session only
   after all local gates pass.

Keep one plan and one integration owner because selection bounds, processing
identity, claim validation, model metadata, and atomic events form one semantic
admission contract. Do not split out a generic model framework or event-system
redesign.

## Verify

- Run focused domain/application tests for bounds, privacy, candidate
  validation, fact precedence, empty results, processing identity, and failure
  behavior.
- Run focused Gateway adapter tests with an `httptest` server and SQLite tests
  for migration, atomicity, ordering, reuse, and forbidden downstream rows.
- Run the offline CLI acceptance flow and `make check`.
- With explicit user approval after automated checks, run one bounded claim
  analysis against an already-indexed real session and the configured Gateway
  route. Inspect support quality, false or weak claims, privacy blocks,
  omissions, and whether a second model pass or optional summary is actually
  justified. Do not store the private evaluation note in the repository.

## Boundaries

- Do not modify Sessions, invoke `sessions index`, read its SQLite database,
  parse provider histories, persist a complete transcript, or resolve against
  a changed digest.
- Do not reuse the foundation `Observation` or `Distiller` as a semantic claim,
  and do not modify the earlier fact analysis when creating a semantic run.
- Do not add Content Scout fields, subscriptions, jobs, worker generalization,
  artifacts, summaries, knowledge units, episodes, embeddings, full-text
  search, scheduling, retries, fallback providers, Inngest, Cloudflare, or a
  public plugin/configuration system.
- Do not claim that local validation proves semantic entailment, that a valid
  evidence ID makes a claim true, or that a requested privacy control proves an
  upstream provider's behavior.
- Stop and revise the plan if the pinned provider cannot satisfy structured
  output plus the required Gateway privacy controls, or if a real approved
  session cannot fit a meaningful explicit entry range within the bounded
  input contract.
