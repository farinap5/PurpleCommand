package encrypt

import (
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// LoadPublicKey reads a PEM-encoded PKIX RSA public key from path and stores
// it on the Encrypt instance.  Used by the implant to embed the server pubkey.
func (e *Encrypt) LoadPublicKey(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("LoadPublicKey: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return fmt.Errorf("LoadPublicKey: no PEM block in %s", path)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("LoadPublicKey: %w", err)
	}
	pub, ok := key.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("LoadPublicKey: key is not RSA")
	}
	e.RSAPublic = pub
	return nil
}

// SetPublicKeyDER sets the RSA public key from a raw DER-encoded PKIX block.
// Intended for use by the implant when the public key is embedded.
func (e *Encrypt) SetPublicKeyDER(der []byte) error {
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return fmt.Errorf("SetPublicKeyDER: %w", err)
	}
	pub, ok := key.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("SetPublicKeyDER: key is not RSA")
	}
	e.RSAPublic = pub
	return nil
}

func (e Encrypt) RSAEncode(data []byte) ([]byte, error) {
	if e.RSAPublic == nil {
		return nil, fmt.Errorf("RSAEncode: no public key set")
	}
	auxEnc := EncryptInit()
	dataEnc := auxEnc.AESCbcEncrypt(data)

	a, b := auxEnc.EncryptGetKeys()

	var keys []byte
	keys = append(keys, a[:]...)
	keys = append(keys, b[:]...)

	keysEncoded, err := rsa.EncryptPKCS1v15(rand.Reader, e.RSAPublic, keys)
	if err != nil {
		return nil, err
	}

	return append(keysEncoded, dataEnc...), nil
}

func (e Encrypt) RSADecode(data []byte) ([]byte, error) {
	privKey := serverRSAKey
	if privKey == nil {
		if e.RSAPrivate == nil {
			return nil, fmt.Errorf("RSADecode: no private key loaded; call LoadServerRSAKey first")
		}
		privKey = e.RSAPrivate
	}

	rsaKeyLen := privKey.Size()
	if len(data) < rsaKeyLen+aes.BlockSize {
		return nil, fmt.Errorf("RSADecode: data too short for key and payload")
	}
	if (len(data)-rsaKeyLen)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("RSADecode: encrypted payload is not block-aligned")
	}

	decodedKeys, err := rsa.DecryptPKCS1v15(rand.Reader, privKey, data[:rsaKeyLen])
	if err != nil {
		return nil, err
	}

	if len(decodedKeys) < 32 {
		return nil, fmt.Errorf("RSADecode: decrypted key block too short")
	}

	var key, iv [16]byte
	copy(key[:], decodedKeys[:16])
	copy(iv[:], decodedKeys[16:32])

	auxEnc := EncryptImport(key, iv)
	decPayload, err := auxEnc.AESCbcDecrypt(data[rsaKeyLen:])
	if err != nil {
		return nil, err
	}

	return decPayload, nil
}
