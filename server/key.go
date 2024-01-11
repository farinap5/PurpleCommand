package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
)



func GeneratePrivKey() []byte {
	privkey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		println(err.Error())
	}
	// Get ASN.1 DER format
	privDER, err := x509.MarshalECPrivateKey(privkey)
	if err != nil {
		println(err.Error())
	}
	// pem.Block
	privBlock := pem.Block{
		Type:    "EC PRIVATE KEY",
		Headers: nil,
		Bytes:   privDER,
	}

	// Private key in PEM format
	privatePEM := pem.EncodeToMemory(&privBlock)

	return privatePEM
}

func encodePubKey(privatekey *ecdsa.PublicKey) []byte {
	pubkey, err := ssh.NewPublicKey(privatekey)
	if err != nil {
		println(err.Error())
	}

	return ssh.MarshalAuthorizedKey(pubkey)
}
