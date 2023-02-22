package src

import (
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

func normalizeSockFile(n string) {
	_, err := os.Stat(n)
	if err != nil {
		return
	} else {
		err := os.Remove(n)
		if err != nil {
			println(err.Error())
			os.Exit(1)
		}
	}
}

var AuthorizedKeysMap = map[string]bool{}

func pubCallBack(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	//println(string(key.Marshal()))
	if AuthorizedKeysMap[FingerprintKey(key)] {
		println("found")
		return &ssh.Permissions{},nil
	} else {
		println("not found")
		return nil, nil
	}
}

func handServerConn(x string, chans <-chan ssh.NewChannel) {
	// println("aaaa")
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType,"unknow channel type")
			continue
		}

		channel, req, err := newChan.Accept()
		Err(err)

		go func(in <-chan *ssh.Request) {
			defer channel.Close()
			for req := range in {
				println(req.Type,req.Payload)
				switch req.Type {
				case "shell":
					println("shelllll")
					cmd := exec.Command("/bin/bash","-i")
					ptm, err := pty.Start(cmd)
					Err(err)
					defer func() { _ = ptm.Close() }()

					go io.Copy(ptm,channel)
					io.Copy(channel,ptm)
					
					channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
					return
				case "pty-req":
					req.Reply(true, nil)
					println("execcccc")
					println(string(req.Payload))
					cmd := exec.Command("/bin/bash","-i")
					ptm, err := pty.Start(cmd)
					Err(err)
					defer func() { _ = ptm.Close() }()

					go io.Copy(ptm,channel)
					io.Copy(channel,ptm)
					
					channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
					return
				}
			}
		} (req)
	}
}

func Listen() {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte("ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBDm7lFJASftWM9Bmw+sQnjNtr48wXhSRDf43XUhbfRBT05j5dZ4+2qUhPt5gugkECSINzOs2nGz0hkCFTGDqPIM="))
	Err(err)
	AuthorizedKeysMap[FingerprintKey(pubKey)] = true
	
	config := &ssh.ServerConfig {
		PublicKeyCallback: pubCallBack,
	}

	privkeyB, err := ioutil.ReadFile("./id_ecdsa")
	Err(err)

	pkey,err := ssh.ParsePrivateKey(privkeyB)
	Err(err)
	config.AddHostKey(pkey)

	println("0.0.0.0:2222")

	fileName := "/tmp/ssh.sock"
	normalizeSockFile(fileName)
	listener, err := net.Listen("unix",fileName)
	
	//listener, err := net.Listen("tcp","0.0.0.0:2222")
	Err(err)
	defer listener.Close()

	AConn, err := listener.Accept()
	Err(err)

	//println("aaaa")
	conn, chans, reqs, err := ssh.NewServerConn(AConn, config)
	Err(err)
	go ssh.DiscardRequests(reqs)
	handServerConn(conn.Permissions.Extensions["x"],chans)
}