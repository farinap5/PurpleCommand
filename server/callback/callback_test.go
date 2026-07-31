package callback

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"net/http/httptest"
	"testing"

	impx "purpcmd/implant"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	serverimplant "purpcmd/server/implant"
)

func writeMetadata(t *testing.T, buffer *bytes.Buffer, sessionID uint32) {
	t.Helper()
	metadata := []any{
		uint32(1),
		sessionID,
		[12]byte{},
		uint32(0),
		uint16(0),
		uint32(10),
		uint8(1),
	}
	for _, value := range metadata {
		if err := binary.Write(buffer, binary.BigEndian, value); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseMetadataRejectsEveryTruncation(t *testing.T) {
	full := new(bytes.Buffer)
	writeMetadata(t, full, 12345)
	for length := 0; length < full.Len(); length++ {
		var metadata impx.ImplantMetadata
		if err := ParseMetadata(bytes.NewReader(full.Bytes()[:length]), &metadata); !errors.Is(err, ErrMalformedPayload) {
			t.Fatalf("length %d returned %v", length, err)
		}
	}
}

func TestParseResponseRejectsClaimedHugeLengthBeforeAllocation(t *testing.T) {
	packet := new(bytes.Buffer)
	writeMetadata(t, packet, 12345)
	_ = binary.Write(packet, binary.BigEndian, [8]byte{'t', 'a', 's', 'k'})
	_ = binary.Write(packet, binary.BigEndian, uint32(math.MaxUint32))

	if err := ParseResponse(bytes.NewReader(packet.Bytes()), "12345"); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("huge response length returned %v", err)
	}
}

func TestParsersRejectTrailingAndOversizedFields(t *testing.T) {
	response := new(bytes.Buffer)
	writeMetadata(t, response, 12345)
	_ = binary.Write(response, binary.BigEndian, [8]byte{'t', 'a', 's', 'k'})
	_ = binary.Write(response, binary.BigEndian, uint32(0))
	response.WriteByte(1)
	if err := ParseResponse(bytes.NewReader(response.Bytes()), "12345"); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("trailing response data returned %v", err)
	}

	chunk := new(bytes.Buffer)
	writeMetadata(t, chunk, 12345)
	_ = binary.Write(chunk, binary.BigEndian, [8]byte{'t', 'a', 's', 'k'})
	_ = binary.Write(chunk, binary.BigEndian, uint32(maxLootFileNameSize+1))
	if err := ParseChunkData(bytes.NewReader(chunk.Bytes()), "12345"); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("oversized file name returned %v", err)
	}

	chunk.Reset()
	writeMetadata(t, chunk, 12345)
	_ = binary.Write(chunk, binary.BigEndian, [8]byte{'t', 'a', 's', 'k'})
	_ = binary.Write(chunk, binary.BigEndian, uint32(1))
	chunk.WriteByte('a')
	_ = binary.Write(chunk, binary.BigEndian, uint32(maxLootContentSize+1))
	if err := ParseChunkData(bytes.NewReader(chunk.Bytes()), "12345"); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("oversized content returned %v", err)
	}
}

func TestParseCheckBindsEncryptedMetadataToAuthenticatedSession(t *testing.T) {
	packet := new(bytes.Buffer)
	writeMetadata(t, packet, 54321)
	if _, err := ParseCheck(bytes.NewReader(packet.Bytes()), "12345"); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("mismatched session returned %v", err)
	}
}

func TestRegistrationRejectsInvalidPayloadType(t *testing.T) {
	previousMap := serverimplant.ImplantMAP
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	t.Cleanup(func() { serverimplant.ImplantMAP = previousMap })

	packet := new(bytes.Buffer)
	writeMetadata(t, packet, 12345)
	_ = binary.Write(packet, binary.BigEndian, [16]byte{})
	_ = binary.Write(packet, binary.BigEndian, [16]byte{})
	data := bytes.Join([][]byte{[]byte("proc"), []byte("host"), []byte("user"), []byte("invalid type")}, internal.SEP)
	_ = binary.Write(packet, binary.BigEndian, uint16(len(data)))
	packet.Write(data)

	if err := ParseAndReg(bytes.NewReader(packet.Bytes()), httptest.NewRequest("POST", "/", nil)); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("invalid payload type returned %v", err)
	}
	if len(serverimplant.ImplantMAP) != 0 {
		t.Fatal("invalid payload type registered a session")
	}
}

func TestParseCallbackRejectsInvalidFramingWithoutPanicking(t *testing.T) {
	previousMap := serverimplant.ImplantMAP
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	t.Cleanup(func() { serverimplant.ImplantMAP = previousMap })

	key := [16]byte{1}
	iv := [16]byte{2}
	encryption := encrypt.EncryptImport(key, iv)
	imp := serverimplant.ImplantNew("12345")
	imp.ImplantSetEncryption(encryption)
	imp.ImplantAddImplant()
	request := httptest.NewRequest("POST", "/?a=12345", nil)

	cases := [][]byte{
		[]byte("%%%"),
		[]byte("AA=="),
		[]byte("QUJD\nRA=="),
	}
	for _, encoded := range cases {
		messageType, task, err := ParseCallback(encoded, request, "12345")
		if !errors.Is(err, ErrMalformedPayload) || messageType != internal.NIL || task != nil {
			t.Fatalf("frame %q returned type=%d task=%q err=%v", encoded, messageType, task, err)
		}
	}

	misalignedCiphertext := make([]byte, 17)
	encryption.HMACPackAddHmac(&misalignedCiphertext)
	encoded := []byte(base64.StdEncoding.EncodeToString(misalignedCiphertext))
	if _, _, err := ParseCallback(encoded, request, "12345"); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("valid-HMAC misaligned ciphertext returned %v", err)
	}
}

func TestParseCallbackRejectsOversizedEncodedInput(t *testing.T) {
	encoded := bytes.Repeat([]byte{'A'}, MaxEncodedPayloadSize+1)
	if _, _, err := ParseCallback(encoded, nil, ""); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("oversized input returned %v", err)
	}
}

func TestParseCallbackRejectsEncryptedSessionMismatch(t *testing.T) {
	previousMap := serverimplant.ImplantMAP
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	t.Cleanup(func() { serverimplant.ImplantMAP = previousMap })

	key := [16]byte{1}
	iv := [16]byte{2}
	encryption := encrypt.EncryptImport(key, iv)
	imp := serverimplant.ImplantNew("12345")
	imp.ImplantSetEncryption(encryption)
	imp.ImplantAddImplant()

	plaintext := new(bytes.Buffer)
	_ = binary.Write(plaintext, binary.BigEndian, internal.CHK)
	writeMetadata(t, plaintext, 54321)
	framed := encryption.AESCbcEncrypt(plaintext.Bytes())
	encryption.HMACPackAddHmac(&framed)
	encoded := []byte(base64.StdEncoding.EncodeToString(framed))

	if _, _, err := ParseCallback(encoded, nil, "12345"); !errors.Is(err, ErrMalformedPayload) {
		t.Fatalf("encrypted session mismatch returned %v", err)
	}
}

func FuzzParseMetadataNeverPanics(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 31))
	f.Fuzz(func(t *testing.T, data []byte) {
		var metadata impx.ImplantMetadata
		_ = ParseMetadata(bytes.NewReader(data), &metadata)
	})
}
