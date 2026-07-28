# Produce evidence-backed content ideas through a portable agent boundary

- Status: approved 2026-07-28
- Plan review: pass (`20260728-175151-579287`)
- Roadmap: [V0 Milestone 3: Content Scout](../../docs/roadmap.md#v0-milestone-3-content-scout)

## Goal

Complete Noema's first fact → claim → event → focused-agent path. An explicit
local command matches one retained `analysis.completed` event to the in-code
Content Scout definition and creates or reuses one durable job. A separate
one-shot Noema dispatcher loads only the job's immutable knowledge inputs,
prepares one bounded versioned execution request, and wakes a separately
running local Eve Content Scout. Noema treats the returned candidate result as
untrusted, admits it locally, and atomically stores zero to five ranked
`content-idea` artifacts with exact claim, fact, and Sessions-evidence lineage.

This is one scoped vertical slice, not a general agent platform. The slice is
complete when routine offline tests prove SQLite-only producer/worker handoff,
the language-neutral executor contract, configuration-sensitive job identity,
exact reuse, generic run and artifact contracts, bounded privacy-safe
generation, terminal failure inspection, and zero-result behavior. A fixed
public canary then proves the Eve HTTP and AI Gateway path. A separately
approved real-session run supplies the V0 product evidence; generated ideas
remain local proposals requiring human review.

The user accepted one task-specific cost decision on 2026-07-27: Content Scout
is a non-interactive, non-critical background workload and should request
Vercel's lower-cost Flex service tier. Flex is best effort. Noema accepts and
records the requested tier, observed Gateway generation ID, usage, and cost,
and includes the complete requested route in job identity. Eve 0.27.8 does not
expose the applied tier or resolved provider through its public durable stream,
so Noema leaves those fields unset rather than treating configuration as an
observed response. Eve may perform versioned framework-owned recovery inside
one invocation; Noema still makes one terminal job attempt. This decision
applies to Content Scout; it does not change the existing semantic route or
create a general service-tier policy.

The implementation must preserve these constraints:

- Sessions remains an external evidence plane. Content Scout reads no Sessions
  data and does not re-resolve transcript bodies.
- Facts and claims remain the admitted knowledge inputs. No summary or
  knowledge-unit prerequisite is added.
- The retained semantic event stays independent of subscribers. Matching never
  republishes claims or reruns semantic extraction.
- Queue, dispatcher, execution, run, and completion contracts know only generic
  knowledge references, versioned executor payloads, and artifact envelopes.
  Content Scout alone owns `ContentIdeaV1`.
- The agent implementation language and runtime are adapter choices. Eve gets
  no SQLite or Sessions access and never constructs authoritative Noema
  artifacts.
- V0 calls Eve only over loopback, starts one fresh execution per job, and
  disables every user-callable tool, connection, subagent, schedule, and
  cross-job memory surface. Eve's internal structured-output finalizer is
  permitted only as protocol machinery.
- Remote work remains explicit, route-pinned, privacy-filtered, and
  human-reviewed. No idea is treated as safe to publish automatically.

## Delivery slices

The numbered changes below are the complete Milestone 3 acceptance ledger, not
one pull-request boundary. Implement them through this dependency graph:

```text
1. Contracts and V1 runtime freeze
   ├── 2. Retained event → deterministic V1 job
   │      └── 3. Offline dispatcher → admitted artifacts
   ├── 4A. Go Eve HTTP adapter
   └── 4B. TypeScript Eve agent
                  │
3 + 4A + 4B ──────┴──→ 5. Production integration and acceptance
```

1. **Contracts and V1 runtime freeze.** Add the shared V1 domain
   values, execution and candidate JSON Schemas, configuration shapes,
   canonical digest rules, cross-language fixtures, and additive runtime
   tables. Define the two-phase queue
   contract as inspect oldest pending V1 job → perform local or remote
   preflight → claim that exact still-pending job. Do not switch job insertion,
   worker behavior, or public commands yet. This serial prerequisite prevents a
   half-migrated worker and freezes the contract used by every later branch.
2. **Retained event to deterministic V1 job.** Add the in-code Content Scout
   definition, strict agent and disclosure configuration loading, subscription
   matcher, atomic V1 job and detail insertion, safe list/show inspection, and
   explicit `subscriptions match` command. This remains entirely local: it
   creates or reuses work without an executor, endpoint, credential, remote
   authority, or model call.
3. **Offline dispatcher to admitted artifacts.** Load and validate the job's
   ordered claims, derived facts, and evidence; prepare bounded generalized
   input; run a fake `AgentExecutor`; apply schema, lineage, privacy,
   disclosure, duplicate, and safety admission; and atomically persist the
   generic run, artifacts, optional idea projection, and job completion. Include
   the zero-claim local completion path and replace the obsolete foundation
   spine as part of the V1 cutover. This is the first complete
   offline event → job → execution → artifact vertical slice.
4. **Parallel executor adapters after Slice 1.**
   - **4A: Go Eve HTTP adapter.** Own only `internal/adapters/eve/` and its
     fixtures and tests: loopback and route authentication, health and info
     checks, fresh sessions, the exact Eve 0.27.8 stream-format-19 state
     machine, bounds, forbidden events, failure cascades, safe receipts,
     redirects, timeouts, and sanitized errors.
   - **4B: TypeScript Eve agent.** Own only `agents/content-scout/`: pinned
     Node, npm, and Eve dependencies, instructions, strict output-schema use,
     Azure-only GPT-5.4-mini Flex routing, authenticated HTTP channel, disabled
     capabilities, absent cross-job state, and contract tests against the
     frozen fixtures.
5. **Production integration and acceptance.** Wire `worker --once` to the real
   Eve adapter, add remote authority and endpoint handling, complete job
   inspection, root Node/npm setup and CI, the fixed public canary, regression
   gates, and operator documentation. After routine checks and the public
   canary, explicitly run one selected real semantic analysis and review its
   receipt, exact lineage, privacy result, and zero-to-five ideas. The private
   run is acceptance evidence, not an automatic test or committed fixture.

Slices 2, 4A, and 4B may proceed in parallel in isolated branches or worktrees
after Slice 1 freezes their shared contracts. Slice 3 may begin after Slice 2
while 4A and 4B continue. Slice 5 integrates only after 3, 4A, and 4B pass their
own offline checks. The public positive Eve stream fixture is accepted only
after the fixed canary confirms that exact live sequence.

Every slice updates the tests and contributor or operator documentation for the
behavior it introduces. A slice must leave `make check` passing and must not
temporarily weaken semantic processing, privacy, or remote-authority gates.
Mutable configuration files and operational credentials never become semantic
job inputs; a job retains the bounded sanitized policy values required to
execute the exact reviewed configuration.

Milestone 3 deliberately does not preserve the walking-skeleton job, run, or
idea contract. Rows without V1 runtime details are outside the supported
contract: the V1 queue and inspection paths ignore them, do not backfill them,
and never guess their schema from JSON content. They may remain physically
present in a mixed database. This cutover does not require deleting the
database or alter retained facts, claims, analyses, or semantic events.

## Changes

1. `internal/domain/types.go`, a focused `internal/domain/agent.go`, and a
   focused `internal/domain/artifact.go` — replace the foundation's
   scan/observation-shaped `JobPayload`, `AgentRun.Output any`, and
   `JobCompletion.Ideas` with small versioned runtime values:

   - `KnowledgeInputRefsV1` containing one completed analysis-run ID and its
     ordered admitted claim IDs;
   - `AgentConfigurationIdentity` containing the prompt, output schema, route,
     privacy, disclosure, safety, and retrieval-policy versions plus one stable
     digest and bounded sanitized handler configuration JSON;
   - `AgentJobPayloadV1`, which contains only those inputs and configuration;
   - `AgentExecutionIdentity` containing the executor kind and version,
     agent-definition digest, execution-contract version, and recorded recovery
     policy;
   - a generic `AgentRunResultV1` containing an outcome (`succeeded` or
     `failed`), execution disposition (`skipped-no-claims`, `not-invoked`, or
     `invoked`), stage-appropriate optional executor and bounded model receipt
     metadata, privacy outcome, fixed safe failure category and stage when
     failed, and produced artifact IDs; and
   - an `Artifact` envelope containing kind, schema version, canonical typed
     JSON payload, producing run and trigger event, the triggering V1 inputs,
     locally resolved ordered supporting fact IDs, supporting and contradicting
     evidence, proposal status, and creation time.

   Move the existing content fields into a Content Scout-owned
   `ContentIdeaV1` payload and candidate type. Treat the candidate array order
   as relative strength; the model does not supply an artifact rank. Require
   bounded non-empty fields, zero to five candidates, at least one known claim
   ID and at least one supporting fact across those claims per admitted idea,
   valid format angles, confidence in `[0,1]`, and a runtime-assigned safety
   result. Derive ordered fact IDs and direct evidence from the cited admitted
   claims instead of trusting the model to reconstruct lineage. Filter a
   candidate whose otherwise valid admitted claims have no supporting facts,
   preserve the relative order of the remaining candidates, and assign their
   final sequential artifact ranks locally.

   Derive each artifact fingerprint from one canonical V1 structure containing
   the fixed artifact kind and schema version, the complete job fingerprint
   (which already commits to the trigger, immutable knowledge inputs, agent,
   and configuration), every admitted `ContentIdeaV1` content field except
   locally assigned rank, the ordered cited claim IDs, locally derived ordered
   fact IDs, ordered supporting and contradicting evidence references, and the
   runtime-assigned safety status. Do not include rank, run ID, timestamps,
   transport metadata, or executor receipt. Derive the artifact ID with
   `platform.DerivedID("artifact_", fingerprint)`. Reject duplicate artifact
   fingerprints within one candidate batch. This keeps an idea stable when a
   filtered sibling changes its final rank while making distinct content or
   lineage produce distinct artifacts. The `content_ideas` projection uses the
   exact artifact ID.

   Remove the unused foundation `EvidenceChunk`, `Observation`, `Scan`,
   `ScanCommit`, and scanner-only ports during the V1 runtime cutover. The
   public `scan sessions` command already uses `FactAnalyzer`, so this removal
   must not change fact or semantic analysis behavior. Do not add import,
   backfill, or compatibility readers for walking-skeleton jobs, runs, or ideas.
   Existing databases continue to open and retain supported fact, claim,
   analysis, and event data; V1 queue and inspection paths simply ignore
   runtime rows without `agent_job_details`.

   Define the result combinations strictly. `skipped-no-claims` is successful,
   has no receipt, and has no artifacts. `not-invoked` is failed, records a
   fixed stage and safe category for work that failed before calling
   `AgentExecutor`, and prohibits any executor receipt. Every response-decoding
   or local-admission failure after that call uses `invoked`. `invoked` may
   succeed or fail: success requires one validated receipt, while failure
   retains a receipt only when the adapter safely observed and validated one
   before the later failure. Failed results have no artifact IDs. Privacy
   metadata contains only policy stages that actually completed; absent values
   remain absent rather than being invented.

2. `contracts/agent-execution/v1/`,
   `contracts/agents/content-scout/v1/`,
   `internal/application/agent_execution.go`, and
   `internal/adapters/eve/` — add the first versioned language-neutral
   execution boundary. Keep the contract deliberately small:

   - `AgentExecutionRequestV1` carries the contract version, job and trigger
     identities, agent and configuration identity, one bounded input payload
     with its schema identity, required output-schema identity, deadline, and
     authority flags;
   - `AgentExecutionResponseV1` carries bounded canonical candidate JSON plus an
     `AgentExecutionReceiptV1`; and
   - the receipt carries executor kind/version, Eve session and turn IDs,
     completed model-step count, requested route identity, Gateway generation
     ID, usage, cost, latency, and fixed failure category when available.

   Store the shared request envelope and Content Scout candidate output as
   strict JSON Schemas. Go types and validation remain authoritative for Noema
   admission; Eve receives the same checked-in output schema per turn. Add
   cross-language fixture tests that the Go decoder and Eve-side TypeScript
   contract test both accept the same valid documents and reject unknown,
   malformed, oversized, or wrong-version documents. This is an internal
   versioned protocol, not a public plugin API.

   Add an `AgentExecutor` application port that accepts only the prepared
   request and strict output schema. Its Eve HTTP adapter:

   - permits only an explicit loopback base URL in V0;
   - checks `/eve/v1/health` and a bounded `/eve/v1/info` response before
     claiming work, requires info schema version `1`, and compares only fields
     that contract exposes: agent name, model, routing and provider options,
     static instructions digest, diagnostics, output-schema mode, and absence
     of enabled authored or user-callable framework tools, connections, skills,
     hooks, sandbox, subagents, schedules, or workflow; Eve's internal
     structured-output finalizer may be present;
   - starts one fresh `/eve/v1/session` for the job, includes the bounded
     request as untrusted JSON input, supplies the Noema-owned per-turn output
     schema, and consumes the durable NDJSON stream to a terminal result;
   - accepts exactly one `result.completed`, a successful terminal turn, and
     only the framework events needed for one no-tool execution;
   - rejects tool or action requests, input or authorization requests,
     subagents, unexpected continuations, unknown event shapes, missing or
     duplicate results, invalid finish reasons, redirects, oversized bodies,
     timeouts, and unsafe remote failure details;
   - retains only the validated result and bounded receipt. It never stores
     reasoning, assistant prose, raw events, remote error bodies, or
     credentials.

   Pin the adapter to Eve `0.27.8` stream format `19`. Require the NDJSON
   content type and exact `x-eve-stream-format: ndjson` and
   `x-eve-stream-version: 19` headers. Cap the stream at 2 MiB, each line at
   256 KiB, and the event count at 4,096. Every event must have only `type`,
   `data` when defined by the pinned type, and `meta`; `meta.at` is a bounded
   RFC 3339 timestamp. Bound all IDs to 256 bytes and the discarded
   continuation token to 4 KiB.

   Accept this exact successful conversation-turn sequence:

   1. one `session.started` whose required runtime agent ID and model and
      optional name match the probed agent, whose runtime Eve version is exactly
      `0.27.8`, and whose subagent invocation is absent;
   2. one `turn.started`, then one `message.received`, both at sequence `0`
      with the same non-empty turn ID; verify the received-message digest
      against the request and discard its text;
   3. one `step.started` at step index `0`;
   4. zero or more `reasoning.appended`, `reasoning.completed`,
      `message.appended`, or `message.completed` events at that same turn,
      sequence, and step; validate their cumulative/delta consistency and
      bounds, then discard all prose;
   5. one `step.completed` at that step with finish reason `tool-calls`,
      optional bounded non-negative usage, and optional Gateway generation ID;
   6. one `result.completed` after the completed step, at the same coordinates,
      containing the sole candidate result;
   7. one `turn.completed`; and
   8. one final `session.waiting` with `wait: "next-user-message"`.

   Sequence numbers, step indexes, turn IDs, event timestamps, and cardinality
   must be internally consistent and monotonically ordered. No event may appear
   after `session.waiting`. Recognize only the pinned terminal failure cascades
   `step.failed → turn.failed → session.failed` and
   `step.failed → turn.failed → session.waiting`, including the possible
   `step.completed` prefix when output-schema fulfillment fails; map their
   bounded codes to a fixed safe failure category and discard messages and
   details. Every other Eve event type—including actions, action results,
   input, authorization, subagents, compaction, cancellation, extra steps,
   session completion, or continuation turns—is forbidden. Framework-internal
   provider retries and empty-response recovery do not add public stream steps
   in Eve 0.27.8; they remain represented only by the pinned recovery-profile
   version, not invented event metadata.

   Use Eve's built-in `httpBasic()` route auth with the fixed username `noema`
   and a non-empty password read by both processes from
   `NOEMA_EVE_ROUTE_PASSWORD`. Noema sends that credential on `/info`, session
   creation, and stream requests; `/health` remains public and proves liveness
   only. Refuse to start either side when the variable is absent or empty.
   Treat the password as an operational secret: it never enters the agent
   configuration digest, job payload or identity, SQLite, receipts, errors,
   logs, or command output.

   Eve's public `step.completed` metadata exposes only the Gateway generation
   ID plus usage and cost, not applied service tier or resolved provider. Store
   those observed values and the requested route separately; leave unavailable
   response fields unset. Count observed completed model steps. Record the
   pinned Eve recovery-profile version because Eve may retry retry-class or
   empty model responses inside one invocation. This remains one Noema job
   attempt and creates no automatic Noema retry.

   Keep the existing `SemanticGenerator`, semantic route, evaluator,
   conformance command, Gateway adapter, processing identities, and failure
   behavior unchanged. Semantic extraction is a Noema processing stage, not an
   independently dispatched focused agent, so this milestone must not refactor
   it through the agent-execution port.

3. `agents/content-scout/` and
   `config/content-scout-agent.example.json` — add one standalone TypeScript Eve
   agent package, pinned to Eve `0.27.8`, Node `24.18.0`, and npm `11.16.0`,
   with its own lockfile, tests, `agent/agent.ts`, `agent/instructions.md`,
   channel auth, and explicit tool-disable files. Set `engines.node` to the
   pinned Node 24.18 line and `packageManager` to `npm@11.16.0`; do not accept
   whichever Node or package manager happens to be installed.

   Configure the agent with:

   - Vercel AI Gateway and `openai/gpt-5.4-mini`;
   - Azure as the only allowed and ordered provider;
   - strict structured output supplied by Noema on each turn;
   - requested `providerOptions.gateway.serviceTier: "flex"`;
   - zero-data-retention and no-prompt-training both explicitly `false`;
   - a model wrapper that applies numeric temperature `0`;
   - a 180-second Noema invocation deadline and a bounded Eve session token
     budget compatible with 4,096 maximum output tokens; and
   - no provider fallback authored by Content Scout.

   Disable `bash`, `read_file`, `write_file`, `glob`, `grep`, `web_fetch`,
   `web_search`, `todo`, `ask_question`, and root `agent` delegation. Declare no
   skills, connections, sandbox, subagents, schedules, channels beyond the
   authenticated Eve HTTP channel, or agent state. Configure that channel with
   Eve's `httpBasic()` helper, the fixed username `noema`, and
   `NOEMA_EVE_ROUTE_PASSWORD`. Disable input and output telemetry recording.
   Allow only Eve's internal structured-output finalizer; treat any other
   enabled or requested tool as a configuration or execution failure. Eve's
   local `.eve/.workflow-data` may contain an operational copy of the already
   generalized request and returned result; document it as private local
   execution state that Noema never reads as authority.

   The strict Noema agent file records the expected executor protocol and Eve
   version, agent and instructions versions, checked-in output-schema digest,
   Azure-only model route, Flex request, privacy choices, temperature, limits,
   disabled-tool inventory, and framework recovery profile. Canonicalize the
   whole file into the agent configuration digest. Endpoint and credentials are
   operational inputs and do not change semantic job identity. Before claiming
   a non-empty matching job, the worker compares the running agent's bounded
   `/info` projection with the observable expected fields above. `/info`
   version `1` is the info-schema version, not the Eve package version, and it
   does not expose the model wrapper's temperature or every session limit.
   Those remain requested configuration committed to the job digest, not
   observed runtime metadata. The later `session.started` event is the first
   public contract that exposes `runtime.eveVersion`; a mismatch occurs after
   invocation and becomes an `invoked` terminal failure with no artifact.
   Zero-claim jobs bypass this remote preflight. `AI_GATEWAY_API_KEY` belongs
   only to the Eve process.

4. `internal/application/subscription_matcher.go`,
   `internal/application/registry.go`, and narrow persistence ports — define
   Content Scout in code with its `analysis.completed` subscription, agent and
   admission versions, agent-execution contract, Eve definition and
   instructions versions, `ContentIdeaV1` schema,
   `content-scout-knowledge-v1` input policy, `content-safety-v1` policy,
   `content-disclosure-v1` policy, and validated agent configuration. Do not add
   a public agent-definition format.

   Add an explicit matcher that accepts one semantic analysis ID, loads its
   completed `SemanticAnalysisRecord`, validates its single retained
   `analysis.completed` event and exact analysis ID plus ordered claim IDs, then
   creates or reuses one job. `KnowledgeInputRefsV1` stores only those values.
   Supporting facts and evidence remain reachable through the immutable claims
   and are resolved later; the job does not separately carry all semantic input
   facts or any prior artifact.

   Derive the job fingerprint from the event ID, agent name/version, V1 analysis
   and ordered claim inputs, and complete agent configuration digest. A changed
   Content Scout configuration may therefore create another job against the
   same event; an exact match creates no duplicate. Matching performs no model
   call, requires no API key or remote approval, and never emits another event.

   Add:

   ```text
   noema subscriptions match <semantic-analysis-id> \
     --agent-config <content-scout-agent> \
     --disclosure-config <approved-public-terms> [--database <path>]
   ```

   The command reports the event, configuration digest, ordered job IDs, and
   which jobs were created or reused. The required disclosure file contains
   only an explicit bounded list of terms the user asserts are public and may
   appear literally, never credentials or private terms. Its canonical digest
   is part of the agent configuration and job identity; its sanitized approved
   terms are stored with the job so the worker executes the exact reviewed
   policy without rereading a mutable file. Matching reads and validates the
   local agent file but does not contact Eve, require an API key, or approve
   remote execution.

5. `internal/application/content_scout.go` and
   `internal/application/worker.go` — replace the content-specific `Agent`
   contract with agent-specific preparation and admission around the generic
   `AgentExecutor`. The worker owns job lifecycle and dispatch; the independent
   Eve agent owns instructions and model execution.

   Content Scout preparation loads only the job's selected completed analysis
   and ordered admitted claims. From those claims it derives the stable,
   deduplicated supporting fact IDs in first-reference order, then loads only
   those facts and the claims' already retained bounded evidence references. It
   fails closed if any claim, supporting fact, or evidence relationship no
   longer matches the retained analysis. It sends no source identity, document
   digest, complete transcript, raw provider record, prompt-like evidence
   instruction, or uncited fact. Include claim type, statement, status,
   confidence, outcome, supporting fact IDs, bounded fact values, opaque
   evidence IDs, coverage, and omissions. Apply fixed count, per-value, and
   complete-request byte limits before constructing
   `AgentExecutionRequestV1`.

   Traverse every outbound free-text field through
   `PrivacyPolicy.PreflightBatch`, then apply a separate fail-closed
   `ContentDisclosurePolicyV1`. The disclosure policy uses fixed, versioned
   token and phrase normalization to compare the exact private input with the
   outbound generalized input. It permits literal source-derived terms only
   when they belong to the policy's small in-code generic-language vocabulary
   or the explicit per-run `approvedPublicTerms`; every other source-derived
   token or multi-token phrase is replaced by a typed private-detail
   placeholder before generation. Unicode words, mixed-case or symbolic
   identifiers, issue-like values, and terms outside the generic vocabulary are
   protected by default. The compiled protected values remain transient; only
   category counts, policy version, and configuration digest may be stored.

   The Noema-owned strict output schema supplied to Eve allows an empty `ideas`
   array and at most five candidates containing concept, core lesson, audience
   benefit, hook, resonance, format angles, confidence, and cited claim IDs.
   Locally decode the returned JSON again and reject unknown or duplicate claim
   IDs, invalid values, duplicate ideas, unsupported lineage, and any output
   that fails `PrivacyPolicy.Postflight`. Filter candidates whose claims
   produce no valid supporting fact, then preserve relative order and assign
   final sequential artifact ranks starting at one. Run the disclosure policy
   again against generated fields.

   `ContentDisclosurePolicyV1` uses one closed output rule. Reject the complete
   candidate batch if a generated field contains:

   - an exact normalized token or phrase from the transient protected-input
     set;
   - a value rejected by the existing deterministic postflight scanners; or
   - an identifier-shaped value absent from the sanitized outbound input,
     in-code safe technical vocabulary, and normalized
     `approvedPublicTerms`.

   The fixed identifier grammar covers URLs and hostnames, email and IP
   addresses, filesystem paths, UUIDs and long hashes, issue-like keys, scoped
   packages and repository coordinates, camel- or Pascal-case names, snake-case
   or dotted identifiers, and non-generic all-caps alphanumeric codes. Ordinary
   prose is text that does not match one of those shapes; it may introduce
   normal words needed to explain an idea. Approval exempts only an exact
   normalized term or phrase, never a pattern class. Neither the rejected value
   nor generated prose is persisted.

   This deterministic V0 rule protects private names observed in the source and
   rejects common novel identifier shapes. It does not claim to recognize an
   arbitrary invented proper name that has ordinary sentence shape and never
   appeared in the source. Human review remains required for that residual
   quality and disclosure risk; adding a model classifier requires later
   evidence.

   Derive each artifact's non-empty ordered fact IDs and
   supporting/contradicting evidence from the cited claims. Assign
   `review-required` under `content-safety-v1` to every admitted idea only after
   both privacy and disclosure admission pass. This is the publication status,
   not a substitute for content-output privacy. The model never supplies or
   overrides either result.

   A completed analysis with no admitted claims still creates a job. The
   handler validates its local V1 identity, then atomically claims and completes
   it with a `skipped-no-claims` run, zero artifacts, no executor receipt, and
   no model request. This path requires no `--allow-remote`, agent file,
   endpoint, route password, Eve health or info probe, or `AgentExecutor`.
   Any preparation, executor, schema, admission, or privacy failure on a
   non-empty job is terminal for the V0 one-attempt job. Store one canonical
   failed `AgentRunResultV1`: failures before `AgentExecutor` invocation use
   `not-invoked` with no receipt; failures after invocation use `invoked` and
   retain only safely validated observed receipt fields. Store only a fixed
   safe failure stage and category, never remote or generated prose.

6. `internal/adapters/sqlite/migrations/004_agent_runtime.sql` and focused
   runtime-store files — add only idempotent `CREATE TABLE IF NOT EXISTS`
   changes because the current migration runner reapplies every migration on
   every database open:

   - `agent_job_details`, keyed by the existing job ID, stores payload schema
     version and configuration digest for exact worker selection; and
   - `artifacts`, keyed by generic artifact ID and unique fingerprint, stores
     the versioned envelope, canonical payload JSON, event/run/input lineage,
     ordered artifact-specific claim and fact IDs, evidence JSON, status, and
     timestamp.

   Continue storing `AgentJobPayloadV1` in `jobs.payload_json` and
   `AgentRunResultV1` in `agent_runs.output_json`; do not alter either table in
   this slice. Do not decode or inspect rows without `agent_job_details`; they
   belong to the unsupported walking-skeleton runtime. The V1 queue, job
   inspection, and idea inspection paths use an inner join or equivalent exact
   V1 lookup and never fall back based on the contents of `payload_json` or
   `output_json`. No migration backfills those rows.

   Land the migration and V1-only read boundary first, then switch new job
   insertion and worker reads to V1, then remove the foundation scanner and
   public old types. Insert a new job and its detail row atomically. Inspect the
   oldest pending V1 job and its bounded input identities without claiming it. A
   zero-claim job needs only a matching registered local handler and valid
   stored configuration identity before the local completion path claims it.
   Claim a non-empty job only when its agent name, version, and configuration
   digest match a registered executable definition and successfully probed Eve
   agent, so starting a worker with the wrong executor or agent configuration
   does not consume or fail another configuration's work. Load the claim-derived
   fact IDs in their derived order and fail closed on missing, duplicate,
   reordered, uncited, or cross-analysis inputs.

   On success, atomically insert the versioned run result, generic artifacts,
   and optional `content_ideas` query projection, then mark the job succeeded.
   Use the artifact ID as the projection ID. On failure, atomically insert the
   canonical failed V1 result into the existing non-null
   `agent_runs.output_json` and mark the job failed without artifacts. A
   pre-execution failure has `not-invoked`, its fixed safe stage and category,
   and no executor receipt; a post-invocation failure has `invoked` and only
   the bounded receipt fields actually validated before failure. Read
   `noema ideas list` from authoritative `content-idea` artifact payloads; the
   dedicated table remains a disposable query projection and cannot be
   required by generic completion.

   Add one job-centered inspection query that loads the job, trigger event,
   optional run, and ordered generic artifacts without resolving Sessions.
   Pending jobs have no run; succeeded zero-result jobs return a completed run
   and an empty artifact list; failed jobs return the fixed safe category and no
   generated prose. The inspection includes agent and configuration identity,
   immutable V1 analysis/claim inputs, executor receipt, requested and observed
   model metadata, policy metadata, artifact proposal and safety status, and
   stored claim, resolved fact, and evidence lineage. It distinguishes the
   requested Flex/Azure route from the Gateway generation ID, usage, and cost
   observed through Eve and leaves unavailable applied-tier and resolved-provider
   fields unset.

7. `cmd/noema/`, `internal/integration/spine_test.go`, and existing focused
   tests — wire the matcher and one-shot dispatcher through separate
   composition roots. `noema worker --once` may inspect the next V1 job before
   claim. For a non-empty job it must validate `--allow-remote`, the strict
   Content Scout agent file, explicit loopback `--agent-endpoint`, Eve health
   and info, and the complete executor identity observable through info before
   claim. For a zero-claim job it completes the local path without any of those
   remote inputs. On a remote path it reads the Eve HTTP Basic password only from
   `NOEMA_EVE_ROUTE_PASSWORD`, sends it on protected requests, and never prints
   or stores it. Noema does not read `AI_GATEWAY_API_KEY`; the separately
   started Eve process owns that credential. The command reports no-work,
   remote-authority-required without claim, succeeded-with-zero,
   succeeded-with-artifact IDs, or one fixed terminal failure category. It
   never loops, performs a Noema retry, or processes a differently configured
   job.

   Replace the fake source/distiller/observation spine with an offline
   integration test that seeds a valid retained fact and semantic analysis,
   closes the producer database, matches Content Scout, opens a fresh database
   connection, runs a fake `AgentExecutor` through the dispatcher, and inspects
   the resulting generic artifacts. Prove:

   - producer and worker share only SQLite;
   - shared JSON fixtures round-trip through the generic execution contract and
     Content Scout candidate schema in both Go and TypeScript tests;
   - a checked-in public synthetic
     `eve-0.27.8-output-schema-success.ndjson` fixture, captured through the
     fixed live canary and containing no private input or credential, proves the
     exact positive stream sequence; the canary must fail if the live sequence
     differs before this fixture is accepted or updated;
   - exact matching and execution create no duplicate jobs, runs, artifacts, or
     ideas;
   - changing only Content Scout configuration creates a new job without new
     semantic events or generation;
   - a worker with the wrong executor or agent configuration leaves the job
     pending and makes no executor call;
   - zero claims complete locally with `skipped-no-claims`, zero artifacts, and
     no remote flag, agent file, endpoint, password, Eve probe, or executor
     call;
   - for a non-empty job, missing remote authority or an unavailable or
     mismatched Eve `/health` or `/info` response leaves the job pending;
   - an Eve package-version mismatch in `session.started` occurs after claim
     and persists an `invoked` terminal failure with no artifact;
   - for a non-empty job, a missing or rejected Eve route-auth password fails
     before claim, leaves the job pending, and does not expose the secret;
   - unexpected Eve tools, subagents, questions, authorization, duplicate
     results, unknown events, redirects, or oversized streams fail safely
     without persisting their payloads;
   - the receipt preserves Eve session and turn IDs, completed step count,
     Gateway generation ID, usage, cost, and requested route while leaving
     unavailable applied-tier and resolved-provider values unset;
   - malformed, unsupported, unsafe, and remote-failure outputs leave an
     inspectable terminal run and no artifact;
   - a preparation failure persists a `not-invoked` failed result with its safe
     stage and category, no executor receipt, and no invented metadata;
   - an invoked failure retains only a safely validated partial receipt when
     one was actually observed;
   - a protected source value is generalized before generation and its
     reappearance produces no artifact or persisted matched value;
   - a novel generated issue-like identifier fails the complete batch even
     when it never appeared in the input;
   - an exact approved public term and ordinary safe prose pass disclosure
     admission;
   - filtering the first or a middle candidate for missing supporting facts
     preserves the remaining relative order and assigns sequential artifact
     ranks;
   - exact artifact fingerprinting is stable across rank changes, distinct for
     every changed admitted content or lineage field, rejects duplicates, and
     gives the query projection the same artifact ID;
   - each admitted idea has non-empty claim, fact, and evidence lineage that
     round-trips in exact order;
   - run, artifact, projection, and job completion are atomically visible; and
   - rows without `agent_job_details` are invisible to V1 queue and inspection
     queries and can never be claimed or decoded as V1.

   Keep the semantic conformance command, evaluator, processing identity,
   failure categorization, route loader, and strict claim decoding unchanged;
   run their existing regression suites to prove the agent boundary did not
   alter Milestone 2.

   Add `noema jobs show <job-id> [--database <path>]` and CLI-level tests for a
   successful artifact-producing run, a successful zero-artifact run, and a
   terminal failed run. The command returns the complete durable job inspection
   described above; it does not reconstruct provider responses or resolve raw
   evidence.

8. `cmd/noema/agent.go`, `.node-version`, `Makefile`,
   `.github/workflows/check.yml`, `README.md`,
   `docs/contributing/development.md`, `docs/contributing/testing.md`,
   `docs/architecture.md`, and
   `docs/roadmap.md` — add a fixed public Content Scout canary before private
   real-session use. The user starts the pinned local Eve agent explicitly. The
   canary checks its public `/health` and `/info` contracts, sends fixed public
   synthetic input through the production execution request and output schema,
   requests the Azure-only Flex route, consumes the Eve stream, verifies
   `session.started.runtime.eveVersion`, and verifies empty-output decoding plus
   the bounded receipt without opening SQLite or reading Sessions. It requires
   fresh `--allow-remote` authority and never enters `make check`.

   Add root `.node-version` containing `24.18.0`. Extend `make check-env` to
   require exactly Node `24.18.0` and npm `11.16.0`. Extend `make setup` to run
   the existing Go download plus `npm --prefix agents/content-scout ci`; setup
   is the explicit network-capable dependency-install step. Extend formatting,
   tests, builds, and the complete handoff gate with the agent package's
   scripts. `make check` never installs or updates dependencies; it is offline
   after setup and fails clearly when the locked install is absent.

   Update GitHub Actions to install Node from `.node-version`, install exactly
   npm `11.16.0`, enable npm caching against
   `agents/content-scout/package-lock.json`, run `make setup`, then run the
   unchanged canonical `make check` handoff gate. Keep dependency installation
   outside the gate itself. Routine checks use the Eve package's mock or fixture
   path; they never start the real agent server or read credentials.

   Document the manual flow:

   ```text
   export NOEMA_EVE_ROUTE_PASSWORD=<shared-secret>
   npm --prefix agents/content-scout run start
   noema subscriptions match <semantic-analysis-id> --agent-config <path> \
     --disclosure-config <path>
   noema worker --once --allow-remote --agent-config <path> \
     --agent-endpoint http://127.0.0.1:<port>
   noema jobs list
   noema jobs show <job-id>
   noema ideas list
   ```

   Record the resolved artifact/projection choice, portable execution boundary,
   local Eve startup and private workflow-state location, loopback route auth,
   disabled tool surface, strict agent file, Flex best-effort and Eve recovery
   behavior, unavailable applied-tier and resolved-provider observations,
   disclosure configuration and generalization limits, `review-required`
   publication meaning, the V1-only runtime cutover, and offline versus
   explicitly approved live proof. Mark Milestone 3 complete only after one
   approved real session traverses all three milestones and its ideas receive
   the roadmap's external human usefulness review.

## Verify

- `go test ./internal/domain ./internal/application ./internal/adapters/aigateway ./internal/adapters/sqlite ./internal/integration ./cmd/noema ./cmd/noema-semantic-eval`
- `npm --prefix agents/content-scout test`
- `npm --prefix agents/content-scout run check`
- `make check`
- With fresh authority after routine checks, start the local Eve agent and run
  the fixed public Content Scout canary once. Then run one explicitly selected
  real semantic analysis through subscription matching and `worker --once`,
  inspect the stored job, run, executor receipt, artifact, safety, and evidence
  lineage, and record idea usefulness in the roadmap's external review note.
  Neither live action is part of the automatic gate.

## Boundaries

- Do not add summaries, knowledge units, embeddings, full-text retrieval,
  Sessions re-resolution, evidence windows, manifests, multi-session analysis,
  or a second source.
- Do not add event scanning, cursors, scheduling, daemons, retries, leases,
  replay, a remote queue, a deployed Eve agent, Inngest, or Cloudflare
  execution.
- Do not add a plugin format, tool registry, dynamic subscription store, generic
  route bundle, or artifact-produced event.
- Do not enable any Eve tool, connection, skill, subagent, schedule, sandbox, or
  cross-job state in V0. Eve's internal structured-output finalizer is the only
  permitted protocol mechanism. Do not make Eve workflow state authoritative.
- Do not generate complete posts, threads, or articles; persist idea angles
  only. Do not publish, mutate a source, or apply an agent proposal.
- Stop rather than weaken the existing semantic processing identities,
  admission checks, privacy behavior, or immutable event identities to simplify
  the agent path.
