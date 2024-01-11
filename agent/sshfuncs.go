package agent

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"log"
	"net"
	"os"
	"purpcmd/utils"

	"golang.org/x/crypto/ssh"
)


func FingerprintKey(k ssh.PublicKey) string {
	bytes := sha256.Sum256(k.Marshal())
	return base64.StdEncoding.EncodeToString(bytes[:])
}

// Verify if unix socket file exist, if so delete it.
func (s Session) NormalizeSockFile() {
	_, err := os.Stat(s.SockName)
	if err != nil {
		return
	} else {
		err := os.Remove(s.SockName)
		if err != nil {
			log.Println(err.Error())
			os.Exit(1)
		}
	}
}

// Hand challeng with publick key
func (s Session) pubCallBack(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	if s.AuthKeys[FingerprintKey(key)] {
		log.Printf("Key %s found.",FingerprintKey(key))
		return &ssh.Permissions{},nil
	} else {
		log.Printf("Key %s not found.",FingerprintKey(key))
		return nil, nil
	}
}


func Listen(key embed.FS) {
	// Basic setup
	s := Session{
		SockName: "/tmp/ssh.sock",
		AuthKeys: make(map[string]bool),
	}
	var err error

	// TODO: Make pubkey embed to the binary
	s.PubKey, _, _, _, err = ssh.ParseAuthorizedKey([]byte("ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBDm7lFJASftWM9Bmw+sQnjNtr48wXhSRDf43XUhbfRBT05j5dZ4+2qUhPt5gugkECSINzOs2nGz0hkCFTGDqPIM="))
	utils.Err(err)

	// Keep the fingerprint for authentication
	s.AuthKeys[FingerprintKey(s.PubKey)] = true
	
	config := &ssh.ServerConfig {
		PublicKeyCallback: s.pubCallBack, // Challenge with pubkey
	}

	privKey,_ := key.ReadFile("utils/key/id_ecdsa")
	

	pkey,err := ssh.ParsePrivateKey(privKey)
	utils.Err(err)
	config.AddHostKey(pkey)

	s.NormalizeSockFile()
	listener, err := net.Listen("unix",s.SockName)
	utils.Err(err)
	defer listener.Close()

	AConn, err := listener.Accept()
	utils.Err(err)

	conn, chans, reqs, err := ssh.NewServerConn(AConn, config)
	utils.Err(err)
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	s.HandServerConn(conn.Permissions.Extensions["x"],chans)
}