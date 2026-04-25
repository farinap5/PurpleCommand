package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

func RandomString(length int) []byte {
	b := make([]byte, length)
	rand.Read(b)
	return b
}

func RandomBytes8() [8]byte {
	var b [8]byte
	rand.Read(b[:])
	return b
}

// RandomAlphanumericID8 generates an 8-byte ID with random lowercase letters and numbers.
func RandomAlphanumericID8() [8]byte {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	var id [8]byte
	var randomBytes [8]byte
	rand.Read(randomBytes[:])

	for i := 0; i < 8; i++ {
		id[i] = charset[int(randomBytes[i])%len(charset)]
	}
	return id
}

// ReadPublicKeyDER reads a PEM-encoded RSA public key and returns its DER bytes.
// This is used to embed the server public key into implants.
func ReadPublicKeyDER(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ReadPublicKeyDER: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("ReadPublicKeyDER: no PEM block in %s", path)
	}
	// Verify it's actually an RSA public key
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("ReadPublicKeyDER: %w", err)
	}
	if _, ok := key.(*rsa.PublicKey); !ok {
		return nil, fmt.Errorf("ReadPublicKeyDER: key is not RSA")
	}
	return block.Bytes, nil
}
