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

// globalPublicKeyDER holds the DER-encoded public key for implants
var globalPublicKeyDER []byte

// SetGlobalPublicKeyDER stores public key DER bytes for use by EncryptInit in implants.
func SetGlobalPublicKeyDER(der []byte) error {
	// Verify it's a valid public key
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return fmt.Errorf("SetGlobalPublicKeyDER: %w", err)
	}
	if _, ok := key.(*rsa.PublicKey); !ok {
		return fmt.Errorf("SetGlobalPublicKeyDER: key is not RSA")
	}
	globalPublicKeyDER = der
	return nil
}

// LoadServerRSAKey reads a PEM-encoded RSA private key from path (PKCS#8 or PKCS#1 format)
// and stores it for use by RSADecode.  Call this once during server initialization.
func LoadServerRSAKey(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("LoadServerRSAKey: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("LoadServerRSAKey: no PEM block found in %s", path)
	}

	// Try PKCS#8 first (modern format)
	keyInterface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		// PKCS#8 succeeded, extract the RSA key
		if rsaKey, ok := keyInterface.(*rsa.PrivateKey); ok {
			serverRSAKey = rsaKey
			return nil
		}
		return fmt.Errorf("LoadServerRSAKey: key is not RSA")
	}

	// Fall back to PKCS#1 (traditional RSA format)
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("LoadServerRSAKey: failed to parse key as PKCS#8 or PKCS#1: %w", err)
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

	enc := Encrypt{
		iv:      iv,
		block:   block,
		aeskey:  key,
		hmackey: xor(iv, key),
	}

	// If a global public key has been set (implant usage), load it
	if len(globalPublicKeyDER) > 0 {
		if err := enc.SetPublicKeyDER(globalPublicKeyDER); err != nil {
			panic("failed to load global public key: " + err.Error())
		}
	}

	return enc
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
