package src

import (
	"io/ioutil"
	"net"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/crypto/ssh"
)



// Verify if unix socket file exist, if so delete it.
func (s Session) NormalizeSockFile() {
	_, err := os.Stat(s.SockName)
	if err != nil {
		return
	} else {
		err := os.Remove(s.SockName)
		if err != nil {
			println(err.Error())
			os.Exit(1)
		}
	}
}

// Configure window size
func SetWinSize(f *os.File, h, w int) {
	syscall.Syscall(syscall.SYS_IOCTL, 
		f.Fd(), 
		uintptr(syscall.TIOCSWINSZ),
		uintptr(unsafe.Pointer(&struct{ h, w, x, y uint16 }{uint16(h), uint16(w), 0, 0})))
}

// Hand challeng with publick key
func (s Session) pubCallBack(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	if s.AuthKeys[FingerprintKey(key)] {
		println("found")
		return &ssh.Permissions{},nil
	} else {
		println("not found")
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

	privkeyB, err := ioutil.ReadFile("./id_ecdsa")
	Err(err)

	pkey,err := ssh.ParsePrivateKey(privkeyB)
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