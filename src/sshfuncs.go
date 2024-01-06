package src

import (
	"embed"
	"log"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

//go:embed key
var key embed.FS

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


func Listen() {
	// Basic setup
	s := Session{
		SockName: "/tmp/ssh.sock",
		AuthKeys: make(map[string]bool),
	}
	var err error

	s.PubKey, _, _, _, err = ssh.ParseAuthorizedKey([]byte("ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBDm7lFJASftWM9Bmw+sQnjNtr48wXhSRDf43XUhbfRBT05j5dZ4+2qUhPt5gugkECSINzOs2nGz0hkCFTGDqPIM="))
	Err(err)

	// Keep the fingerprint for authentication
	s.AuthKeys[FingerprintKey(s.PubKey)] = true
	
	config := &ssh.ServerConfig {
		PublicKeyCallback: s.pubCallBack, // Challenge with pubkey
	}

	privKey,_ := key.ReadFile("key/id_ecdsa")
	

	pkey,err := ssh.ParsePrivateKey(privKey)
	Err(err)
	config.AddHostKey(pkey)

	s.NormalizeSockFile()
	listener, err := net.Listen("unix",s.SockName)
	Err(err)
	defer listener.Close()

	AConn, err := listener.Accept()
	Err(err)

	conn, chans, reqs, err := ssh.NewServerConn(AConn, config)
	Err(err)
	go ssh.DiscardRequests(reqs)
	s.HandServerConn(conn.Permissions.Extensions["x"],chans)
}