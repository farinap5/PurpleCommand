package protocol

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	implantwire "purpcmd/implant"
)

func goldenMetadata() *implantwire.ImplantMetadata {
	return &implantwire.ImplantMetadata{
		PID: 0x01020304, SessionID: 0x05060708,
		OTS: [12]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		IP:  0x0c0d0e0f, Port: 0x1011, Sleep: 0x12131415, Arch: 0x16,
		Proc: "proc", Hostname: "host", User: "user", Type: "impl",
	}
}

func assertGolden(t *testing.T, name string, actual []byte, expectedHex string) {
	t.Helper()
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("%s frame = %x, want %x", name, actual, expected)
	}
}

func TestGoldenProtocolFrames(t *testing.T) {
	metadata := goldenMetadata()
	metadataHex := "0102030405060708000102030405060708090a0b0c0d0e0f10111213141516"
	keyHex := "202122232425262728292a2b2c2d2e2f"
	ivHex := "303132333435363738393a3b3c3d3e3f"
	dataHex := "001370726f6300686f7374007573657200696d706c"
	var key, iv [16]byte
	for index := range key {
		key[index] = byte(0x20 + index)
		iv[index] = byte(0x30 + index)
	}
	assertGolden(t, "registration", EncodeRegistration(metadata, key, iv), "0001"+metadataHex+keyHex+ivHex+dataHex)
	assertGolden(t, "check", EncodeCheck(metadata), "0002"+metadataHex)
	assertGolden(t, "response", EncodeResponse(metadata, []byte("abc"), [8]byte{'t', 'a', 's', 'k', '0', '0', '0', '1'}), "0003"+metadataHex+"7461736b30303031"+"00000003"+"616263")
	assertGolden(t, "chunk", EncodeChunk(metadata, "a.txt", []byte("xyz"), [8]byte{'t', 'a', 's', 'k', '0', '0', '0', '1'}), "0004"+metadataHex+"7461736b30303031"+"00000005"+"612e747874"+"00000003"+"78797a")
	assertGolden(t, "task", EncodeTask(0x0102, [8]byte{'t', 'a', 's', 'k', '0', '0', '0', '1'}, []byte("abc")), "0102"+"7461736b30303031"+"00000003"+"616263")
}

func TestMetadataAndTaskRoundTrip(t *testing.T) {
	metadata := goldenMetadata()
	buffer := new(bytes.Buffer)
	WriteMetadata(buffer, metadata)
	if buffer.Len() != MetadataSize {
		t.Fatalf("metadata size = %d", buffer.Len())
	}
	var decoded implantwire.ImplantMetadata
	if err := ReadMetadata(buffer, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PID != metadata.PID || decoded.SessionID != metadata.SessionID || decoded.OTS != metadata.OTS ||
		decoded.IP != metadata.IP || decoded.Port != metadata.Port || decoded.Sleep != metadata.Sleep || decoded.Arch != metadata.Arch {
		t.Fatalf("decoded metadata = %#v", decoded)
	}

	encoded := EncodeTask(7, [8]byte{'1', '2', '3', '4', '5', '6', '7', '8'}, []byte("payload"))
	task, err := DecodeTask(bytes.NewReader(encoded), DefaultMaxTaskPayload)
	if err != nil {
		t.Fatal(err)
	}
	if task.Code != 7 || string(task.ID[:]) != "12345678" || string(task.Payload) != "payload" {
		t.Fatalf("decoded task = %#v", task)
	}
}

func TestDecodeTaskIsBoundedAndExact(t *testing.T) {
	oversized := EncodeTask(1, [8]byte{}, []byte("12345"))
	if _, err := DecodeTask(bytes.NewReader(oversized), 4); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("oversized task error = %v", err)
	}
	claimedHuge := append(EncodeTask(1, [8]byte{}, nil)[:10], 0xff, 0xff, 0xff, 0xff)
	if _, err := DecodeTask(bytes.NewReader(claimedHuge), DefaultMaxTaskPayload); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("claimed huge task error = %v", err)
	}
	trailing := append(EncodeTask(1, [8]byte{}, nil), 1)
	if _, err := DecodeTask(bytes.NewReader(trailing), DefaultMaxTaskPayload); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("trailing task error = %v", err)
	}
}

func TestHTTPEnvelopeLimitFitsLargestProtocolFrame(t *testing.T) {
	// CHU is the largest frame: type, metadata, task ID, file-name length and
	// bytes, content length and bytes. CBC always adds one padding block at an
	// exact boundary, followed by the 16-byte protocol HMAC.
	plaintext := 2 + MetadataSize + 8 + 4 + MaxFileNameSize + 4 + MaxPayloadSize
	authenticated := ((plaintext / 16) + 1) * 16
	authenticated += 16
	encoded := base64.StdEncoding.EncodedLen(authenticated)
	if authenticated > MaxAuthenticatedPacketSize || encoded > MaxEncodedPacketSize {
		t.Fatalf("largest frame needs decoded=%d encoded=%d; limits=%d/%d",
			authenticated, encoded, MaxAuthenticatedPacketSize, MaxEncodedPacketSize)
	}
}
