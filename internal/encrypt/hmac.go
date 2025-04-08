package encrypt

import (
	"crypto/hmac"
	"crypto/sha256"
)

func (e *Encrypt) HMACGenerateHash(pack []byte) []byte {
	mac := hmac.New(sha256.New, e.hmackey[:])
	mac.Write(pack)
	return mac.Sum(nil)[:16]
}

func (e *Encrypt) HMACVerifyHash(pack []byte) bool {
	mac := hmac.New(sha256.New, e.hmackey[:])
	mac.Write(pack[:len(pack)-16])
	expectedMAC := mac.Sum(nil)[:16]
	return hmac.Equal(expectedMAC, pack[len(pack)-16:])
}


func (e *Encrypt) HMACPackAddHmac(pack *[]byte) {
	*pack = append(*pack, e.HMACGenerateHash(*pack)...)
}
