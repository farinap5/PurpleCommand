package encrypt

import "crypto/cipher"

type Encrypt struct {
	hmackey [16]byte
	aeskey  [16]byte
	iv      [16]byte

	block cipher.Block
}
