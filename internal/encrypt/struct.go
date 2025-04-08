package encrypt

import (
	"crypto/cipher"
	"crypto/rsa"
)

type Encrypt struct {
	hmackey [16]byte
	aeskey  [16]byte
	iv      [16]byte

	block cipher.Block

	RSAPublic 	*rsa.PublicKey
	RSAPrivate 	*rsa.PrivateKey
}
