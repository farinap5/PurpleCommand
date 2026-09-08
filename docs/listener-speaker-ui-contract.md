# Listener and Speaker UI Contract

Status: implemented contract  
Team API version: `1`  
Control transport: WebSocket `purpcmd.v1`

This document is the frontend contract for managing listeners, speakers, and
their session associations. “Must” describes behavior required for a UI to
interoperate safely with the current teamserver.

## 1. Concepts and direction of communication

| Property | Listener | Speaker |
|---|---|---|
| Network role | Teamserver accepts inbound connections | Teamserver makes outbound HTTP requests |
| Implant role | Implant initiates callbacks | Bind implant accepts teamserver requests |
| First blood | Arrives asynchronously after the listener starts | Performed synchronously during `speaker.start` |
| Successful start means | The listening socket is ready | Registration was fetched, decrypted, and associated with the speaker |
| Command delivery | Returned in the implant's next callback response | Teamserver performs a health/check exchange and then a task request |
| Health behavior | Implant callback cadence is based on implant `sleep` | Optional teamserver-driven health requests; task-triggered checks remain enabled |
| Connection meaning | A running inbound server exists | No permanent command channel is implied; each exchange is a finite HTTP request |
| Typical fan-out | One listener may accept many sessions | One worker targets one bind endpoint; historical associations can remain |
| Configuration model | Driver-defined `options` and routes | Statically typed outbound HTTP client and request template |
| Dynamic form schema | Yes, through listener driver/carrier definition APIs | No; use the speaker types in this document |
| Update while active | General configuration requires stop; hosted files have separate live-mutation APIs | Configuration requires stop |
| Delete while active | Delete stops the listener first | Delete is rejected until the speaker is stopped |
| Rename | Not supported by listener update | Supported through `new_name`; UUID and session ownership remain stable |
| Mutation permission | Any authenticated client under the current server contract | Admin only |

Listener and speaker transports carry the same encrypted PurpleCommand protocol
frames. The UI never constructs `REG`, `CHK`, `TASK`, `RSP`, or `CHU` frames.

## 2. Control-plane envelope

Connect to:

```text
GET /api/v1/ws
Authorization: Bearer <operator-token>
Sec-WebSocket-Protocol: purpcmd.v1
```

Every request is a text-frame JSON envelope:

```json
{
  "version": 1,
  "type": "ask.speaker.list",
  "id": "unique-request-id",
  "client_id": "stable-ui-instance-id",
  "data": {}
}
```

- `version`, `type`, `id`, and `client_id` are required.
- `id` must be unique per logical operation. Retrying the same logical request
  must reuse the same `id`; the server deduplicates by authenticated principal,
  `client_id`, and `id`.
- Keep `client_id` stable for the lifetime of the UI installation or browser
  client identity.
- Replies use `rpy.<suffix>`. For example, `ask.speaker.start` returns
  `rpy.speaker.start` with the same `id` and `client_id`.
- Events use `evt.*`, have a monotonically increasing `sequence`, and do not
  correlate to a request ID.
- The maximum control message is 1 MiB.
- Unknown fields in request `data`, including nested speaker fields, are
  rejected.

Successful reply:

```json
{
  "version": 1,
  "type": "rpy.speaker.list",
  "id": "unique-request-id",
  "client_id": "stable-ui-instance-id",
  "time": "2026-09-06T12:00:00Z",
  "ok": true,
  "data": []
}
```

Failed reply:

```json
{
  "version": 1,
  "type": "rpy.speaker.start",
  "id": "unique-request-id",
  "client_id": "stable-ui-instance-id",
  "time": "2026-09-06T12:00:00Z",
  "ok": false,
  "error": {
    "code": "request_failed",
    "message": "speaker first blood request: ..."
  }
}
```

Relevant error codes are:

| Code | Meaning |
|---|---|
| `bad_envelope` | Invalid JSON or missing envelope identity |
| `bad_operation` | Unknown/non-request operation |
| `version_mismatch` | `version` is not `1` |
| `forbidden` | Authenticated user lacks permission; speaker mutations are admin-only |
| `request_failed` | Validation, state, version, network, or persistence failure |
| `encode_failed` | Server could not encode the reply |

The UI must display `error.message`; it must not infer a successful mutation
from an event or an HTTP/WebSocket write alone.

## 3. Initial synchronization and events

The system snapshot includes both resources:

```ts
interface Snapshot {
  listeners: Listener[];
  speakers: Speaker[];
  sessions: Session[];
  event_sequence: number;
  // Other existing snapshot collections are omitted here.
}
```

Recommended synchronization:

1. Connect and start accepting events.
2. Send `ask.system.hello` with the last durable event sequence.
3. Fetch `ask.system.snapshot` on initial load or when `resync_required` is true.
4. Atomically replace local collections with the snapshot and set the local
   cursor to `snapshot.event_sequence`.
5. Apply only events whose sequence is greater than the local cursor.
6. On reconnect, use `ask.event.replay` or obtain another snapshot if history
   is unavailable.

Speaker events:

```text
evt.speaker.created
evt.speaker.updated
evt.speaker.connecting
evt.speaker.connected
evt.speaker.disconnected
evt.speaker.failed
evt.speaker.stopped
evt.speaker.deleted
```

All speaker events except `evt.speaker.deleted` contain a complete, redacted
`Speaker`. The delete event contains:

```json
{"name":"edge-speaker","uuid":"stable-speaker-uuid"}
```

Listener events:

```text
evt.listener.created
evt.listener.updated
evt.listener.starting
evt.listener.started
evt.listener.stopping
evt.listener.stopped
evt.listener.failed
evt.listener.deleted
evt.listener.hosted.updated
```

Listener lifecycle events except delete contain a complete `Listener`. Delete
contains `{"name":"listener-name"}`. The hosted event contains a
`ListenerHostedConfiguration`.

For create/update/start/stop/restart, use the successful reply as the immediate
authoritative value and also process events. Resource UUID is the stable store
key. Names are operation selectors and display labels.

## 4. Speaker resource

```ts
type SpeakerState =
  | "stopped"
  | "connecting"
  | "connected"
  | "disconnected"
  | "failed";

type DesiredState = "stopped" | "running";

interface Speaker {
  name: string;
  uuid: string;
  running: boolean;
  in_flight: boolean;
  persistent: boolean;
  state: SpeakerState;
  desired_state: DesiredState;
  config_version: number;
  associations: number;
  session?: string;
  last_attempt_at?: string;
  last_success_at?: string;
  last_error?: string;
  config: SpeakerConfig;
}
```

Field semantics:

- `uuid` is immutable and must be used for stable list keys and association
  reconciliation.
- `state` is the authoritative current lifecycle state.
- `desired_state` is persisted intent. It may remain `running` after a failed
  start so the failure is visible and a persistent speaker can be reconciled.
- `running` means a worker is active or transitioning. A `disconnected`
  speaker can still have `running: true`; there is no persistent socket.
- `in_flight` means one outbound request is currently active.
- `session` is the numeric session name learned during first blood.
- `associations` counts sessions owned by the stable speaker UUID.
- `last_attempt_at` changes when an HTTP exchange begins.
- `last_success_at` changes after a valid protocol exchange.
- `last_error` is cleared by the next successful exchange.
- `config_version` is an optimistic-concurrency token and can change on
  lifecycle operations because desired state is persisted.
- `persistent: false` means the speaker will not survive a teamserver restart.

### 4.1 Speaker state transitions

```text
create -> stopped
stopped --start--> connecting --first blood succeeds--> connected
connecting --first blood fails--> failed
connected --exchange fails--> disconnected
disconnected --exchange succeeds--> connected
connected/disconnected --stop--> stopped
stopped/failed --restart--> connecting
```

Important UI behavior:

- `speaker.start` is synchronous with first blood. Do not show start as
  successful until the reply succeeds.
- A failed start can still increment `config_version`; refetch the speaker or
  consume `evt.speaker.failed` before another update.
- `connected` means the most recent protocol exchange succeeded, not that a
  socket is permanently open.
- A transient request error produces `disconnected`. The worker continues and
  can recover to `connected`.
- Stop is valid for an active or failed speaker. Delete is valid only after the
  worker is stopped.

Recommended controls:

| State | Primary controls |
|---|---|
| `stopped` | Start, edit, delete |
| `connecting` | Stop; disable edit/delete/start |
| `connected` | Stop, restart; disable edit/delete/start |
| `disconnected` | Stop, restart; show `last_error` |
| `failed` | Stop to clear desired state, restart, or edit after stopped |

Always tolerate `request_failed`, because another operator can change state
between rendering and clicking.

### 4.2 Speaker configuration

```ts
interface SpeakerConfig {
  profile?: string;
  client: SpeakerHTTPClientConfig;
  request: SpeakerHTTPRequestConfig;
  healthcheck?: SpeakerHealthcheckConfig;
  retry?: SpeakerRetryConfig;
}

interface SpeakerHealthcheckConfig {
  enabled?: boolean;
  interval?: number;          // nanoseconds
  failure_threshold?: number;
}

interface SpeakerRetryConfig {
  interval?: number;          // nanoseconds
}

interface SpeakerHTTPClientConfig {
  base_url: string;
  host?: string;
  headers?: Record<string, string[]>;
  query?: Record<string, string[]>;
  cookies?: Record<string, string>;

  proxy_url?: string;
  use_environment_proxy?: boolean;
  follow_redirects?: boolean;
  allow_cross_origin_redirects?: boolean;
  max_redirects?: number;

  request_timeout?: number;          // nanoseconds
  dial_timeout?: number;             // nanoseconds
  tls_handshake_timeout?: number;    // nanoseconds
  response_header_timeout?: number;  // nanoseconds
  idle_connection_timeout?: number;  // nanoseconds
  max_request_bytes?: number;
  max_response_bytes?: number;
  max_response_header_bytes?: number;
  max_idle_connections?: number;
  max_idle_per_host?: number;
  disable_compression?: boolean;
  reuse_connections?: boolean;
  tls?: SpeakerTLSConfig;
}

interface SpeakerTLSConfig {
  server_name?: string;
  root_ca_file?: string;
  client_cert_file?: string;
  client_key_file?: string;
  spki_sha256_pins?: string[];
  min_version?: "1.2" | "TLS1.2" | "tls1.2" |
                "1.3" | "TLS1.3" | "tls1.3";
  insecure_skip_verify?: boolean;
}

interface SpeakerHTTPRequestConfig {
  method?: string;
  path?: string;
  host?: string;
  headers?: Record<string, string[]>;
  query?: Record<string, string[]>;
  cookies?: Record<string, string>;
  expected_status?: number[];
}
```

Go `time.Duration` values are JSON integers in nanoseconds. The UI should edit
human units and convert at the API boundary. Examples:

```text
100 ms = 100000000
10 s   = 10000000000
30 s   = 30000000000
```

Operational defaults when a value is absent or zero:

| Field | Default |
|---|---|
| `healthcheck.enabled` | `true` |
| `healthcheck.interval` | 30 seconds |
| `healthcheck.failure_threshold` | 3 |
| `retry.interval` | 15 seconds |
| `client.request_timeout` | 30 seconds |
| `client.dial_timeout` | 10 seconds |
| `client.tls_handshake_timeout` | 10 seconds |
| `client.response_header_timeout` | 15 seconds |
| `client.idle_connection_timeout` | 90 seconds |
| `client.max_request_bytes` | 11190488 bytes, the shared maximum protocol envelope |
| `client.max_response_bytes` | 11190488 bytes |
| `client.max_response_header_bytes` | 65536 bytes |
| `client.max_idle_connections` | 100 |
| `client.max_idle_per_host` | 10 |
| `client.max_redirects` | 5 when redirects are enabled |
| `client.follow_redirects` | `false` |
| `client.allow_cross_origin_redirects` | `false` |
| `client.use_environment_proxy` | `false` |
| `client.reuse_connections` | `false` |
| `client.tls.min_version` | TLS 1.2 |
| `request.expected_status` | Any 2xx status |

Validation and UI constraints:

- Speaker name must match `[A-Za-z0-9_.-]{1,64}`.
- `client.base_url` is required, must use `http` or `https`, must contain a
  host, and cannot contain user information or a fragment.
- The standard bind implant accepts `POST`. The UI must default `method` to
  `POST`; leaving it empty would make the generic HTTP engine use `GET`, which
  the implant rejects.
- `path` must be relative to the configured origin. It cannot be an absolute
  URL, supply user information, or contain a fragment. Use `/` for the default
  bind payload.
- `Host` cannot be supplied through headers; use the dedicated `host` field.
- Header names and cookie names must be valid HTTP tokens. Values cannot
  contain control characters; cookie values also reject `"`, `;`, and `\`.
- Request-level headers, query values, cookies, and host replace client-level
  values with the same key.
- `X-PurpleCommand-Exchange` is reserved and overwritten by the worker. Do not
  expose it as an editable header.
- Redirects are disabled by default. Cross-origin redirects require both
  `follow_redirects` and `allow_cross_origin_redirects`.
- Explicit proxy schemes may be `http`, `https`, `socks5`, or `socks5h`.
- Client certificate and key paths must be supplied together.
- SPKI pins accept 64-character hexadecimal or standard/raw base64, with an
  optional `sha256/` prefix.
- A custom positive retry interval must be at least 100 ms.
- Health interval cannot be negative. Failure threshold `0` selects the
  default; explicit values are `1` through `100`.
- Keep `expected_status` empty unless an intermediary requires a strict list.
  A task that terminates the implant may return `204`.
- `profile` is optional. When supplied, it must identify the implant profile
  used to build the endpoint. First blood validates payload type and consumes
  that profile's OTS when configured.

Minimal recommended configuration:

```json
{
  "client": {
    "base_url": "https://10.20.30.40:8443"
  },
  "request": {
    "method": "POST",
    "path": "/"
  },
  "healthcheck": {
    "enabled": true,
    "interval": 30000000000,
    "failure_threshold": 3
  }
}
```

To disable background health traffic while retaining task delivery:

```json
{
  "healthcheck": {"enabled": false}
}
```

The worker will still perform a health/check request when a task is queued,
then send the returned encrypted task in a second request.

### 4.3 Speaker secret redaction

Speaker values are redacted in list/get replies, snapshots, and lifecycle
events. The server returns `[REDACTED]` for:

- every client/request header value;
- every client/request query value;
- every client/request cookie value;
- `client.proxy_url`;
- `client.tls.client_key_file`;
- matching credential text embedded in `last_error`.

Keys/names remain visible. `[REDACTED]` is a display marker, not a secret
preservation instruction.

The current update API replaces the entire configuration when `config` is
present. Therefore:

- The UI must never send a fetched redacted configuration back unchanged.
- Rename-only and persistence-only changes must omit `config`.
- A configuration edit must require the operator to re-enter all redacted
  values that must be preserved.
- After create/update, discard plaintext credentials from frontend state and
  retain the redacted reply.

## 5. Speaker operations

All reads require authentication. All mutations below require the admin
principal.

| Request type | `data` | Success `data` |
|---|---|---|
| `ask.speaker.list` | `{}` | `Speaker[]` |
| `ask.speaker.get` | `{"name": string}` | `Speaker` |
| `ask.speaker.create` | `SpeakerCreateRequest` | stopped `Speaker` |
| `ask.speaker.update` | `SpeakerUpdateRequest` | updated `Speaker` |
| `ask.speaker.start` | `{"name": string}` | connected `Speaker` after first blood |
| `ask.speaker.stop` | `{"name": string}` | stopped `Speaker` |
| `ask.speaker.restart` | `{"name": string}` | connected `Speaker` after new first blood |
| `ask.speaker.delete` | `{"name": string}` | deleted `Speaker` snapshot |

Create:

```ts
interface SpeakerCreateRequest {
  name: string;
  persistent?: boolean; // defaults to true
  config: SpeakerConfig;
}
```

```json
{
  "version": 1,
  "type": "ask.speaker.create",
  "id": "a63de20d-bd5a-44ba-94e5-e82092499d25",
  "client_id": "web-ui",
  "data": {
    "name": "edge-speaker",
    "persistent": true,
    "config": {
      "profile": "linux-bind",
      "client": {"base_url": "https://10.20.30.40:8443"},
      "request": {"method": "POST", "path": "/"},
      "healthcheck": {
        "enabled": true,
        "interval": 30000000000,
        "failure_threshold": 3
      }
    }
  }
}
```

Create never starts the speaker; send `ask.speaker.start` separately.

Update:

```ts
interface SpeakerUpdateRequest {
  name: string;                       // current name / selector
  new_name?: string;
  persistent?: boolean;
  config?: SpeakerConfig;             // full replacement when present
  expected_config_version?: number;
}
```

The UI should always include the latest `expected_config_version`. On a
version conflict, refetch and ask the operator to reconcile instead of silently
overwriting another operator's change.

Rename-only example that safely omits redacted configuration:

```json
{
  "name": "edge-speaker",
  "new_name": "edge-speaker-2",
  "expected_config_version": 4
}
```

## 6. Listener resource

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
  host: string;
  port: string;
  running: boolean;
  persistent: boolean;
  associations: number;
  driver?: string;
  options?: Record<string, unknown>;
  routes?: ListenerRoute[];
  state?: ListenerState;
  desired_state?: "stopped" | "running";
  address?: string;
  last_error?: string;
  config_version?: number;
}
```

- `host` and `port` are compatibility projections of `options.bind`; new UIs
  should edit `options` using the driver schema.
- `address` is the actual runtime address and can differ from requested bind
  options, especially when port `0` requests an ephemeral port.
- `running` is true only in `running` state.
- General updates are rejected in `starting`, `running`, or `stopping`.
- Listener deletion stops an active runtime automatically.
- A persistent listener with desired state `running` is started during server
  restoration.

Listener operations:

| Request type | `data` | Success `data` |
|---|---|---|
| `ask.listener.list` | `{}` | `Listener[]` |
| `ask.listener.get` | `{"name": string}` | `Listener` |
| `ask.listener.create` | `ListenerCreateRequest` | `Listener` |
| `ask.listener.update` | `ListenerUpdateRequest` | `Listener` |
| `ask.listener.start` | `{"name": string}` | running `Listener` |
| `ask.listener.stop` | `{"name": string}` | stopped `Listener` |
| `ask.listener.restart` | `{"name": string}` | running `Listener` |
| `ask.listener.delete` | `{"name": string}` | `{"name": string}` |
| `ask.listener-type.list` | `{}` | `ListenerDriverDefinition[]` |
| `ask.listener-type.get` | `{"name": driverID}` | `ListenerDriverDefinition` |
| `ask.listener-carrier.list` | `{}` | `ListenerCarrierDefinition[]` |

```ts
interface ListenerCreateRequest {
  name: string;
  host?: string;                 // compatibility input
  port?: string;                 // compatibility input
  persistent?: boolean;          // defaults to true
  driver?: string;               // defaults to "http"
  options?: Record<string, unknown>;
  routes?: ListenerRoute[];
  start?: boolean;
}

interface ListenerUpdateRequest {
  name: string;
  driver?: string;
  key?: string;                  // legacy host/port/persist update
  value?: string;
  options?: Record<string, unknown>;
  routes?: ListenerRoute[];
  persistent?: boolean;
  expected_config_version?: number;
}
```

Use the latest nonzero `expected_config_version` for updates. Listener update
does not support rename.

### 6.1 Dynamic listener form contract

The UI should obtain definitions instead of hardcoding listener drivers:

```ts
type ListenerOptionType =
  | "string" | "integer" | "boolean" | "duration"
  | "string_list" | "string_map" | "object" | "file" | "secret";

interface ListenerOptionDefinition {
  key: string;                    // dotted JSON path, e.g. "bind.host"
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
```

The built-in `http` driver currently advertises `routes`, `custom_headers`,
`tls`, `interactive`, `carriers`, `file_hosting`, and `custom_404`.

Routes select how an inbound HTTP exchange is matched and where protocol bytes
are read/written:

```ts
interface ListenerRoute {
  id: string;
  purpose: string;
  priority?: number;
  match?: Record<string, unknown>;
  options?: Record<string, unknown>;
  inbound:  {type: string; options?: Record<string, unknown>};
  outbound: {type: string; options?: Record<string, unknown>};
}
```

Carrier definitions use the same option-definition shape. Built-in carrier
types are discoverable from `ask.listener-carrier.list`; the UI should not
assume the registry is static.

### 6.2 Hosted-file subresource

Hosted files are independently mutable while an HTTP listener is running:

| Request | Purpose |
|---|---|
| `ask.listener.hosted` | Read current hosted-file configuration |
| `ask.listener.hosted.set` | Replace hosted files and optional 404 page |
| `ask.listener.hosted.add` | Add/replace one exact URL path |
| `ask.listener.hosted.remove` | Remove one URL path |
| `ask.listener.hosted.not-found.set` | Set the custom 404 file |
| `ask.listener.hosted.not-found.clear` | Clear the custom 404 file |

Every mutation accepts `expected_config_version`; use the version returned by
the most recent listener or hosted-file response.

## 7. Sessions and transport association

```ts
interface Session {
  name: string;
  uuid: string;
  payload_type: string;
  transport: "listener" | "speaker";
  speaker?: string;
  speaker_uuid?: string;
  listener?: string;
  listener_uuid?: string;
  alive: boolean;
  liveness: "healthy" | "unavailable" | "unknown";
  health_monitoring: boolean;
  terminating: boolean;
  last_seen: string;
  // Existing host, user, process, socket, PID, sleep, and first_seen fields
  // remain unchanged.
}
```

Render association by `transport`:

- `listener`: show `listener` and resolve by `listener_uuid` when available.
- `speaker`: show `speaker` and resolve by `speaker_uuid`.
- Never infer transport from which optional name happens to be populated.
- Use UUID for navigation and identity. Names can change or represent legacy
  records.

Liveness differs by transport:

- Listener sessions become `unavailable` when implant callback timing exceeds
  the reverse-session timeout.
- Speaker sessions with health monitoring enabled become `unavailable` only
  after the speaker worker reaches its configured failure threshold.
- Speaker sessions with health monitoring disabled report `unknown`; this is
  intentional and must not be styled as healthy or failed.
- Speaker task traffic updates `last_seen` even when periodic healthchecks are
  disabled.

## 8. Required UI acceptance cases

The UI implementation should pass these behavior cases:

1. Create a stopped speaker with POST `/`, then start it and show `connected`
   only after the successful reply/first blood.
2. Display `disconnected` plus sanitized `last_error`, then return to
   `connected` when the recovery event arrives.
3. Disable background healthchecks and render the associated session liveness
   as `unknown`; verify that queued tasks still complete.
4. Reject local speaker names outside `[A-Za-z0-9_.-]{1,64}` and still display
   server validation errors.
5. Prevent non-admin speaker mutation controls and handle server `forbidden`
   responses regardless.
6. Never repopulate a secret input with `[REDACTED]` and never submit that
   marker as configuration.
7. Send the latest speaker/listener config version and recover from a version
   conflict by refetching.
8. Disable speaker edit/delete while active; require stop before configuration
   replacement.
9. Allow listener hosted-file updates while the listener is running, while
   keeping general listener configuration locked.
10. Group sessions by the explicit `transport` discriminator and stable UUID.
11. Reconcile replies, snapshots, and duplicate/out-of-order events without
    duplicating resources.
12. Convert every speaker duration between human units and integer nanoseconds
    without precision loss.
