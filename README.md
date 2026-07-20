# Noema

> **Noema** — from Greek — means *thought*, *concept*, or *what is thought
> about*.
>
> In philosophy, it is the object or content of a thought, feeling, or
> perception: the thing as it is intended or understood by the mind.

The name reflects the project's purpose: turning evidence from our work into
explicit, traceable knowledge that focused agents can act on.

Noema is a local-first knowledge, event, and agent runtime for personal work.
It turns evidence from the tools where work happens into normalized knowledge,
durable events, and useful outputs from small, focused agents.

The first source is [Sessions](https://github.com/ferueda/sessions). The first
agent is Content Scout, which finds evidence-backed content ideas from recent
AI-assisted development work.

Noema is in early implementation. The current code establishes the local
producer-to-worker spine: normalized evidence, events, jobs, agent runs, and
content ideas persist in SQLite, and the producer and worker communicate only
through that database. The Sessions adapter and remote model adapters are the
next milestones; `scan sessions` and `worker --once` deliberately refuse to
make remote calls until those safety boundaries are wired.

The current sources of truth are:

- [Project intent](docs/project-intent.md): purpose, priorities, boundaries,
  safety rules, and the first useful outcome.
- [Architecture](docs/architecture.md): system boundaries, data flow, core
  concepts, and the first vertical slice.
- [First experimental implementation plan](dev/plans/260719-first-vertical-slice.md):
  the deliberately small Sessions-to-content-ideas V0.

Noema is a standalone project. It does not depend on Harness, Factory work
items, Inngest, or any single model, queue, workflow engine, or storage
provider.

## Development

Run the complete local gate:

```sh
make check
```

Inspect a Noema database:

```sh
go run ./cmd/noema jobs list --database /path/to/noema.db
go run ./cmd/noema ideas list --database /path/to/noema.db
```

The end-to-end integration test uses a fake source, distiller, and Content
Scout. It opens separate producer and worker database connections and proves
that SQLite is their only handoff:

```sh
go test ./internal/integration
```
