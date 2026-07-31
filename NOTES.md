I completed an end-to-end audit of the repository. It compiles, but I would not deploy this branch in a real engagement yet. The main risks are authentication bypasses, exposed keys, remotely triggerable parser failures, unsafe concurrency, and several features whose configuration does not affect the generated implant.

## Critical and high-severity findings

1. SSH authentication accepts every valid public key.

   [pubCallBack](/home/elf/go/src/PurpleCommand/implant/ssh/sshfuncs.go:40) returns `nil, nil` when a key is unknown. In `x/crypto/ssh`, a nil error means authentication is permitted. Anyone able to become the SSH client on that reverse WebSocket can authenticate with any key and obtain the shell.

   Return a non-nil error for unknown keys and add an authentication test.

2. Private keys and operational state are committed.

   The repository tracks [server.key](/home/elf/go/src/PurpleCommand/server.key:1), [template/key/id_ecdsa](/home/elf/go/src/PurpleCommand/template/key/id_ecdsa:1), and `database.db`, despite the database being listed in [.gitignore](/home/elf/go/src/PurpleCommand/.gitignore:1). The SSH key also has overly broad `0664` permissions.

   Possession of `server.key` allows recovery of session keys from captured registrations. Both key pairs should be considered compromised: rotate them, remove them from Git history, load them from external configuration, and enforce `0600`.

3. The SSH dependency has ten reachable vulnerabilities.

   `govulncheck` found ten called vulnerabilities in `golang.org/x/crypto@v0.18.0`, including authentication/certificate bypasses, panics, deadlocks, and memory-exhaustion paths. Most are fixed by `v0.52.0`; the module currently appears in [go.mod](/home/elf/go/src/PurpleCommand/go.mod:24).

   The reported issues include GO-2026-5023, GO-2026-5020, GO-2026-5018, GO-2026-5017, GO-2026-5014, GO-2026-5013, GO-2025-4134, GO-2025-3487, and GO-2024-3321. Upgrade and rerun `govulncheck`.

4. Malformed network data can panic or exhaust the server.

   **Historical finding — fixed. See “Fix report: finding 4” below.**

   [HMACVerifyHash](/home/elf/go/src/PurpleCommand/internal/encrypt/hmac.go:14) slices `len(pack)-16` without first requiring 16 bytes. Base64 errors are ignored before calling it in [ParseCallback](/home/elf/go/src/PurpleCommand/server/callback/callback.go:41). Likewise, CBC decryption does not validate block alignment before `CryptBlocks`.

   Response, filename, and content lengths are trusted before allocation in [callback.go](/home/elf/go/src/PurpleCommand/server/callback/callback.go:181). A party can register its own implant, send an authenticated packet declaring a multi-gigabyte length inside a small request, and potentially exhaust server memory.

   All parsers should return errors, enforce exact remaining lengths and explicit limits, and never panic on network input.

5. The cryptographic protocol reuses one CBC IV indefinitely.

   Every message uses the same AES key and IV via [AESCbcEncrypt](/home/elf/go/src/PurpleCommand/internal/encrypt/aes.go:28). The HMAC key is derived by XORing the AES key and IV in [encrypt.go](/home/elf/go/src/PurpleCommand/internal/encrypt/encrypt.go:101), and there is no message counter or replay protection. Registration encryption is also CBC without authentication.

   This leaks repeated prefixes, enables replay, and makes the protocol unnecessarily fragile. Prefer AES-GCM or ChaCha20-Poly1305 with unique nonces, sequence numbers, HKDF-separated keys, and explicit protocol/version fields.

6. Shared server and Lua state is not concurrency-safe.

   `ImplantMAP`, task queues, `ListenerMAP`, `ScriptMAP`, and `CMDMAP` are ordinary maps accessed from concurrent HTTP handlers and the CLI. Lua callbacks also use the same `*lua.LState` from HTTP, CLI, and script goroutines; GopherLua states are not safe for concurrent use.

   This can cause data races, corrupt Lua stacks, or produce `concurrent map read and map write` panics. Add ownership boundaries or mutexes and serialize each Lua state through one goroutine.

7. HTTP lifecycle and error handling permit hangs and process termination.

   The implant’s [http.Client](/home/elf/go/src/PurpleCommand/implant/core/http.go:18) has no timeout, panics on transport errors, ignores status codes, and callers ignore returned errors. The server’s [http.Server](/home/elf/go/src/PurpleCommand/server/listener/http.go:20) has no read-header, read, write, or idle timeouts. WebSockets have no deadlines either.

   Network failures can permanently hang the implant, while slow clients can consume server resources.

## Major functional inconsistencies

8. URI and User-Agent profile options are nonfunctional.

   `URI` and `UA` exist in the profile and builder, but the only occurrences in [template/main.go](/home/elf/go/src/PurpleCommand/template/main.go:12) are commented out. The generated implant hardcodes `/` in [core.Start](/home/elf/go/src/PurpleCommand/implant/core/main.go:19), and HTTP requests never set the configured User-Agent.

   Both builder paths therefore modify comments rather than behavior.

9. Interactive SSH is broken in the default checkout.

   The server reads `cmd/key/id_ecdsa` in [connector.go](/home/elf/go/src/PurpleCommand/server/ssh/connector.go:68), but the tracked key is under `template/key`. It also disables host-key verification at [connector.go:80](/home/elf/go/src/PurpleCommand/server/ssh/connector.go:80). The implant invokes SSH using hardcoded `"aaa"` and `"/any.png"` and ignores the returned error in [implant/core/main.go](/home/elf/go/src/PurpleCommand/implant/core/main.go:84).

10. Cross-platform profiles overpromise compatibility.

   The builder advertises Windows, Darwin, ARM64, and 386. My Windows/amd64 build failed because `HandServerConn` is excluded by the Unix build tag but still called. Darwin, ARM64, and 386 compile, but memory execution uses the Linux/amd64 syscall number `319` and `/proc/self/fd/3` in [memexec.go](/home/elf/go/src/PurpleCommand/implant/core/memexec.go:12). Architecture reporting only recognizes amd64 in [implant.go](/home/elf/go/src/PurpleCommand/implant/core/implant.go:19).

   Restrict validated targets or split OS/architecture implementations with proper build tags.

11. Task delivery is at-most-once and loses work easily.

   **Historical finding — fixed. See “Fix report: findings 11-13” below.**

   A task is marked `Sent` before the HTTP response is confirmed in [implant.go](/home/elf/go/src/PurpleCommand/server/implant/implant.go:175). There is no acknowledgment, retry, delivery timeout, or task requeue. A dropped response permanently strands the task.

   Tasks and responses also remain in memory forever.

12. Listener persistence is inconsistent.

   **Historical finding — fixed. See “Fix report: findings 11-13” below.**

   [ListenerDelete](/home/elf/go/src/PurpleCommand/server/listener/listener.go:160) only removes the in-memory listener, so persistent listeners return after restart. The `persist` option deletes or inserts a row and then calls a database update method that does not support `"persist"`. Starting an already-running listener prints a warning but continues, overwrites its server pointer, and starts another goroutine at [http.go:12](/home/elf/go/src/PurpleCommand/server/listener/http.go:12).

13. Loot is not functional on a fresh clone.

   **Historical finding — fixed. See “Fix report: findings 11-13” below.**

   `SaveData` assumes `loot/` already exists at [loot.go:22](/home/elf/go/src/PurpleCommand/server/loot/loot.go:22), but Git does not track the empty ignored directory. Exports use `O_APPEND`, so exporting twice concatenates the file. Partial UUID searches are ambiguous and do not verify uniqueness.

14. Session deletion cannot work as documented.

   Help advertises session deletion, but [runDelete](/home/elf/go/src/PurpleCommand/server/core/defcallbacks.go:282) contains no session branch. Even if called directly, implants initialize `Alive=true`, never transition to false, and [ImplantDelete](/home/elf/go/src/PurpleCommand/server/implant/implant.go:81) refuses to delete a live implant.

15. Sensitive cryptographic and operation data is logged.

   The implant prints its AES key, IV, registration, check-in, and response ciphertext in [implant/core/main.go](/home/elf/go/src/PurpleCommand/implant/core/main.go:31). The server logs complete response bodies in [callback.go](/home/elf/go/src/PurpleCommand/server/callback/callback.go:189). These logs undermine operational security and may contain credentials or loot.

## Documentation drift

The protocol specification materially disagrees with the implementation:

- It calls metadata 25 bytes at [IMPLANT_PROTOCOL.md:108](/home/elf/go/src/PurpleCommand/IMPLANT_PROTOCOL.md:108); the serialized structure is 33 bytes.
- [implant/types.go](/home/elf/go/src/PurpleCommand/implant/types.go:3) separately claims 15 bytes.
- Registration is documented as entirely RSA-encrypted, while the implementation uses RSA-wrapped auxiliary AES.
- The HMAC key derivation is described inconsistently and does not specify the implemented XOR derivation.
- The documented MEMEXEC payload differs from the implemented upload envelope.
- IFCONFIG and CAT are absent from the command-code tables.
- Callback documentation lists five or six arguments while the implementation passes eight.
- The GET transport is described as a parameter/body, while the implementation uses a cookie.
- The document calls CHU “chunked,” but files are read, encrypted, and posted as one allocation/request. The server caps POST bodies at 10 MiB, meaning only roughly 7.5 MiB of binary data fits after Base64 and protocol overhead.

## Original audit verification summary

Historical snapshot captured before the fixes documented below:

- `go test ./...`: passes compilation; every package reports no test files.
- `go vet ./...`: passes.
- Race-enabled compilation: passes, but cannot exercise races without tests.
- `gofmt -d`: reports formatting drift across many files.
- Windows/amd64 implant build: fails.
- Current dependency scan: ten reachable vulnerabilities.
- No repository files were changed. The pre-existing modified `database.db` remains the only worktree change.

The strongest remediation order is: rotate/remove keys, fix SSH authentication, upgrade `x/crypto`, harden all binary parsing, replace the crypto framing with AEAD and replay protection, serialize shared/Lua state, then repair task delivery and builder configuration.

## Fix report: findings 11-13

Implemented on July 31, 2026.

### 11. Task delivery no longer strands a task after one attempt

Status: fixed with leased, at-least-once delivery.

- An unfinished task is leased when sent rather than becoming permanently
  ineligible after the first check-in.
- The retry interval is twice the implant sleep interval, with a minimum of 15
  seconds and a maximum of five minutes.
- When a lease expires without a completed response, the server sends the same
  task again with the same task ID. `Attempts` and `LastSent` record delivery
  history.
- Response processing is reserved atomically. Duplicate or concurrent
  responses for an already completed/in-progress task are ignored, so Lua
  callbacks and loot persistence are not repeated.
- A failed response persistence operation releases the reservation so the
  response can be retried.
- Completed tasks are retained for 24 hours for inspection and then pruned from
  both the queue and task map when task activity resumes.
- Task queue/map operations now use a per-implant mutex.

Operational note: this is intentionally at-least-once delivery. If the implant
executes a task but its completion response is lost for longer than the lease,
the task may execute again. True exactly-once execution would require a new
implant-side receipt/acknowledgment state in the wire protocol. The current
change prefers a possible duplicate over silently losing the task.

Regression coverage:

- Initial delivery, no early retry, retry after lease expiration, stable task
  ID, and incremented attempt count.
- Completed tasks are not resent.
- Concurrent/duplicate responses are rejected.
- Aborted response processing can be retried.
- Expired completed tasks are removed.

### 12. Listener persistence now has one consistent lifecycle

Status: fixed.

- New persistent listeners are inserted into SQLite before being exposed in
  memory. A failed insert no longer leaves a listener that only appears to have
  been saved.
- `host`, `port`, and `uuid` updates write to SQLite first for persistent
  listeners, preventing database/memory divergence on failure.
- `set persist false` removes the database row and keeps the listener in memory
  as an ephemeral listener. Later option changes do not recreate the row.
- `set persist true` inserts one row containing the listener's current values
  and running state.
- Deleting a persistent listener deletes its SQLite row before deleting the
  in-memory object, so it does not reappear after restart.
- Startup reload constructs listeners directly from stored rows instead of
  calling the normal create path and attempting a duplicate insert.
- Stale rows marked non-persistent by older versions are removed and are not
  resurrected.
- Starting an already running listener now returns a defined error and cannot
  overwrite the active `http.Server` pointer.
- Socket binding is performed synchronously, so bind failures are reported
  before the listener is marked running. Persistent running state is updated
  on successful start and shutdown.
- Listener running state is protected by a mutex.
- Database update/delete helpers now reject unknown options and verify that
  exactly one row was affected.

Regression coverage:

- Persistent create, disable persistence, ephemeral update, re-enable
  persistence with current values, and permanent delete.
- Reload of a persistent listener without duplicate insertion.
- Cleanup/non-restoration of stale non-persistent rows.
- Rejection of a duplicate start.

### 13. Loot works on a fresh checkout and no longer exports ambiguously

Status: fixed.

- The configured loot directory is created automatically on the first save
  with mode 0700; no pre-created Git directory is required.
- Loot files use mode 0600 and exclusive creation, preventing silent overwrite
  of an existing UUID file.
- If database insertion fails after writing a file, the new file is removed so
  it does not become an untracked orphan.
- Export uses truncation rather than append, so repeated exports produce the
  exact loot content instead of concatenating copies.
- Stored UUIDs are parsed and validated before they are used to construct disk
  paths.
- A full UUID receives exact-match priority. Partial UUIDs must resolve to one
  entry; zero matches and ambiguous matches return errors.
- Loot listing now reports database errors instead of silently displaying an
  empty table.
- `LOOT_USAGE.md` now documents automatic directory creation, 0700/0600
  permissions, unique partial matching, rollback behavior, and truncating
  exports.

Regression coverage:

- First save creates the missing directory and writes correct content with
  private permissions.
- Export truncates stale destination data.
- Ambiguous fragments are rejected while exact UUIDs still resolve.
- A failed database insert removes the newly created file.

## Fix report: finding 4

Implemented on July 31, 2026.

### 4. Malformed network data is rejected without panics or unbounded allocation

Status: fixed at the server callback boundary.

Cryptographic framing:

- HMAC verification rejects frames shorter than the 16-byte tag before taking
  any slices.
- Authenticated callback frames must contain at least one AES block plus the
  HMAC tag.
- AES-CBC decryption rejects uninitialized ciphers, empty ciphertext, and
  ciphertext that is not block-aligned before calling `CryptBlocks`.
- PKCS#7 unpadding validates every padding byte, not only the final byte.
- RSA registration frames must contain a complete RSA key block followed by at
  least one aligned AES block.
- Base64 is decoded in strict standard form. Whitespace, non-alphabet bytes,
  invalid padding, empty input, and oversized input are rejected.

Binary parsing and allocation limits:

- Every metadata and packet field read now returns and propagates an error.
- Lengths are checked against both a configured maximum and the actual bytes
  remaining before memory is allocated.
- Registration metadata and loot filenames are limited to 4 KiB.
- Response and loot-content fields are limited to 8 MiB.
- Encoded HTTP payloads are limited to 10 MiB and decoded payloads to 8 MiB.
- All packet types reject trailing bytes, preventing alternate or ambiguous
  packet interpretations.
- Registration packets are accepted only on the registration route; encrypted
  session routes reject registration message types.
- The session ID inside encrypted metadata must match the session selected by
  the authenticated request parameter.
- A final recovery guard converts any unforeseen parser panic into a malformed
  payload error instead of allowing it to escape the callback boundary.

HTTP behavior:

- Parser errors propagate to the listener and produce HTTP 400 without
  incrementing listener association state.
- Unsupported methods produce HTTP 405 with `Allow: GET, POST`.
- GET callbacks require the cookie named `a`; an unrelated first cookie is no
  longer interpreted as callback data.
- POST bodies use the same 10 MiB limit as the Base64 parser.
- Detailed errors are logged server-side while clients receive generic HTTP
  status text.

Regression coverage:

- Short HMAC frames and empty/misaligned CBC ciphertext do not panic.
- Invalid PKCS#7 padding and incomplete RSA hybrid frames are rejected.
- Every truncated metadata length is rejected.
- Claimed 4 GiB response lengths fail before allocation.
- Oversized filenames/content and packets with trailing bytes are rejected.
- Invalid/non-canonical Base64, short authenticated frames, and valid-HMAC but
  misaligned ciphertext are rejected.
- Encrypted cross-session metadata is rejected.
- Malformed HTTP requests return 400/405 and do not change association state.
- A fuzz target exercises metadata parsing with arbitrary byte strings.

This fix hardens the existing protocol; it does not replace the CBC/HMAC design
or add replay protection. Those remain tracked separately under finding 5.
