# Build the first experimental Sessions-to-content-ideas slice

## Goal

Prove Noema's core hypothesis with the smallest useful end-to-end path:

```text
Sessions evidence
  → bounded distillation
  → SQLite observations and events
  → one queued Content Scout job
  → at most five evidence-backed idea cards
```

This is an experimental V0, not a production knowledge system. It should teach
us whether small event-driven agents can turn everyday AI-assisted work into
useful content ideas before we invest in robust reconciliation, retries,
retrieval, or distributed infrastructure.

The standalone Go CLI supports:

```text
noema scan sessions --after <RFC3339> --before <RFC3339> --allow-remote
noema ideas list
noema jobs list
```

`scan sessions` reads already-indexed Sessions evidence for an exclusive time
window. It processes new bounded entry versions, stores observations and
events, creates one Content Scout job for the scan, runs that job synchronously,
and prints zero to five ideas. Each idea shows how the concept could work as a
short post, a thread, and an article.

Acceptance requires:

- Noema uses only the Sessions structured CLI and never indexes Sessions.
- A successful scan demonstrates the complete evidence → event → job → agent
  → artifact path.
- Every observation, event, run, and idea retains bounded Sessions evidence
  references.
- Repeating the same unchanged range makes no new model call and creates no
  duplicate derived records.
- Remote calls are explicit, bounded, privacy-filtered, and provider-pinned.
- Model output is schema-validated before entering SQLite.
- Partial Sessions coverage, skipped evidence, empty results, and failures are
  visible instead of being presented as complete success.
- Nothing is published automatically.

## Changes

1. `go.mod`, `Makefile`, `cmd/noema/main.go`, and `internal/config/` — create a
   Go 1.26 CLI with `scan sessions`, `ideas list`, and `jobs list`. Use OS user
   config and data directories with explicit file overrides for tests. Read the
   gateway key only from an environment variable. Require both exclusive scan
   bounds and `--allow-remote` before any model request.

   Ship two task-level aliases:

   - `distillation.primary`: `openai/gpt-oss-120b` pinned to `cerebras`.
   - `content-scout.primary`: `openai/gpt-5.4-mini` pinned to `azure`.

   Both routes require `zeroDataRetention: true` and
   `disallowPromptTraining: true`. Reject weaker or multi-provider routes in
   V0. The distillation route has a 60-second timeout and 4,096 output-token
   cap; Content Scout has a 90-second timeout and 4,096 output-token cap.
   Alternative models use separately selected aliases, never automatic
   fallback. Keep model IDs and Vercel settings out of domain and agent code.

2. `internal/domain/` and `internal/ports/` — define the minimum
   provider-neutral types and interfaces for:

   - bounded source chunks and evidence references;
   - observations;
   - domain events;
   - jobs and agent runs;
   - content ideas with short-post, thread, and article angles;
   - structured model generation.

   Use opaque local IDs, UTC timestamps, schema and prompt versions, and
   deterministic SHA-256 fingerprints. Keep three identities separate without
   adding more storage layers:

   - the evidence key describes only the canonical Sessions coordinate and
     admitted content;
   - the distillation key combines that evidence key with the distiller alias,
     prompt, schema, content scope, and version;
   - the Content Scout job key combines its ordered observation IDs with the
     agent alias, prompt, schema, content scope, and version.

   This lets a deliberate model or prompt change re-evaluate only that stage
   without copying source evidence. Exact unchanged keys make no model call. A
   model response is never evidence.

3. `internal/adapters/sessions/` — implement the source adapter through
   Sessions 0.1 JSONL commands:

   - list entries with `sessions entries --entry-after ... --entry-before ...
     --format jsonl` and follow cursors;
   - export each selected entry with a bounded `sessions export` ordinal range;
   - require schema version 1 and `untrusted-history`;
   - require each export document digest to match the inventory digest before
     any model call.

   Treat one exported entry as one stable distillation chunk. Skip and report an
   entry if its bounded export omits segments or if its admitted text exceeds
   64 KiB; do not add recursive splitting in V0. Fingerprint source evidence
   from source identity, entry coordinate, and a canonical representation of
   every admitted field that can affect the model input, including ordered
   segment kind, origin, origin confidence, content hash, and truncation state.
   Preserve the current document digest as provenance, but do not put it in the
   key: appending a later entry must not duplicate unchanged earlier evidence.
   Task aliases and processing versions belong to the separate distillation
   key, not the source-evidence fingerprint.

   Fail when Sessions is uninitialized. When Sessions reports incomplete
   capture, process usable retained entries but label the scan partial. The
   time-filtered API cannot report entries with no canonical timestamp, so V0
   does not claim coverage for them. Noema never invokes `sessions index`,
   requests `--full`, reads the Sessions database, or stores transcript bodies.

4. `internal/privacy/` — add a small deterministic safety boundary. Before a
   remote call, block likely credentials, private keys, security-sensitive
   details, configured client or private repository names, configured forbidden
   terms, and private URLs. Redact absolute home paths and email addresses.

   Default to private content scope, where generated ideas must generalize
   source-specific names and details. An explicit `--content-scope public`
   permits safe public project names but never secrets, client data, security
   details, or quotations. Reject generated content containing a protected term
   or a normalized sequence of eight or more words copied from evidence. Do not
   build a general privacy classifier in V0.

5. `internal/adapters/vercel/`, `internal/contracts/`, and
   `internal/prompts/` — implement Noema's structured-generation port using the
   official OpenAI Go client against Vercel AI Gateway's OpenAI-compatible Chat
   Completions endpoint.

   The adapter sends one pinned provider in both `providerOptions.gateway.only`
   and `order`, sends both privacy flags, disables SDK retries, and requests a
   strict JSON Schema response. It applies the configured context deadline and
   maximum output tokens to every request. Keep all SDK and Vercel types inside
   the adapter. Record requested model, resolved provider and model, token
   usage, latency, cost, and request ID when returned.

   Add two embedded, versioned contracts:

   - distillation returns zero or more observations with confidence and one or
     more admitted evidence IDs;
   - Content Scout returns an ordered list of at most five ideas. Each has its
     one-based rank, working concept, core lesson, audience benefit, hook,
     reason it may resonate, confidence, evidence IDs, and suitability and angle
     for a short post, thread, and article.

   Validate JSON Schema locally, strictly decode into Go types, reject unknown
   evidence IDs, and treat evidence text as untrusted history rather than
   instructions.

6. `internal/adapters/sqlite/` — use `modernc.org/sqlite` with embedded
   migrations and `database/sql`. Keep the first schema deliberately small:

   ```text
   scans
   evidence_chunks
   observations
   events
   jobs
   agent_runs
   content_ideas
   ```

   Store bounded evidence references as validated JSON fields on observations,
   events, jobs, agent runs, and ideas rather than adding relation tables in
   V0. Store no raw transcript, prompt, secret, or complete provider response.

   Use unique fingerprints so an unchanged chunk, observation, event, job, or
   idea is inserted once. Jobs have only `pending`, `running`, `succeeded`, and
   `failed` states. A failed V0 job is terminal and inspectable; retries,
   leasing, backoff, and recovery are deferred.

7. `internal/application/scan_sessions.go` — orchestrate one manual scan:

   - collect and validate all Sessions metadata and bounded exports before a
     model call;
   - filter private data;
   - reuse source evidence by evidence key and skip distillation keys already
     processed;
   - distill new chunks in batches of at most 20 entries and 64 KiB;
   - validate every distillation batch before persistence;
   - in one transaction, record every successfully processed chunk version,
     including chunks that produced zero observations, the new observations and
     their `observation.created` events, and the final `scan.completed` event
     plus `content-scout@v0` job when the scan produced an observation or a
     deliberate Content Scout configuration change produces a new job key for
     the retained observations in this exact scan;
   - after that transaction commits, claim and run only the job created by this
     command.

   Persist and display the current invocation's bounds, processing
   configuration, coverage, skips, and outcome even when no chunk is new. Reuse
   stored idea results only when an exact scan identity matches the bounds,
   source inventory fingerprint, content scope, selected aliases, and prompt
   and schema versions. A different or overlapping no-new scan reports its own
   result. It makes no model call unless one of the separate stage keys is new.
   A changed entry becomes a new immutable chunk version. V0 does not supersede
   old observations or infer source deletion; the CLI and README must state
   that current-state reconciliation is deferred.

8. `internal/agents/content_scout.go` — load only the new observations named in
   the job payload and their bounded evidence excerpts. Do not retrieve prior
   ideas, deduplicate across scans, expose tools, or loop with the model.

   Build one request with a hard cap of 100 observations and 64 KiB. If the
   fixed payload exceeds either cap, fail the job before a model call. Validate
   at most five output ideas and their evidence IDs, apply the output privacy
   check, and commit the run, ideas, and job result together. An empty result is
   a successful run.

9. `README.md` and focused tests — document setup, explicit Sessions indexing,
   configuration, remote-data implications, content scope, commands, and V0
   limitations. Use a fake Sessions executable, temporary SQLite database, and
   fake gateway for the end-to-end test.

   Cover:

   - Sessions pagination, incomplete coverage, oversized or omitted entry
     content, digest mismatch, command failure, and unsupported schema;
   - config, privacy filtering, exact gateway request fields, timeout and output
     token enforcement, and invalid model output;
   - the full successful path;
   - a second unchanged scan making zero model calls;
   - partial, empty, blocked, and failed scans;
   - a failed Content Scout job remaining visible and terminal.

## Verify

- Run focused package tests while implementing, then `make check`.
- In the end-to-end test, resolve every observation, event, job, run, and idea
  evidence ID to a stored Sessions source coordinate and content hash.
- With explicit authority and a gateway key, optionally smoke-test each shipped
  route using generic fixture text only. Recheck current provider privacy
  support before the request.
- Manually index Sessions outside Noema, run one approved scan, inspect no more
  than five idea cards, and confirm an unchanged rerun makes no remote call.

## Explicitly deferred

- Correcting or retiring old knowledge when Sessions evidence changes or
  disappears.
- Perfect behavior across overlapping scan windows.
- Retrying failed jobs, leases, backoff, crash recovery, replay, or a daemon.
- Cross-scan idea deduplication, idea strengthening, and idea lifecycle.
- Episodes, embeddings, full-text or vector retrieval, reranking, and
  model-facing tools.
- Inngest, Cloudflare storage or execution, a web UI, more sources, more agents,
  draft writing, and publishing.

These are follow-up hypotheses, not missing V0 implementation work. We should
add them only after using the first slice reveals which ones are valuable.
