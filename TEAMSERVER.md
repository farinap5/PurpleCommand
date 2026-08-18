# PurpleCommand teamserver/client architecture

PurpleCommand is split into two processes:

- `purpcmd-teamserver` owns listeners, implant callbacks, sessions, tasks,
  Lua states, loot, build jobs, SQLite, and the implant RSA key. It is intended
  to remain running when no operator is connected.
- `purpc` is the transient operator CLI. Menu state and selected listener,
  session, or profile are local to that client and disappear when it exits.

The implant-facing HTTP and encrypted task protocol is unchanged. Only the
operator boundary moved to an authenticated HTTP/WebSocket API.

## Build and run

Build both binaries:

```sh
make build
```

Start a loopback teamserver with a shared operator token:

```sh
export PURPCMD_TOKEN='replace-with-at-least-20-random-characters'
./bin/purpcmd-teamserver -listen 127.0.0.1:8080
```

Connect the CLI when needed:

```sh
export PURPCMD_TOKEN='replace-with-at-least-20-random-characters'
./bin/purpc -server http://127.0.0.1:8080
```

The compatibility entrypoint `go run ./cmd/main.go` now starts the remote
client. The server is `go run ./cmd/teamserver`.

A non-loopback teamserver binding is rejected unless both `-tls-cert` and
`-tls-key` are set. The CLI validates TLS normally; `-insecure-tls` exists
only for controlled testing. There are deliberately no operator accounts or
server-side operator sessions: all clients use the same bearer token.

Teamserver flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-listen` | `127.0.0.1:8080` | Operator API address |
| `-token` | `PURPCMD_TOKEN` | Shared bearer token |
| `-tls-cert`, `-tls-key` | empty | TLS files; required off loopback |
| `-rsa-key` | `server.key` | Implant registration private key |
| `-database` | `database.db` | SQLite state |
| `-loot-dir` | `loot` | Loot files |
| `-script-dir` | `script/uploads` | Uploaded Lua scripts |

## Operator protocol

The control endpoint is `/api/v1/ws` and requires:

- `Authorization: Bearer TOKEN` for native clients, or a secondary
  `purpcmd.auth.BASE64URL_TOKEN` subprotocol for browser clients;
- WebSocket subprotocol `purpcmd.v1`;
- protocol version `1`;
- a unique request `id` and stable per-process `client_id`.

Requests are typed JSON envelopes named `ask.resource.operation`. Correlated
replies use `rpy.resource.operation`. Asynchronous events use
`evt.resource.operation`. For example:

```text
ask.listener.list   -> rpy.listener.list
ask.task.create     -> rpy.task.create
                     + evt.task.created
                     + evt.task.dispatched
                     + evt.task.completed
```

The task-create reply means that the task was queued, not completed. Request
IDs are persisted with their serialized reply, so retrying the same
`client_id` and request `id` does not perform the mutation twice.

Control messages are limited to 1 MiB. Loot, build artifacts, Lua scripts, and
payload-command attachments use authenticated HTTP endpoints instead of being
embedded in WebSocket JSON. Attachment uploads are limited to 64 MiB, expire
after ten minutes, and are consumed once. In the CLI, prefix a local argument
with `@`, for example:

```text
upload @./local.bin /tmp/remote.bin
memexec @./tool argument
```

## Reconnection and events

Events have a monotonically increasing SQLite sequence. The client sends its
last sequence in `ask.system.hello`; when history is missing it requests
`ask.event.replay` in pages. Duplicate event sequences are discarded.
A fresh snapshot contains listeners, sessions, scripts, profiles, commands, and
the sequence at which it was generated.

A slow WebSocket subscriber is disconnected instead of blocking implant
callbacks or other operators. The client reconnects on the next request and
uses persisted request IDs/replies and event replay to recover safely.

## Resource ownership and persistence

The teamserver owns all long-lived state:

- Listener definitions and running state remain in SQLite.
- Sessions and task history are persisted. After a teamserver restart sessions
  are restored as inactive history because symmetric implant transport keys
  are intentionally not written to SQLite. A fresh registration with the same
  session ID may replace an inactive record.
- Task delivery remains leased, at-least-once delivery.
- Events, request deduplication records, build jobs, profiles, scripts, and loot
  metadata are persisted.
- Build jobs run asynchronously and artifacts are downloaded through an
  authenticated endpoint.
- Each Lua state is serialized. Command execution receives the target session
  explicitly; it never depends on another client's selected session.
- Lua paths are stored canonically. When a database is moved with the checkout,
  startup relocates a missing path ending in script/name.lua to the current
  checkout's script/name.lua and updates the persisted path.

The client owns only presentation state, completion caches, local export paths,
terminal state, and its current menu selections.

## Interactive SSH

`interactive` in a selected session creates a short-lived broker stream and
queues the existing SSH task. The client connects to the authenticated stream;
the implant's reverse WebSocket is paired with it, and the SSH terminal runs in
the client process. Reloading or reconnecting the CLI never starts a new
teamserver or CLI subprocess.

The SSH private key defaults to `template/key/id_ecdsa` and can be changed
with the client `-ssh-key` option. Replace the repository development keys
before any engagement.

## Verification

```sh
make test
make test-race
```

Integration coverage starts the real HTTP/WebSocket handler and verifies bearer
authentication, subprotocol negotiation, request correlation, mutation
deduplication, and event replay. Persistence coverage verifies that session and
task history restore inactive with completed task responses intact.

Headless logging coverage also verifies that listener goroutines can report
status before any CLI prompt exists, and script-path coverage verifies database
relocation between checkouts.
