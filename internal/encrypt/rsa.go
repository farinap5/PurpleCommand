package encrypt

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
)

func (e Encrypt) RSAEncode(data []byte) ([]byte, error) {
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
	rsaKeyLen := e.RSAPrivate.Size()
	if len(data) < rsaKeyLen {
		return nil, fmt.Errorf("invalid data length")
	}

	decodedKeys, err := rsa.DecryptPKCS1v15(rand.Reader, e.RSAPrivate, data[:rsaKeyLen])
	if err != nil {
		return nil, err
	}

	if len(decodedKeys) < 32 {
		return nil, fmt.Errorf("invalid decrypted key length")
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