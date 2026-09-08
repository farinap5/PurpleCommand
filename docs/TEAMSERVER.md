# PurpleCommand teamserver/client architecture

PurpleCommand is split into two processes:

- `purpcmd-teamserver` owns listeners, implant callbacks, sessions, tasks,
  Lua states, loot, build jobs, SQLite, and the implant RSA key. It is intended
  to remain running when no operator is connected.
- `purpc` is the transient operator CLI. Menu state and selected listener,
  session, or profile are local to that client and disappear when it exits.

The implant-facing binary and encrypted task frames remain shared and
unchanged across transports. Reverse implants initiate requests to listeners;
for bind implants, a teamserver speaker initiates requests to the payload. The
complete frame and speaker HTTP-carriage contract is documented in
[`IMPLANT_PROTOCOL.md`](IMPLANT_PROTOCOL.md). The operator boundary uses a
separate authenticated HTTP/WebSocket API.

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
only for controlled testing. The startup bearer token is the admin credential.
The admin can create additional operator users, refresh their tokens, and
delete them through the control API.

Teamserver flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `-listen` | `127.0.0.1:8080` | Operator API address |
| `-token` | `PURPCMD_TOKEN` | Admin bearer token |
| `-tls-cert`, `-tls-key` | empty | TLS files; required off loopback |
| `-rsa-key` | `server.key` | Implant registration private key |
| `-database` | `database.db` | SQLite state |
| `-loot-dir` | `loot` | Loot files |
| `-script-dir` | `script/uploads` | Uploaded Lua scripts |
| `-hosted-dir` | `hosted` | Files available to implant-facing HTTP listener hosting |

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
ask.listener.delete -> rpy.listener.delete + evt.listener.deleted
ask.task.create     -> rpy.task.create
                     + evt.task.created
                     + evt.task.dispatched
                     + evt.task.completed
```

The payload-builder registry has an explicit listing operation. Its reply uses
the requested `rpl` prefix as a scoped protocol exception:

```text
ask.payload-builder.list -> rpl.payload-builder.list
```

The reply data is a name-sorted array of `{name, description, source}` objects
for builders registered by currently loaded Lua scripts.

The complete listener UI wire contract, including every listener request,
reply, event, DTO, state transition, HTTP option, route, carrier, and reconnect
rule, is documented in [`LISTENER_UI_CONTRACT.md`](LISTENER_UI_CONTRACT.md).

Builder registration and build jobs use the following complete lifecycle:

```text
evt.payload-builder.registered
evt.payload-builder.unregistered

ask.build.create {"profile":"linux-impl","builder":"implant-builder-linux-amd64"}
                 -> rpy.build.create (status=queued)
                   + evt.build.queued
                   + evt.build.started
                   + evt.build.output      (zero or more)
                   + evt.build.completed   OR evt.build.failed

ask.build.get    -> rpy.build.get
ask.build.list   -> rpy.build.list
ask.build.delete -> rpy.build.delete + evt.build.deleted
```

Queued, started, completed, failed, and deleted event data is a `Build` object.
The `builder` request field is optional; when omitted, the profile's persisted
builder is snapshotted into the job. An explicit builder overrides that job
only, must currently be registered, and does not mutate the profile.
Output event data is `{build_id, profile, builder, message}`, so every message
can be correlated to its job. Makefile, built-in Go, and Lua commands all use
the same output event. At most 256 KiB is retained per command; the full output
still appears on the teamserver console and retained output is marked when
truncated. Jobs remain `queued` until they acquire the serialized build slot.

The task-create reply means that the task was queued, not completed. Request
IDs are persisted with their serialized reply, so retrying the same
`client_id` and request `id` does not perform the mutation twice.

### Operator users

User management uses these control operations:

```text
ask.user.list
ask.user.create  {"name":"alice"}
ask.user.update  {"name":"alice"}
ask.user.delete  {"name":"alice"}
ask.user.message {"message":"hello team"}
```

Create and update return a newly generated token. The token is returned only
in that operation's reply; list and snapshot responses do not expose tokens.
Only the startup admin credential can perform create, update, or delete. User
tokens otherwise authenticate the same operator API. Updating a user rotates
the token and disconnects that user's existing WebSockets; deleting a user
also disconnects them immediately. A user's `connected` field is true while
at least one authenticated control WebSocket for that user is open.

Any authenticated user can publish `ask.user.message`. The server derives the
sender from the authenticated WebSocket and broadcasts an `evt.user.message`
event to all connected users. The CLI command is `message <text>`.

Control messages are limited to 1 MiB. Loot, build artifacts, Lua scripts, and
payload-command attachments use authenticated HTTP endpoints instead of being
embedded in WebSocket JSON. Attachment uploads are limited to 64 MiB, expire
after ten minutes, and are consumed once. In the CLI, prefix a local argument
with `@`, for example:

```text
upload @./local.bin /tmp/remote.bin
memexec @./tool argument
```

### HTTP listener file hosting

HTTP listeners can map exact URL paths to server-local files through the
`hosted_files` listener option. They can also define `not_found_page`, which
returns a custom body with status `404` whenever no listener route or hosted
URL matches. Files are resolved beneath `-hosted-dir`, must be regular files,
and are opened when the listener starts. This boundary prevents an operator
configuration mistake from publishing arbitrary teamserver files.

Malformed implant traffic may fall back to a hosted file at the same URL, but
valid implant traffic always takes precedence. Body-limit and internal
processing failures are not hidden behind a decoy response. Configuration is
persisted as part of the owning listener's options, so no separate hosted-file
database lifecycle exists. Dedicated `ask.listener.hosted*` operations can
atomically change the active hosted set without stopping or rebinding a running
listener.

The hosted-file control API consists of:

```text
ask.listener.hosted
ask.listener.hosted.set
ask.listener.hosted.add
ask.listener.hosted.remove
ask.listener.hosted.not-found.set
ask.listener.hosted.not-found.clear
evt.listener.hosted.updated
```

Every mutation returns and broadcasts the complete resulting hosted-file
configuration, including its listener configuration version. Candidate files
are validated and opened before a live change is persisted or activated.

The CLI manages these values in listener mode:

```text
host list
host add /index.html site/index.html 200 {"Content-Type":"text/html"}
host remove /index.html
host not-found site/404.html {"Content-Type":"text/html"}
host clear-not-found
```

All source paths in these commands refer to the teamserver's hosted-file
directory, not to the machine running the CLI.

## Reconnection and events

Events have a monotonically increasing SQLite sequence. The client sends its
last sequence in `ask.system.hello`; when history is missing it requests
`ask.event.replay` in pages. Duplicate event sequences are discarded.
A fresh snapshot contains listeners, sessions, scripts, profiles, commands,
users, and the sequence at which it was generated.

A slow WebSocket subscriber is disconnected instead of blocking implant
callbacks or other operators. The client reconnects on the next request and
uses persisted request IDs/replies and event replay to recover safely.

### Event retention

The teamserver prunes expired event-replay records at startup and once per
hour. Policies live in the SQLite `EventRetentionConfiguration` table, one row
per event type. `RetentionSeconds` is authoritative; `RetentionTier` is a
human-readable grouping. Setting `RetentionSeconds` to `0` disables expiration
for that event type. Unconfigured event types are retained so a newly added or
extension event is never deleted before it has an explicit policy.

Default policies are:

| Tier | Retention | Event classes |
| --- | ---: | --- |
| `short` | 24 hours | Session check-ins |
| `standard` | 7 days | Reconstructible listener, session, task, loot, script, build, and speaker lifecycle notifications |
| `important` | 30 days | Failures, completions, and user audit events |
| `archive` | 90 days | User messages and session, script, or build output |

Configuration changes survive restarts because schema initialization uses
`INSERT OR IGNORE` for defaults. For example, this keeps user messages for one
year:

```sql
UPDATE EventRetentionConfiguration
SET RetentionTier = 'archive',
    RetentionSeconds = 31536000,
    UpdatedAt = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE EventType = 'evt.user.message';
```

Cleanup uses indexed batches of 1,000 rows per transaction. Deleting records
makes their SQLite pages reusable but does not immediately shrink the database
file; file compaction should be performed separately during maintenance.
Event sequence watermarks remain monotonic even if every retained row expires.
When a client detects a gap caused by retention, `HelloReply.HistoryTruncated`
is set locally and the CLI's normal post-hello snapshot restores current state.

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
- Profiles may opt into a registered Lua `payload_build` handler. Profiles with
  no builder retain the existing Makefile or built-in Go build path. Lua
  `os.write` uses a caller-selected path and creates its missing parent
  directories; it does not allocate or remove a random build workspace.
- Each Lua state is serialized. Command execution receives the target session
  explicitly; it never depends on another client's selected session. Within a
  command or lifecycle/task callback, `session()` returns that invocation's
  session metadata; `session(id)` retains explicit lookup.
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
