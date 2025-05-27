package ssh

import (
	"crypto/sha256"
	"encoding/base64"
	"log"

	"golang.org/x/crypto/ssh"
)


func FingerprintKey(k ssh.PublicKey) string {
	bytes := sha256.Sum256(k.Marshal())
	return base64.StdEncoding.EncodeToString(bytes[:])
}

// Hand challenge with publick key
func (s Session) pubCallBack(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	if s.AuthKeys[FingerprintKey(key)] {
		log.Printf("Key %s found.",FingerprintKey(key))
		return &ssh.Permissions{},nil
	} else {
		log.Printf("Key %s not found.",FingerprintKey(key))
		return nil, nil
	}
}