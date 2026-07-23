# Noema

> **Noema** — from Greek — means *thought*, *concept*, or *what is thought
> about*.
>
> In philosophy, it is the object or content of a thought, feeling, or
> perception: the thing as it is intended or understood by the mind.

The name reflects the project's purpose: turning evidence from our work into
explicit, traceable knowledge that focused agents can act on.

Noema is a local-first derived-knowledge, event, and agent runtime for personal
work. It turns canonical evidence from the tools where work happens into
traceable facts and claims, durable events, and reviewable outputs from small,
focused agents.

The first source is [Sessions](https://github.com/ferueda/sessions). The first
agent is Content Scout, which finds evidence-backed content ideas from recent
AI-assisted development work. Content Scout is the first consumer, not the
product boundary: the same admitted facts and claims should later support
workflow improvement and personal coding-development agents.

Noema is in early implementation. It can process one explicitly selected,
already-indexed Sessions snapshot into deterministic, inspectable facts without
a model call. Exact unchanged reruns reuse the prior analysis, changed document
digests create a new analysis, and stored evidence resolves only while Sessions
can return the recorded revision. With explicit approval, the semantic path can
send bounded, privacy-filtered evidence and facts through a pinned Vercel AI
Gateway route, then atomically retain locally validated claims and knowledge
events. The public-data conformance command has passed against the pinned
Cerebras route; a reviewed generic evaluation corpus still gates completion of
this milestone. The local producer-to-worker spine also persists its foundation
records in SQLite; `worker --once` remains fail-closed until the later
agent-runtime milestone.

The foundation still contains Content Scout-specific request, worker, and
completion seams used by its fake end-to-end proof. The accepted architecture
requires those seams to become use-case-neutral as the real evidence, claim,
subscription, and artifact stages are implemented.

The current sources of truth are:

- [Project intent](docs/project-intent.md): purpose, priorities, boundaries,
  safety rules, and the first useful outcome.
- [Architecture](docs/architecture.md): system boundaries, data flow, core
  concepts, and milestone structure.
- [Roadmap](docs/roadmap.md): locked milestone sequence, completion gates, and
  evidence-triggered later work.

Noema is a standalone project. It does not depend on Harness, Factory work
items, Inngest, or any single model, queue, workflow engine, or storage
provider.

## Development

Noema requires the Go version declared in `go.mod`. Prepare a checkout with:

```sh
make check-env
make setup
```

Run the complete local gate:

```sh
make check
```

Inspect a Noema database:

```sh
sessions index
go run ./cmd/noema scan sessions '<canonical-id>' --database /path/to/noema.db
export AI_GATEWAY_API_KEY='<gateway-key>'
go run ./cmd/noema analyze claims '<fact-analysis-id>' --allow-remote \
  --route-config ./config/semantic-route.example.json \
  --database /path/to/noema.db
go run ./cmd/noema analyses show '<analysis-id>' --database /path/to/noema.db
go run ./cmd/noema analyses show '<analysis-id>' --resolve --database /path/to/noema.db
go run ./cmd/noema jobs list --database /path/to/noema.db
go run ./cmd/noema ideas list --database /path/to/noema.db
```

Check the live semantic route without loading Sessions or opening SQLite:

```sh
export AI_GATEWAY_API_KEY='<gateway-key>'
go run ./cmd/noema gateway check --allow-remote \
  --route-config ./config/semantic-route.example.json
```

Noema never runs `sessions index` implicitly. `scan sessions` invokes
`sessions export '<canonical-id>' --format jsonl --full`; `--full` means the
complete export-eligible retained Sessions snapshot, not proof that the source
provider captured everything. Noema stores the admitted revision and selection,
deterministic facts, bounded selected values, exact evidence coordinates, and
processing metadata. It does not store the complete exported transcript.
The local reader rejects exports larger than 64 MiB as an inspectable failed
analysis instead of buffering unbounded transcript data.

`analyses show` reads only Noema's stored derived records. `--resolve`
explicitly re-exports the selected Sessions identity and returns bounded source
segments only when its document digest exactly matches the stored revision. If
Sessions now returns another digest, resolution fails with
`source-revision-unavailable`; the prior facts remain inspectable.

The Milestone 1 scan and inspection path is local and makes no model or other
remote request. Set `NOEMA_SESSIONS_COMMAND` only when the Sessions executable
is not available as `sessions` on `PATH`.

`gateway check` sends fixed public synthetic input with no evidence entries or
facts through the production semantic prompt, schema, route loader, and Gateway
adapter. It requires explicit `--allow-remote`, `AI_GATEWAY_API_KEY`, and the
reviewed route file; it makes one small paid request. It does not invoke
Sessions, open or create a Noema database, admit claims, or write a report.
Success proves the current authentication, request encoding, pinned routing,
schema acceptance, response metadata, and empty-envelope decoding path. It does
not measure semantic quality or prove provider retention, training, or other
privacy behavior. The command reports bounded route, schema, provider, model,
request, usage, latency, and cost metadata, or one sanitized failure category.

`analyze claims` is the remote private-evidence path. It requires both
`--allow-remote` and `AI_GATEWAY_API_KEY`, plus the exact reviewed route in
[config/semantic-route.example.json](config/semantic-route.example.json). It
sends only the selected entries, deterministic facts, evidence IDs, omissions,
and coverage after deterministic privacy filtering; it does not send the
Sessions database, provider files, or Noema's source identity. If the complete
retained snapshot exceeds a fixed budget, the command fails before the request.
Use `--first-entry <n> --last-entry <n>` together to approve a smaller
contiguous range; that result is stored with partial coverage.

The route pins Cerebras, temperature zero, strict JSON Schema output, no
fallback, and no SDK retries. Temperature zero reduces sampling variation but
does not guarantee identical output. Zero data retention and no prompt training
are explicit route choices; the example currently disables both so it can run
on a Vercel Hobby team. Noema records those choices in the sanitized route and
processing identity, but they are Gateway requests rather than local proof of
provider behavior. Noema verifies the resolved provider and canonical model
before it admits output. It stores prompt and schema versions, token counts,
latency, Gateway request identity, and decimal USD cost when returned. The API
key, outbound prompt, raw response, and resolved evidence text are not
persisted.
Previously copied route files must add the explicit numeric `temperature` value
before Noema will accept them.

Remote failures retain only a fixed operational category such as permission
denied, schema rejected, context too large, content rejected, rate limited,
timeout, or invalid response; provider messages and response bodies are
discarded.
Local claim-admission failures follow the same rule: inspection reports fixed
categories for evidence and fact reference failures, attribution, provenance,
duplicates, values, or outcome failures (wrong claim type, unsupported result,
or conflicting result) without retaining rejected model prose.

The test suite includes both the foundation's fake source/agent spine and a
generic fake Sessions executable that proves revision-safe fact processing:

```sh
go test ./...
```

Contributor setup, command behavior, and test-layer guidance live in
[docs/contributing/](docs/contributing/index.md).
