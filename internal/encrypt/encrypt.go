package encrypt

import (
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
)

// serverRSAKey holds the server RSA private key, set at startup via LoadServerRSAKey.
var serverRSAKey *rsa.PrivateKey

// LoadServerRSAKey reads a PKCS#1 PEM-encoded RSA private key from path and
// stores it for use by RSADecode.  Call this once during server initialisation.
func LoadServerRSAKey(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("LoadServerRSAKey: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("LoadServerRSAKey: no PEM block found in %s", path)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("LoadServerRSAKey: %w", err)
	}
	serverRSAKey = key
	return nil
}

// LoadServerRSAKeyBytes parses a PKCS#1 DER-encoded RSA private key from raw
// bytes (e.g. from an embedded FS) and stores it for use by RSADecode.
func LoadServerRSAKeyBytes(der []byte) error {
	key, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return fmt.Errorf("LoadServerRSAKeyBytes: %w", err)
	}
	serverRSAKey = key
	return nil
}

/*
xor function for byte arrays.
Both arrays must have the same length.
*/
func xor(d1, d2 [16]byte) [16]byte {
	var r [16]byte
	for i := range r {
		r[i] = d1[i] ^ d2[i]
	}
	return r
}

func EncryptInit() Encrypt {
	var key [16]byte
	io.ReadFull(rand.Reader, key[:])

	var iv [16]byte
	io.ReadFull(rand.Reader, iv[:])

	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err.Error())
	}

	return Encrypt{
		iv:      iv,
		block:   block,
		aeskey:  key,
		hmackey: xor(iv, key),
	}
}

/*
EncryptImport creates a symmetric-only Encrypt used for AES-CBC+HMAC
operations once the session key has been exchanged.  RSA is not available
on objects created this way; use the package-level serverRSAKey for that.
*/
func EncryptImport(key, iv [16]byte) Encrypt {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err.Error())
	}

	return Encrypt{
		aeskey:  key,
		iv:      iv,
		hmackey: xor(key, iv),
		block:   block,
	}
}

func (e Encrypt) EncryptGetKeys() ([16]byte, [16]byte) {
	return e.aeskey, e.iv
}

/*
	key  = x
	IV   = y
	hmac = x xor y
*/
