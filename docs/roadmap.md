# Roadmap

- Status: accepted product roadmap
- Date: 2026-07-22

## Purpose and authority

This roadmap turns Noema's accepted product intent and architecture into an
ordered path. It owns milestone sequence, completion gates, and the conditions
that justify later work.

- [Project intent](project-intent.md) owns purpose, priorities, hard boundaries,
  and unsafe assumptions.
- [Architecture](architecture.md) owns system boundaries and component
  responsibilities.
- This roadmap owns sequence and decision gates.
- `dev/plans/` contains temporary, executable plans for one milestone at a
  time. A roadmap milestone is not itself an implementation plan.

## Locked product model

```text
Sessions evidence plane
  canonical coding-agent history
  structural normalization
  provenance and retrieval
          ↓
Noema derived-insights plane
  deterministic facts
  semantic claims
  durable events
  focused agents
  reviewable artifacts
```

The following decisions are fixed for V0:

- Sessions owns canonical coding-session evidence. Noema consumes only its
  public, versioned CLI JSON or JSONL contract.
- Noema does not parse Codex or Cursor formats, read the Sessions database, or
  create another complete transcript or transcript-search archive.
- Canonical evidence is authoritative for what was recorded. Noema facts,
  claims, events, and artifacts are derived, versioned, and rebuildable while
  the referenced canonical evidence remains available.
- Code extracts observable facts before a model interprets their meaning.
- Facts, semantic claims, and agent artifacts remain separate authority
  classes. Every derived record retains exact source coordinates.
- Deterministic facts stay literal and preserve unknown outcomes; repeatable
  parsing is not treated as certainty.
- Facts and semantic claims use distinct domain types and validation paths even
  if they share an initial SQLite projection.
- Source identity plus document digest identifies an `EvidenceRevision`;
  selection bounds and coverage belong to the consuming `AnalysisRun`.
- Milestone 1 reads one complete export-eligible Sessions snapshot locally.
  Remote semantic and agent inputs remain separately bounded.
- Stored coordinates resolve only against their recorded Sessions document
  digest and fail closed when that revision is unavailable.
- Every processing attempt has an `AnalysisRun` that records its stage,
  evidence revision and selection, coverage, versions, admitted outputs, and
  final status.
- Summaries are optional, rebuildable projections over admitted facts and
  claims. They never replace those records or become source evidence.
- Evidence admission, fact extraction, and semantic claims are use-case-neutral.
  Content fields and personal-development judgments exist only in typed agent
  artifacts.
- V0 starts with one explicitly selected canonical session. Time-range,
  multi-session, and ambient scans come later.
- Go, SQLite, manual execution, and human review remain the V0 operating model.
- The separate producer and worker boundary remains because event-driven,
  focused agents are a core product hypothesis, not because V0 needs separate
  deployments.
- Subscription matching creates `SubscriptionJob` records from stable events.
  Evidence and semantic processing do not name or configure downstream agents.
- The generic worker records agent runs and versioned artifact envelopes.
  Registered in-code handlers own typed payload validation.
- Generic jobs reference a stable trigger and immutable knowledge inputs rather
  than a scan or one agent's payload shape.
- Content Scout produces ideas, not complete drafts, and never publishes.

## Foundation: runtime spine

- Status: implemented
- Evidence: PR #5

The foundation establishes the executable shape without claiming that real
Sessions or model behavior is complete:

- Go CLI and composition root.
- Durable producer, event, job, and worker process boundaries.
- Seven-table SQLite schema for scans, evidence metadata, observations, events,
  jobs, agent runs, and content ideas.
- Atomic producer writes and worker completion or failure writes.
- Separate producer and one-shot worker roles with SQLite as their only handoff.
- Stage-specific fingerprints and exact unchanged-run reuse.
- Evidence-backed output validation and terminal, inspectable failures.
- Job and idea inspection commands.
- An end-to-end proof with a fake source, distiller, and agent across fresh
  SQLite connections.

The real Sessions and model commands still fail closed. That is intentional.
The foundation's request, worker result, completion, and `content_ideas` write
path still contain Content Scout-specific types. They are accepted scaffold
seams, not reusable contracts, and must be cut over by the milestone gates
below.

## V0 Milestone 1: canonical evidence and deterministic facts

- Status: implemented

### Goal

Understand one explicitly selected Sessions session without a model call.

### Scope

- Accept one canonical Sessions identity supplied by the user.
- Invoke `sessions export '<id>' --format jsonl --full` to read the complete
  export-eligible latest retained snapshot locally.
- Validate schema version, `untrusted-history` disposition, canonical identity,
  document digest, entry and segment coordinates, bounds, omissions, and
  available coverage.
- Reconstruct ordered canonical entries transiently. Do not persist a complete
  transcript body.
- Persist the admission and processing metadata needed to explain the attempt:
  source contract and trust disposition, selected identity and digest,
  project or workspace and time scope when known, bounds, omissions, coverage,
  extractor and schema versions, admitted fact identities, and final status.
- Extend Noema evidence references to preserve the complete useful Sessions
  coordinate, including segment ordinal and content hash when present.
- Deterministically identify supported facts such as:
  - tool calls and results;
  - commands;
  - test commands and parsed test summaries;
  - outcomes only when structured status, observed exit code, or an exact
    supported parser establishes them;
  - compiler, package, or tool errors;
  - files mentioned or changed when evidence supports it;
  - repository or workspace metadata when available;
  - package names and URLs;
  - explicit verification evidence.
- Store these as a dedicated fact type with structured value, extractor name
  and version, parse rule, schema version, exact evidence, and an explicit
  success, failure, or unknown outcome when applicable.
- Add a local inspection path for the admitted evidence metadata, extracted
  facts, omissions, and failures.
- Remove agent configuration and job creation from the evidence and fact
  processing request.

### Gate

- No model, gateway configuration, or remote request is involved.
- Repeating the same canonical session version creates no duplicate facts.
- Every stored fact resolves to valid Sessions entry and segment evidence.
- Resolving a stored fact requires the current Sessions export to have its
  recorded document digest. A mismatch returns `source-revision-unavailable`
  and never substitutes coordinates from the newer document.
- A revision mismatch leaves prior facts and their lineage inspectable but marks
  their canonical content unavailable for resolution or rerun.
- Partial, omitted, malformed, stale, or unsupported evidence is reported
  honestly and never presented as complete.
- Full export is described as complete-retained-snapshot coverage, not proof
  that Sessions captured unsupported or missing provider content.
- Narrative assertions such as “tests should pass” cannot become successful
  outcome facts.
- A changed canonical document can be processed as a new version without
  silently overwriting the earlier derived identity.
- Generic fixtures exercise the public Sessions contract without private data.
- Evidence and fact fingerprints contain no Content Scout or other agent
  configuration.

### Not included

- Semantic interpretation.
- Content Scout jobs.
- Broad date-range or multi-session discovery.
- Raw transcript retention in Noema.

## V0 Milestone 2: validated semantic claims

- Status: in progress — admission, durability, bounded cleanup, explicit
  remote execution, and offline acceptance are implemented; one explicitly
  approved real-session evaluation remains

### Goal

Turn bounded canonical evidence and deterministic facts into admitted,
evidence-backed meaning.

### Initial claim vocabulary

- problem
- symptom
- hypothesis
- failed attempt
- root cause
- decision
- solution
- verification
- lesson

The semantic claim type remains small rather than introducing a large ontology.
It records:

- type and statement;
- status: `observed`, `inferred`, or `uncertain`;
- confidence;
- supporting and contradicting evidence;
- optional supporting fact identities;
- actor, origin, and subject or scope only when supported;
- causal attribution when relevant: `user`, `agent`, `environment`, `mixed`,
  or `unknown`;
- extractor, schema, prompt, model, and route versions.

### Scope

- Add the provider-neutral structured-generation boundary.
- Add explicit remote opt-in, one pinned provider per route, bounded inputs,
  deterministic privacy filtering, and recorded retention and training
  choices.
- Treat model output as untrusted candidate claims.
- Validate schema, evidence identities, confidence, status, contradictions,
  privacy, and consistency with deterministic facts.
- Apply coding-evidence precedence: test, compiler, and tool output is stronger
  than an assistant's narrative claim; an observed diff or edit is stronger
  than an unsupported assumption.
- Store admitted claims, processing identity, bounded failure details, and
  durable granular knowledge events. An empty result is successful.
- Record the semantic attempt in an `AnalysisRun` with the ordered reused
  fact identities, admitted claim identities, and the semantic schema, prompt,
  model, route, and privacy configuration used. Do not modify the earlier
  fact-only analysis.
- Keep summaries, if introduced for inspection, as separate versioned
  projections whose sections cite admitted fact or claim identities.
- Commit the semantic `AnalysisRun`, admitted claims, granular events, and
  subscriber-independent `analysis.completed` event atomically.

### Gate

- Every admitted claim has valid bounded evidence and passes schema, privacy,
  contradiction, and deterministic-consistency checks.
- Reviewed fixtures and explicitly approved local sessions measure semantic
  support quality; passing local checks is not represented as proof that the
  cited text entails the claim.
- Deterministic facts remain distinguishable from model interpretations.
- Protected content is blocked before remote transmission and after generation.
- Remote failures expose a useful fixed operational category without retaining
  provider messages, response bodies, prompts, or credentials.
- A reviewed generic fixture set and one explicitly approved local session
  produce inspectable results.
- A changed semantic configuration can rerun without reindexing or reading raw
  provider histories.
- Claim extraction contains no audience, hook, content-format, weakness,
  learning-recommendation, or workflow-fix fields.
- Deleting and rebuilding a summary, when one exists, cannot delete or alter
  its facts, claims, evidence references, or analysis lineage.

### Decision checkpoint

Begin with one semantic extraction pass. Add a model-based verification pass
only if evaluation shows unsupported or contradicted claims surviving local
validation.

Add a small `KnowledgeUnit` projection only if real claims prove too granular
for retrieval or Content Scout. Do not add it merely because it appears in a
long-term conceptual model.

Add a session or phase summary only when it materially improves inspection or
bounded retrieval. Its initial sections are problem, root cause, decisions,
attempts, solution, outcome, verification, lessons, and unknowns; it must not
become a second extraction schema or an input requirement for focused agents.

## V0 Milestone 3: Content Scout

### Goal

Prove that admitted semantic knowledge can trigger a focused agent and produce
useful, safe content ideas.

### Scope

- Read the retained Milestone 2 `analysis.completed` batch event for newly
  admitted semantic claims.
- Add deterministic subscription matching, match the in-code Content Scout
  subscription, and create one durable job without re-emitting the event.
- Keep the analysis event identity independent of Content Scout configuration.
  A changed agent configuration creates a new job against the retained event
  and ordered claim identities; it does not re-emit claims or rerun semantic
  extraction.
- Run the existing one-shot worker as a separate process role.
- Replace the scan- and Content Scout-specific generic job payload, `Agent`,
  worker result, and job completion contracts with immutable knowledge input
  references, a generic agent result, and a versioned artifact envelope.
  Content Scout retains its own typed `ContentIdeaV1` validation.
- Load only the immutable claim identities in the job payload and their bounded
  facts and evidence references.
- Produce zero to five ranked content ideas. Each includes:
  - concept and core lesson;
  - audience benefit;
  - hook and reason it may resonate;
  - short-post, thread, and article suitability and angle;
  - claim identities and direct evidence references;
  - confidence and deterministic safety outcome.
- Store every idea as a proposal for local human review.
- Persist content ideas through the generic artifact lifecycle. A dedicated
  `content_ideas` table may remain only as an optional query projection.

### Gate

- Producer and worker communicate only through SQLite.
- Every idea traces through admitted claims and facts to exact Sessions
  evidence.
- Changed Content Scout configuration can rerun without semantic
  re-extraction.
- An exact unchanged run creates no duplicate jobs, runs, or ideas.
- Empty results and terminal failures remain inspectable.
- No complete draft is generated and no content is published.
- Generic queue, worker, run, and completion ports do not import or return
  Content Scout payload types.

## V0 completion gate

V0 is complete only when:

- One explicitly selected and approved real session can traverse all three
  milestones.
- Safety, evidence, claims, runs, and ideas can be inspected without reading
  Noema internals.
- A manual evaluation note outside Noema records which ideas are worth keeping
  and why others are rejected. Persisting those decisions in Noema begins after
  V0.
- Repeated use shows that at least some ideas are worth developing further.
- We record missing evidence, extraction failures, false or weak claims, safety
  blocks, and which later abstraction those observations actually justify.

Completing the code path without useful ideas does not validate the product
hypothesis. It does, however, provide evidence for revising extraction or
stopping before adding more infrastructure.

## Later phases, gated by use

The expected order after V0 is:

1. **Idea decisions and feedback.** Record keep, reject, and reason so later
   ranking and drafting have real signals. The same decision model can later
   support approve, decline, and defer for non-content proposals.
2. **Knowledge units when needed.** Consolidate claims only if individual
   claims are too granular or lessons recur across sessions.
3. **Incremental session windows.** Replace manual entry-number selection for
   one growing session with deterministic local windows and process only new or
   changed windows after an explicit preview and remote approval.
4. **Multi-session analysis.** Add explicit manifest-backed evidence sets,
   bounded time ranges, correlation, and revisable episodes after one-session
   processing is trustworthy.
5. **Coding Evaluation.** Use bounded multi-session evidence to identify
   development patterns and propose concrete learning goals without treating
   every failure as a user weakness.
6. **Draft generation.** Generate complete short posts, threads, or articles
   only after idea-selection feedback exists.
7. **Second source.** Add Git, tests, notes, or another source when Sessions
   demonstrably lacks decisive evidence.
8. **Workflow Scout.** Test that the event and agent model is not
   Content-Scout-specific. It may propose creating or fixing skills,
   instructions, workflows, tools, configuration, or tests when repeated
   friction, corrections, failed approaches, or verification gaps support the
   proposal. Every proposal carries evidence, confidence, and expected benefit;
   it never changes the system by itself.
9. **Full-text derived retrieval.** Index Noema-owned facts, claims, and
   artifacts when exact and metadata queries become limiting.
10. **Semantic retrieval.** Add embeddings only after measured queries show that
   structured and full-text retrieval miss useful knowledge.
11. **Scheduling.** Automate runs only when useful manual runs are regularly
   missed.
12. **Remote execution.** Consider Inngest or Cloudflare only when local durable
    execution, waiting, approval, or access becomes the actual constraint.

### Incremental session windows milestone

This phase starts after Content Scout proves that one-session claims can create
useful, inspectable ideas, idea decisions and reasons are being recorded, and
the knowledge-unit checkpoint has either been implemented because evidence
requires it or explicitly recorded as unnecessary. It closes the gap between
manual semantic ranges and multi-session processing without adding discovery
or scheduling:

1. Plan one current, fully admitted session revision locally into ordered,
   contiguous windows. Start at human work, retain adjacent model and tool
   context, keep related tool calls and results in the same window, prefer a
   safe boundary after deterministic verification, and otherwise close at the
   semantic input limit or end of revision.
2. Store one versioned window-plan analysis with the fact-analysis identity,
   exact revision and coverage, ordered bounds, boundary reasons, provisional
   state, and content fingerprints. Store no transcript body or generated
   summary.
3. Fingerprint a window from its canonical source identity, planner version,
   ordered entry structure, and content hashes without the document digest.
   The same unchanged window can therefore be recognized after the canonical
   session receives a new revision.
4. Preview the plan without a remote call. Preview loads and records the
   reviewed route without an API key and uses the complete local semantic
   preflight, including privacy-filtered and full request-envelope size checks.
   A separate explicit execution approves a maximum number of new semantic
   attempts that may invoke the model, must use the previewed route digest, and
   processes windows in canonical order. Reused and deferred windows do not
   consume that limit.
5. Reuse a prior completed semantic analysis only when the window fingerprint
   and the complete fact, semantic-input builder and limits, schema, prompt,
   privacy, route, and adapter configuration match. A new or changed window
   gets a new revision-bound semantic analysis; a changed configuration reruns
   an unchanged window.
6. Treat the trailing end-of-revision window as provisional. Appending entries
   changes and reprocesses that window while stable earlier windows remain
   unchanged. A rewrite changes every affected fingerprint; old plans, claims,
   and events remain immutable.
7. Stop an execution at its first failed window. Already completed windows stay
   durable, and an exact rerun resumes without repeating them. Each newly
   completed window keeps the normal semantic `analysis.completed` event; the
   first slice adds no session-level reducer or event aggregation.

Cross-revision reuse is a processing decision, not evidence rebinding. Reused
claims retain their original Sessions document digest and may become
unresolvable if Sessions no longer retains that revision. The window plan must
show that condition honestly and must never apply old coordinates to the newer
document.

The milestone passes when a generic two-revision fixture proves that an
unchanged closed window makes no second model call, an appended or rewritten
window produces only the required new call, route or prompt changes rerun the
affected windows, plans and failures are inspectable, and no raw transcript text
is added to SQLite. Every automatically eligible window must pass the same
complete local generation preflight used immediately before execution. The
slice remains manual and single-session: it adds no
manifest, evidence set, scheduler, retry service, cross-session ranking, or
model-based window selection.

### Multi-session analysis milestone

This phase starts only after the one-session fact, claim, and Content Scout
path is useful and inspectable. It adds one inventory-first operation:

1. Select and validate one atomic, transcript-free Sessions manifest with
   explicit supported filters.
2. Persist a Noema `EvidenceSet` only for that explicit operation, including
   normalized selection, capture scope, ordered revision identities, document
   counts, and operation outcomes.
3. Match each revision against the exact completed processing identities for
   the requested stages. Canonical identity plus document digest answers
   whether source evidence changed; the full processing key answers whether a
   stage must rerun.
4. Hydrate only stage misses. Accept a full export only when canonical identity
   and document digest match the selected revision, with document counts checked
   as an additional consistency guard.
5. Mark unavailable or mismatched hydration incomplete without substituting a
   newer body. A refreshed manifest starts a new evidence set rather than
   changing the selected cohort in place.
6. Keep deterministic facts and initial semantic claims per revision. Later
   cohort-level analyses and agents reference the evidence set and reuse those
   per-revision analyses instead of combining transcripts into one document.

Manifest completeness, Sessions capture scope, Noema hydration outcomes, and
per-document analysis coverage remain separate. An empty manifest with
incomplete or uninitialized capture scope does not prove that no applicable
sessions exist. Roots and lineage remain source metadata, not episodes or
semantic relationships.

The cohort selector will be a separate application port from the existing
single-document reader when this phase begins. Do not add that port, an evidence
set table, or a manifest call during V0. Project-scoped selection also remains
deferred: the current manifest contract has no workspace or privacy-safe project
facet, so Noema must not infer project identity from roots or other metadata.

### Coding Evaluation milestone

Coding Evaluation is the first accepted extension test after Content Scout. It
must reuse the same facts, claims, event, queue, retrieval, run, and artifact
primitives rather than introduce an evaluation-specific extractor.

Its prerequisites are trustworthy multi-session selection, bounded retrieval,
supporting and contradicting evidence, and attribution that can distinguish the
user, coding agent, environment, mixed causes, and unknown causes.

The first `coding-assessment` artifact contains:

- evaluated projects, sessions, time range, and evidence coverage;
- growth areas or development patterns;
- observed behavior, likely impact, recurrence, and confidence;
- supporting and contradicting claim and evidence references;
- attribution, including unknown;
- a concrete learning goal, suggested exercise, and success criterion.

The milestone passes when reviewed assessments avoid attributing agent or
environment failures to the user, every recommendation traces to admitted
knowledge, and an empty assessment is valid. It does not score personality or
general ability and performs no external action.

## V0 non-goals

- A Noema-owned raw transcript archive or duplicate Sessions search engine.
- Direct Codex or Cursor parsing.
- Broad date-range, corpus-wide, or ambient scanning.
- A knowledge-unit layer before claims demonstrate a need.
- A second model verification pass by default.
- Full drafts or autonomous publication.
- More sources or agents.
- Coding assessments or personal-development recommendations.
- Cross-session semantic deduplication or strengthening.
- A daemon, scheduler, retries, leases, replay, or distributed execution.
- PostgreSQL, a graph database, embeddings, or a vector database.
- Inngest, Cloudflare storage or execution, a web UI, or a public plugin system.

These are not missing V0 work. Each has a named trigger above.

## Preserved product hypotheses

Earlier exploration produced several useful product ideas that are not part of
the accepted V0 sequence. They remain recorded so choosing Content Scout first
does not erase them:

- **Work Graph / Flight Recorder.** Reconstruct revisable work episodes with
  original and current goals, scope changes, decisions, failures, artifacts,
  validation, current state, and a cited resume card. This belongs with the
  later multi-session and episode work.
- **Improvement Inbox.** Present evidence-backed `create` or `fix` proposals for
  content, skills, workflows, instructions, tools, configuration, and tests.
  Content Scout is the first narrow extraction of this idea; Workflow Scout and
  persisted approve, decline, or defer decisions are later parts.
- **Deterministic Agent Eval Lab.** Compare prompts, models, tools, and
  configuration in replayable environments when evaluation drift becomes a
  concrete problem. This evaluates agent systems; it is distinct from Coding
  Evaluation, which helps the user identify development areas.
- **Context X-Ray.** Explain which instructions, skills, tools, hooks, and model
  configuration influenced a run when context assembly becomes hard to audit.
- **Recovery supervisor.** Suggest recovery from blocked work only after the
  evidence model is trustworthy and an explicit authority design exists.

These are hypotheses, not promises or implementation plans. A focused plan may
promote one only after the current system exposes the need it addresses.
