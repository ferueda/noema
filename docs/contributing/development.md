# Development

## Prerequisites

Noema uses the Go version declared in `go.mod`. Current routine development and
tests need no credentials, remote services, container runtime, Sessions library,
or model gateway.

Check the local tool and download module dependencies explicitly:

```sh
make check-env
make setup
```

`make setup` mutates only the Go module cache. It does not create configuration,
invent credentials, index Sessions, or modify a Noema database.

## Commands

| Command | Use | Repository mutation |
| --- | --- | --- |
| `make help` | List supported development commands | None |
| `make check-env` | Verify that Go is available | None |
| `make setup` | Download pinned module dependencies | Go cache only |
| `make test` | Run the fast Go test suite | Go cache only |
| `make build` | Compile all packages | Go cache only |
| `make check` | Run the complete local handoff gate | Go cache only |
| `make fix` | Format Go source files | Rewrites Go files |

`make check` is the definition of done for normal repository work. It checks Go
formatting, module tidiness, static analysis, race-enabled tests, and compilation.
It must not leave a `noema` binary or mutate tracked source.

The GitHub Actions workflow calls the same `make check` target. Do not duplicate
gate logic in workflow YAML.

## Failure flow

1. Read the first failing command and its native Go diagnostic.
2. For formatting failures, run `make fix` and inspect the resulting diff.
3. Run the narrowest relevant test while iterating.
4. Run `make check` before handoff or publication.

Fix failures at their source. Do not weaken a gate merely to make a change pass.

## Local data and external systems

Routine development commands must not read or write a user's Sessions library,
provider history, Noema database, credentials, or remote model account. Tests use
generic fixtures, temporary directories, fake process boundaries, and isolated
SQLite databases.

Live integration checks require an explicit command, explicit user authority,
documented credentials, a bounded target, and cleanup. They are not part of the
normal local or CI gate.

## Manual Gateway conformance

After obtaining explicit approval, check the pinned semantic route with fixed
public input:

```sh
export AI_GATEWAY_API_KEY='<gateway-key>'
go run ./cmd/noema gateway check --allow-remote \
  --route-config ./config/semantic-route.example.json
```

The command makes one small paid request through the production semantic prompt,
schema, route loader, and Gateway adapter. It does not read Sessions, open
SQLite, create a Noema record, or write a report. Success identifies the
resolved provider and model and reports bounded request, usage, latency, and
cost metadata. It proves the current live protocol path, not semantic quality
or provider privacy guarantees.

Never add this command to `make check`, CI, setup, or an automatic hook. Do not
run it without fresh authority merely because credentials are available.
