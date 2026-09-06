# Listener UI implementation contract

This document is the implementation contract for a PurpleCommand listener UI.
It describes protocol version 1 as implemented by the teamserver. Implement the
UI against these wire names and payloads; do not infer names from labels or
invent listener-specific endpoints.

## Non-negotiable protocol facts

- The control transport is a WebSocket at `/api/v1/ws`.
- The selected WebSocket subprotocol must be `purpcmd.v1`.
- Native clients authenticate with `Authorization: Bearer TOKEN`.
- A browser, which cannot set an `Authorization` header on a WebSocket, must
  offer both `purpcmd.v1` and `purpcmd.auth.BASE64URL_TOKEN` as subprotocols.
  `BASE64URL_TOKEN` is the UTF-8 token encoded with unpadded base64url.
- The envelope `version` is the JSON number `1`.
- Requests use `ask.*`, ordinary correlated replies use `rpy.*`, and
  asynchronous events use `evt.*`.
- There are **no** `rpl.listener.*` messages. `rpl.payload-builder.list` is a
  scoped exception elsewhere in the protocol and must not be generalized.
- Each request requires a unique non-empty `id` and a stable non-empty
  `client_id`. A reply is correlated by `id`, not by arrival order.
- Retrying an ambiguously completed request must reuse the same `id` and
  `client_id`. The server persists the serialized reply and deduplicates that
  pair. A new `id` represents a new operation.
- Events and replies share the same socket. An event caused by a request can be
  observed before its correlated reply. The UI must handle either order.

For a browser connection:

```ts
const authProtocol =
  "purpcmd.auth." + base64urlWithoutPadding(new TextEncoder().encode(token));
const socket = new WebSocket(websocketURL + "/api/v1/ws", [
  "purpcmd.v1",
  authProtocol,
]);
```

Convert `http:` to `ws:` and `https:` to `wss:`. After connection,
`socket.protocol` must be `purpcmd.v1`.

## Envelopes

Request:

```json
{
  "version": 1,
  "type": "ask.listener.get",
  "id": "unique-request-id",
  "client_id": "stable-ui-instance-id",
  "time": "2026-09-02T12:00:00Z",
  "data": { "name": "main" }
}
```

Successful reply:

```json
{
  "version": 1,
  "type": "rpy.listener.get",
  "id": "unique-request-id",
  "client_id": "stable-ui-instance-id",
  "time": "2026-09-02T12:00:00Z",
  "ok": true,
  "data": { "name": "main" }
}
```

Failed reply:

```json
{
  "version": 1,
  "type": "rpy.listener.start",
  "id": "unique-request-id",
  "client_id": "stable-ui-instance-id",
  "time": "2026-09-02T12:00:00Z",
  "error": {
    "code": "request_failed",
    "message": "listener is already running"
  }
}
```

The `ok` field is omitted when false. Treat only `ok === true` as success.
Failed replies normally omit `data`. Do not branch on English error text; show
it to the operator, but branch on `error.code` and refreshed server state.

Event:

```json
{
  "version": 1,
  "type": "evt.listener.started",
  "sequence": 42,
  "time": "2026-09-02T12:00:00Z",
  "ok": true,
  "data": { "name": "main", "state": "running" }
}
```

Events do not have a request `id`. Their durable ordering key is `sequence`.

## Complete listener request/reply map

Every request below is available to any authenticated operator. Listener
management currently has no additional admin-only permission check.

| Request | Exact reply | Request `data` | Successful reply `data` | Listener events |
| --- | --- | --- | --- | --- |
| `ask.listener.list` | `rpy.listener.list` | `{}` | `Listener[]`, name-sorted | none |
| `ask.listener.get` | `rpy.listener.get` | `{name}` | `Listener` | none |
| `ask.listener.create` | `rpy.listener.create` | `ListenerCreateRequest` | `Listener` | `created`; optionally start events |
| `ask.listener.update` | `rpy.listener.update` | `ListenerUpdateRequest` | `Listener` | `updated` |
| `ask.listener.start` | `rpy.listener.start` | `{name}` | `Listener` | `starting`, then `started` or `failed` |
| `ask.listener.stop` | `rpy.listener.stop` | `{name}` | `Listener` | normally `stopping`, then `stopped` or `failed` |
| `ask.listener.restart` | `rpy.listener.restart` | `{name}` | `Listener` | stop events when running, then start events |
| `ask.listener.delete` | `rpy.listener.delete` | `{name}` | `{name}` | stop events when running, then `deleted` |
| `ask.listener.hosted` | `rpy.listener.hosted` | `{name}` | `ListenerHostedConfiguration` | none |
| `ask.listener.hosted.set` | `rpy.listener.hosted.set` | `ListenerHostedSetRequest` | `ListenerHostedConfiguration` | `hosted.updated` |
| `ask.listener.hosted.add` | `rpy.listener.hosted.add` | `ListenerHostedAddRequest` | `ListenerHostedConfiguration` | `hosted.updated` |
| `ask.listener.hosted.remove` | `rpy.listener.hosted.remove` | `ListenerHostedRemoveRequest` | `ListenerHostedConfiguration` | `hosted.updated` |
| `ask.listener.hosted.not-found.set` | `rpy.listener.hosted.not-found.set` | `ListenerHostedNotFoundSetRequest` | `ListenerHostedConfiguration` | `hosted.updated` |
| `ask.listener.hosted.not-found.clear` | `rpy.listener.hosted.not-found.clear` | `ListenerHostedNotFoundClearRequest` | `ListenerHostedConfiguration` | `hosted.updated` |
| `ask.listener-type.list` | `rpy.listener-type.list` | `{}` | `ListenerDriverDefinition[]`, ID-sorted | none |
| `ask.listener-type.get` | `rpy.listener-type.get` | `{name}` | `ListenerDriverDefinition` | none |
| `ask.listener-carrier.list` | `rpy.listener-carrier.list` | `{}` | `ListenerCarrierDefinition[]`, ID-sorted | none |

There is no carrier-get request. Cache or index the result of
`ask.listener-carrier.list` by carrier ID.

### Discovery requests

The UI must discover driver and carrier schemas instead of hardcoding a form.
At present the driver list contains only `http`, but the contract is designed
for future HTTPS, DNS, SQL, and other drivers.

```json
{
  "version": 1,
  "type": "ask.listener-type.list",
  "id": "...",
  "client_id": "...",
  "data": {}
}
```

To retrieve one driver definition, send its ID in the generic name request:

```json
{
  "type": "ask.listener-type.get",
  "data": { "name": "http" }
}
```

Driver lookup trims whitespace and lowercases this value. Listener instance
lookup does not; preserve and send the exact listener name returned by the
server.

### Create

```ts
interface ListenerCreateRequest {
  name: string;
  host?: string;                 // legacy compatibility input
  port?: string;                 // legacy compatibility input
  persistent?: boolean;          // defaults to true when omitted
  driver?: string;               // defaults to "http"
  options?: Record<string, unknown>;
  routes?: ListenerRoute[];
  start?: boolean;               // defaults to false
}
```

Preferred request for the existing implant-compatible HTTP listener:

```json
{
  "name": "main",
  "driver": "http",
  "persistent": true,
  "options": {
    "bind": { "host": "0.0.0.0", "port": "4444" }
  },
  "routes": [],
  "start": true
}
```

`name` is trimmed and must be non-empty and unique. `driver` is normalized to
lowercase. If `host` or `port` is supplied, that legacy field overrides the
corresponding `options.bind` value. A new UI should edit `options` directly and
avoid mixing the two forms.

Create emits `evt.listener.created` before any optional start is attempted. If
`start:true` creates the listener but starting it fails, the correlated create
reply is an error while `created`, `starting`, and `failed` events describe the
listener that now exists. Do not roll it back locally. Fetch it or accept the
events and let the operator correct/delete it.

### Update

```ts
interface ListenerUpdateRequest {
  name: string;
  driver?: string;
  options?: Record<string, unknown>;
  routes?: ListenerRoute[];
  persistent?: boolean;
  expected_config_version?: number;
  key?: string;                  // legacy CLI compatibility; do not use
  value?: string;                // legacy CLI compatibility; do not use
}
```

Always send the current non-zero `config_version` as
`expected_config_version`. A stale version fails with `request_failed` and a
message that the configuration changed; fetch the latest listener and present
a merge/retry choice.

Update semantics are replacement semantics:

- omitted `options` retains the current object;
- supplied `options` replaces the entire current object;
- omitted `routes` retains the current list;
- supplied `routes: []` clears explicit routes and selects driver defaults;
- omitted `persistent` retains its current value;
- `name` and `uuid` are immutable;
- the listener must not retain a runtime. In normal UI flow, update only after
  a successful stop or when a failed listener has no retained runtime.

The compatibility `key/value` form only supports `host`, `port`, and `persist`.
It exists for the old CLI and must not be used for a schema-driven UI.

### Hosted-file subresource

Use the dedicated hosted-file operations instead of `ask.listener.update` when
only hosted content is changing. These operations preserve bind, TLS, routes,
global response headers, persistence, desired state, and the current network
runtime. They work while the listener is running and do not stop or rebind it.

`ask.listener.hosted` accepts `{name}`. Every mutation returns the complete
resulting subresource and emits `evt.listener.hosted.updated` with that same
shape:

```ts
interface ListenerHostedConfiguration {
  name: string;
  listener_uuid: string;
  hosted_files: Record<string, HTTPHostedFile>;
  not_found_page: HTTPHostedFile | null;
  config_version: number;
}

interface ListenerHostedSetRequest {
  name: string;
  hosted_files: Record<string, HTTPHostedFile>; // complete replacement
  not_found_page?: HTTPHostedFile;              // omission clears it
  expected_config_version?: number;
}

interface ListenerHostedAddRequest {
  name: string;
  url_path: string;                 // add or replace this exact URL
  file: HTTPHostedFile;
  expected_config_version?: number;
}

interface ListenerHostedRemoveRequest {
  name: string;
  url_path: string;
  expected_config_version?: number;
}

interface ListenerHostedNotFoundSetRequest {
  name: string;
  file: HTTPHostedFile;
  expected_config_version?: number;
}

interface ListenerHostedNotFoundClearRequest {
  name: string;
  expected_config_version?: number;
}
```

Send `expected_config_version` when editing a previously displayed state. A
stale value fails without changing persistence or the active files. Granular
mutations may omit it; the server applies each operation to the latest state
under its listener mutation lock. `set` replaces both hosted collections, so a
UI should always use version checking for it.

For a running listener, the server validates and opens the entire candidate
file set before committing it. The swap is atomic for new HTTP requests;
downloads already in progress finish against the prior file descriptors. A
failure leaves the persisted configuration, version, and active runtime
unchanged.

### Start, stop, restart, and delete

All four use `{ "name": "exact-listener-name" }`.

- Start changes desired state to `running`, emits `starting`, starts the driver,
  and emits `started`. A driver error emits `failed` and returns a failed reply.
- Stop changes desired state to `stopped`, emits `stopping`, shuts down the
  runtime, and emits `stopped`. If shutdown fails, it emits `failed`, returns a
  failed reply, and retains the runtime so stop/delete can retry safely.
- Restart of a running listener performs the complete stop sequence followed
  by the complete start sequence. Restart of a stopped listener only starts.
- Delete of a running listener first completes the stop sequence. It then
  deletes persistence and in-memory state, emits `deleted`, and replies with
  `{name}`. If stopping or database deletion fails, the reply fails and no
  deleted event is emitted.
- A successful delete closes the callback endpoint and active implant
  interactive WebSockets before replying.

Do not optimistically remove a listener on button click. Remove it only after a
successful delete reply or `evt.listener.deleted`.

## Complete listener event map

| Event | `data` | Required UI action |
| --- | --- | --- |
| `evt.listener.created` | full `Listener` | insert/replace |
| `evt.listener.updated` | full `Listener` | replace |
| `evt.listener.starting` | full `Listener`, `state:"starting"` | replace; show transition |
| `evt.listener.started` | full `Listener`, `state:"running"` | replace |
| `evt.listener.stopping` | full `Listener`, `state:"stopping"` | replace; show transition |
| `evt.listener.stopped` | full `Listener`, `state:"stopped"` | replace |
| `evt.listener.failed` | full `Listener`, `state:"failed"` | replace; prominently show `last_error` |
| `evt.listener.deleted` | `{name:string}` only | remove by exact name |
| `evt.listener.hosted.updated` | full `ListenerHostedConfiguration` | replace the hosted-file subresource and update its version |

All nine event types are persisted. The standard listener events are retained
for seven days by default; failed events are retained for thirty days.

Events are authoritative snapshots for their sequence, but fields such as
`associations` can change later as sessions register. Reconcile with a fresh
listener list/snapshot when accuracy matters.

## Public data types

JSON raw-message fields below are JSON objects on the wire, not JSON strings.

```ts
type ListenerState =
  | "stopped"
  | "starting"
  | "running"
  | "stopping"
  | "failed";

interface Listener {
  name: string;
  uuid: string;
  host: string;                  // configured bind host compatibility field
  port: string;                  // configured bind port compatibility field
  running: boolean;              // true exactly when state === "running"
  persistent: boolean;
  associations: number;         // sessions currently associated with listener
  driver?: string;
  options?: Record<string, unknown>;
  routes?: ListenerRoute[];      // absent/empty means driver-default routes
  state?: ListenerState;
  desired_state?: "stopped" | "running";
  address?: string;              // actual bound runtime address while running
  last_error?: string;
  config_version?: number;
}

interface ListenerCarrierSpec {
  type: string;
  options?: Record<string, unknown>;
}

interface ListenerRoute {
  id: string;
  purpose: "callback" | "interactive";
  priority?: number;
  match?: Record<string, unknown>;
  options?: Record<string, unknown>;
  inbound: ListenerCarrierSpec;
  outbound: ListenerCarrierSpec;
}

type ListenerOptionType =
  | "string"
  | "integer"
  | "boolean"
  | "duration"
  | "string_list"
  | "string_map"
  | "object"
  | "file"
  | "secret";

interface ListenerOptionDefinition {
  key: string;                   // dotted path, for example "bind.host"
  type: ListenerOptionType;
  description?: string;
  required?: boolean;
  secret?: boolean;
  mutable_while_running?: boolean;
  default?: unknown;
}

interface ListenerDriverDefinition {
  id: string;
  description?: string;
  capabilities?: string[];
  options?: ListenerOptionDefinition[];
}

interface ListenerCarrierDefinition {
  id: string;
  description?: string;
  options?: ListenerOptionDefinition[];
}

interface HTTPHostedFile {
  source_path: string;            // beneath the teamserver hosted-file root
  status?: number;                // defaults to 200 for an exact hosted URL
  headers?: Record<string, string>;
}

interface ListenerHostedConfiguration {
  name: string;
  listener_uuid: string;
  hosted_files: Record<string, HTTPHostedFile>;
  not_found_page: HTTPHostedFile | null;
  config_version: number;
}
```

Treat omitted optional fields as their zero/default state. Never use
`running` as the only state representation: `starting`, `stopping`, and
`failed` all have `running:false`.

## HTTP driver schema

The discovery API is the source of truth for the top-level option form. The
currently implemented HTTP object is:

```json
{
  "bind": { "host": "0.0.0.0", "port": "4444" },
  "advertise": { "host": "example.test", "port": "4444" },
  "tls": {
    "enabled": false,
    "cert_file": "/server/path/listener.crt",
    "key_file": "/server/path/listener.key"
  },
  "response_headers": { "X-Header": "value" },
  "hosted_files": {
    "/": {
      "source_path": "site/index.html",
      "status": 200,
      "headers": { "Content-Type": "text/html; charset=utf-8" }
    },
    "/assets/app.js": {
      "source_path": "site/app.js",
      "headers": { "Content-Type": "application/javascript" }
    }
  },
  "not_found_page": {
    "source_path": "site/404.html",
    "headers": { "Content-Type": "text/html; charset=utf-8" }
  },
  "timeouts": {
    "read_header": "5s",
    "read": "30s",
    "write": "30s",
    "idle": "60s"
  },
  "max_body_bytes": 10485760,
  "max_header_bytes": 32768
}
```

Important validation rules:

- port values are strings containing a number from `0` through `65535`;
- bind defaults to `0.0.0.0:4444`; port `0` requests an ephemeral port;
- advertise defaults to bind, but payload-builder endpoint export is not yet
  integrated, so do not imply that editing advertise rebuilds an implant;
- all duration values use Go duration syntax and must be positive;
- body size is 1 through 67,108,864 bytes;
- header size is 1,024 through 1,048,576 bytes;
- enabling TLS requires both certificate and key paths; supplying either path
  while TLS is disabled is invalid;
- TLS paths are paths on the teamserver host, not browser-uploaded files;
- response headers reject invalid field names, control characters, duplicates,
  and server-controlled hop-by-hop or framing headers such as
  `Content-Length`, `Connection`, and `Upgrade`;
- `hosted_files` is an object keyed by exact, canonical URL paths; a listener
  may define at most 256 entries;
- hosted `source_path` values are teamserver paths beneath `-hosted-dir`, not
  paths on the browser or operator client;
- hosted sources must exist and be regular files when the listener starts;
- hosted statuses default to `200` and must be final statuses that permit a
  body; `204`, `205`, and `304` are rejected;
- `not_found_page`, when present, has the same source/header shape but its
  status is fixed to `404`;
- unknown JSON fields are rejected.

### Hosted files and fallback order

Hosted files are binary-safe and support HTML, CSS, JavaScript, JSON, images,
and payload downloads. The teamserver streams an opened file descriptor, sets
`Content-Length`, and infers `Content-Type` only when neither listener-global
nor hosted-file headers provide it. `HEAD` returns the same status and headers
without the body.

Request handling uses this order:

1. A valid callback or interactive WebSocket route wins.
2. If a callback route matched but the carrier or encrypted implant packet is
   malformed, an exact `hosted_files` entry for that URL is returned.
3. An exact hosted URL is returned directly for `GET` or `HEAD` when no
   callback route claims it. This also allows ordinary hosted image requests
   to coexist with the default interactive image route.
4. `not_found_page` is returned for every method when no configured/default
   route or exact hosted URL matches.
5. Without a configured response, the existing `400`, `404`, or `405` response
   is retained.

Body-limit failures and non-protocol callback failures never fall through to a
hosted response. A valid callback with no task also remains a valid callback;
it does not receive the decoy file. Use the hosted-file subresource API to
change mappings, headers, and statuses without stopping a running listener.
Initial source paths are opened at listener startup; live changes are opened
and validated before the active set is swapped.

### HTTP routes

Callback route example:

```json
{
  "id": "custom-callback",
  "purpose": "callback",
  "priority": 50,
  "match": {
    "path": "/pixel.png",
    "methods": ["POST"],
    "host": "example.test",
    "extensions": [".png"]
  },
  "options": {
    "session": { "source": "header", "name": "X-Session" },
    "response": {
      "status": 202,
      "nil_status": 404,
      "headers": { "X-Route": "value" },
      "no_task_body": "Hi!"
    }
  },
  "inbound": { "type": "header", "options": { "name": "X-Callback" } },
  "outbound": { "type": "image", "options": {} }
}
```

Route rules:

- route IDs must be non-empty and unique within a listener;
- supported purposes are `callback` and `interactive`;
- higher priority matches first; equal priority preserves array order;
- `match.path` and `match.path_prefix` are mutually exclusive and must start
  with `/` when present;
- methods are normalized to uppercase;
- extensions are normalized to lowercase with a leading dot;
- `session.source`, when set, is `query`, `header`, or `cookie` and requires a
  name;
- response status values, when set, are 100 through 599;
- `query` cannot be an outbound response carrier;
- unknown match, route-option, carrier-option, and driver-option fields are
  rejected.

Interactive routes use `options.stream_query` to select the query parameter
that contains the interactive stream ID. It defaults to `stream`.

### Carriers

| ID | Options | Direction notes |
| --- | --- | --- |
| `body` | `{}` | inbound or outbound |
| `cookie` | `{name:string}` | inbound or outbound |
| `header` | `{name:string}` | inbound or outbound |
| `query` | `{name:string}` | inbound request only; invalid as an outbound response carrier |
| `image` | `{template_base64?:string}` | inbound or outbound PNG container; template maximum 4 MiB |

Render carrier-specific fields from `ask.listener-carrier.list`. Header and
cookie names must be valid HTTP tokens. The image template must decode to a PNG.

### Existing implant-compatible defaults

When `routes` is omitted or empty, the driver installs these runtime defaults:

1. interactive `GET` for paths ending `.png`, `.jpg`, or `.gif`, using query
   parameter `stream`;
2. callback `GET /`, reading callback data from cookie `a` and session identity
   from query parameter `a`;
3. callback `POST /`, reading callback data from the body and session identity
   from query parameter `a`;
4. callback responses in the body; no-task body `Hi!`.

This empty-route, non-TLS default is the profile understood by the existing
implant. The server can accept custom paths, headers, TLS, and carriers, but a
built implant is not yet generated from those settings. The UI must warn that
custom callback placement requires a matching custom implant; do not present
server-only customization as automatically implant-compatible.

## State and reducer rules

Use `uuid` as the stable entity key and retain a name index because the delete
event contains only `name`.

Recommended reducer behavior:

```ts
function applyListenerMessage(state: Store, envelope: Envelope): Store {
  switch (envelope.type) {
    case "evt.listener.created":
    case "evt.listener.updated":
    case "evt.listener.starting":
    case "evt.listener.started":
    case "evt.listener.stopping":
    case "evt.listener.stopped":
    case "evt.listener.failed":
      return upsertListener(state, envelope.data as Listener);
    case "evt.listener.deleted":
      return removeListenerByName(state, (envelope.data as {name: string}).name);
    default:
      return state;
  }
}
```

Also upsert successful create/get/update/start/stop/restart replies. A successful
delete reply may remove by name; a later duplicate delete event must be
idempotent.

Suggested action availability:

| State | Start | Stop | Restart | Update | Hosted files | Delete |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `stopped` | yes | no | yes | yes | yes | yes |
| `starting` | no | no | no | no | after transition | no |
| `running` | no | yes | yes | no | yes | yes |
| `stopping` | no | no | no | no | after transition | no |
| `failed` | generally yes | context-dependent | yes | server decides | yes | yes |

This table is a UI convenience, not authorization. The server remains
authoritative. In particular, a failed stop can retain a runtime; update/start
will be rejected until a later stop/delete successfully disposes of it.

## Initial synchronization and reconnect

Listener UIs also need these non-listener requests:

| Request | Reply | Purpose |
| --- | --- | --- |
| `ask.system.snapshot` | `rpy.system.snapshot` | full state including `listeners` and `event_sequence` |
| `ask.system.hello` | `rpy.system.hello` | server identity, latest sequence, and resync indication |
| `ask.event.replay` | `rpy.event.replay` | durable events after a sequence |
| `ask.event.ack` | `rpy.event.ack` | currently advisory; returns `{acknowledged:true}` |

Safe initial-load algorithm:

1. Open the socket and begin buffering events immediately.
2. Request `ask.system.snapshot` with `{}`.
3. Replace local listener state with `snapshot.listeners`.
4. Record `snapshot.event_sequence`.
5. Apply buffered/live events whose sequence is greater than that value, in
   ascending sequence order. Ignore duplicate or older sequences.

Safe reconnect algorithm:

1. Preserve the last continuously applied sequence.
2. Reconnect and buffer live events.
3. Send `ask.system.hello` with
   `{ "last_event_sequence": LAST_SEQUENCE }`.
4. If `resync_required` is true, request pages with
   `{ "after": CURSOR, "limit": 1000 }` using `ask.event.replay`.
5. Apply records in sequence order and advance the cursor.
6. If replay returns no records before the advertised latest sequence, or a
   sequence gap cannot be filled, request a fresh system snapshot.
7. Merge buffered live events above the resulting cursor.

Do not assume an event channel is lossless. Detect sequence gaps and recover.
The sequence is global, so advance the cursor for every event even when its
type is unrelated to listeners and its payload is ignored by the listener
reducer.
Acknowledgement currently does not change retention or delivery and is not
required for listener correctness.

## Error contract

Listener validation, not-found, lifecycle, persistence, version-conflict, and
driver errors are returned as:

```json
{
  "code": "request_failed",
  "message": "human-readable server detail"
}
```

Envelope/control errors can additionally use:

| Code | Meaning |
| --- | --- |
| `bad_envelope` | invalid JSON envelope or missing request ID/client ID |
| `bad_operation` | type is not a supported `ask.*` operation |
| `version_mismatch` | envelope version is not 1 |
| `encode_failed` | server could not serialize a reply |
| `forbidden` | authenticated principal lacks permission; not currently used for listener operations |

Request DTO decoding rejects unknown fields. This is intentional: misspelled
form keys must produce a visible error rather than being silently ignored.

On any ambiguous network failure, retry once with the same request identity.
On an explicit failed reply, do not blindly retry a mutation. Apply any events,
then fetch the listener/list because create-with-start and lifecycle failures
can leave valid changed state.

## UI acceptance checklist

The listener UI is complete only when all of the following are demonstrated:

- browser WebSocket authentication and `purpcmd.v1` negotiation work for both
  `ws` and `wss` deployments;
- all eleven listener request/reply pairs in this document are implemented
  using their exact names;
- all nine listener events are decoded, reduced, and sequence-deduplicated;
- list, get, create, update, start, stop, restart, delete, and all hosted-file
  operations refresh the UI
  from reply/event snapshots rather than guessed local state;
- driver and carrier forms come from discovery replies;
- `false`, `0`, empty arrays, and empty objects are preserved when explicitly
  selected instead of being dropped as falsy values;
- update sends `expected_config_version` and handles a conflict by refetching;
- create-with-start failure keeps the created failed listener visible;
- failed start displays `last_error` and desired state;
- delete-while-running waits for shutdown and removes only on success/event;
- reconnect replays by sequence or falls back to a snapshot on a gap;
- the default HTTP form can create and start an empty-route listener compatible
  with the existing implant;
- the UI clearly labels custom routes/carriers/TLS as requiring matching implant
  configuration until listener endpoint snapshots are integrated with builds.

## Verified source locations

- Wire constants and envelope behavior: `pkg/teamapi/api.go`
- Public listener DTOs: `pkg/teamapi/listener.go`
- Request dispatch: `teamserver/server/eventHandler.go`
- WebSocket authentication/correlation/deduplication: `teamserver/server/server.go`
- DTO conversion and event mapping: `server/listener/teamapi.go`
- Lifecycle and state transitions: `server/listener/manager.go`
- HTTP options, routes, and default compatibility: `server/listener/http_driver.go`
- Carrier behavior: `server/listener/carriers.go`
