package ssh

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"log"

	"golang.org/x/crypto/ssh"
)

// GeneratePrivKey generates an ephemeral ECDSA P-256 private key for use as
// the implant's SSH host key.  A fresh key is generated on each run.
func GeneratePrivKey() []byte {
	privkey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	privDER, err := x509.MarshalECPrivateKey(privkey)
	if err != nil {
		log.Fatal(err)
	}
	privBlock := pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privDER,
	}
	return pem.EncodeToMemory(&privBlock)
}

func FingerprintKey(k ssh.PublicKey) string {
	b := sha256.Sum256(k.Marshal())
	return base64.StdEncoding.EncodeToString(b[:])
}

// pubCallBack authenticates connecting clients by public key fingerprint.
func (s Session) pubCallBack(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	if s.AuthKeys[FingerprintKey(key)] {
		log.Printf("Key %s found.", FingerprintKey(key))
		return &ssh.Permissions{}, nil
	}
	log.Printf("Key %s not found.", FingerprintKey(key))
	return nil, nil
}
