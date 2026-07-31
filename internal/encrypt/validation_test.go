package encrypt

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func TestHMACVerifyRejectsShortInputWithoutPanicking(t *testing.T) {
	encryption := EncryptImport([16]byte{}, [16]byte{})
	for length := 0; length < 16; length++ {
		if encryption.HMACVerifyHash(make([]byte, length)) {
			t.Fatalf("accepted %d-byte HMAC frame", length)
		}
	}
}

func TestAESCbcDecryptRejectsInvalidCiphertextLengths(t *testing.T) {
	encryption := EncryptImport([16]byte{}, [16]byte{})
	for _, ciphertext := range [][]byte{nil, {1}, make([]byte, 15), make([]byte, 17)} {
		if _, err := encryption.AESCbcDecrypt(ciphertext); err == nil {
			t.Fatalf("accepted ciphertext with length %d", len(ciphertext))
		}
	}
}

func TestAESUnpadValidatesEveryPaddingByte(t *testing.T) {
	invalid := make([]byte, 16)
	invalid[14] = 3
	invalid[15] = 2
	if _, err := AESUnpad(invalid); err == nil {
		t.Fatal("accepted inconsistent PKCS#7 padding")
	}

	valid := append([]byte("payload"), []byte{9, 9, 9, 9, 9, 9, 9, 9, 9}...)
	plaintext, err := AESUnpad(valid)
	if err != nil || string(plaintext) != "payload" {
		t.Fatalf("valid padding failed: plaintext=%q err=%v", plaintext, err)
	}
}

func TestRSADecodeRejectsMissingOrMisalignedPayload(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	previousServerKey := serverRSAKey
	serverRSAKey = nil
	t.Cleanup(func() { serverRSAKey = previousServerKey })

	encryption := Encrypt{RSAPrivate: privateKey}
	if _, err := encryption.RSADecode(make([]byte, privateKey.Size())); err == nil {
		t.Fatal("accepted RSA frame without an encrypted payload")
	}
	if _, err := encryption.RSADecode(make([]byte, privateKey.Size()+1)); err == nil {
		t.Fatal("accepted non-block-aligned RSA payload")
	}
}
