package encrypt

import (
	"crypto/aes"
	"crypto/rand"
	"io"
)

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
	May be used by the server to load config from
	the implant session.
*/
func EncryptImport(key, iv [16]byte) Encrypt {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err.Error())
	}

	return Encrypt{
		aeskey: key,
		iv: iv,
		hmackey: xor(key, iv),

		block: block,
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
