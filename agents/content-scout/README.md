# Content Scout Eve agent

This package is the first execution-runtime adapter used by Noema. It accepts
one prepared Content Scout input from Noema and returns content ideas through
the strict output schema Noema supplies for that turn.

The agent has no shell, file, web, delegation, skills, connections, sandbox,
schedules, or authored state. Its only public channel is Eve's HTTP channel,
protected with HTTP Basic authentication.

## Local use

Use the exact runtime versions declared in `package.json`, then install and
build:

```sh
npm ci
npm run check
```

The build command supplies a non-operational placeholder route password when
the environment has none. Eve evaluates channel definitions while compiling,
but the built server still reads the real password from its runtime
environment. `npm start` fails closed when that value is missing or blank.

Running the agent requires:

```sh
export AI_GATEWAY_API_KEY=<gateway-key>
export NOEMA_EVE_ROUTE_PASSWORD=<shared-secret>
npm start
```

The HTTP Basic username is always `noema`. Noema and Eve must receive the same
route password through their own process environments.

Eve may write operational data under `.eve/.workflow-data`. That directory is
private, local runtime state. Noema never reads it as evidence or authority,
and it must not be committed.
