package server

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"purpcmd/utils"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

// https://github.com/glinton/ssh/blob/master/client.go#L293
func termSize(fd uintptr) []byte {
	size := make([]byte, 16)

	w, h, err := terminal.GetSize(int(fd))
	/*
		W        H
		ffffffff ffffffff ffffffffffffffff
	*/
	if err != nil {
		binary.BigEndian.PutUint32(size, uint32(80))
		binary.BigEndian.PutUint32(size[4:], uint32(24))
		return size
	}

	binary.BigEndian.PutUint32(size, uint32(w))
	binary.BigEndian.PutUint32(size[4:], uint32(h))

	return size
}

func winChanges(session *ssh.Session, fd uintptr) {
	signals := make(chan os.Signal, 1)

	signal.Notify(signals, syscall.SIGWINCH)
	defer signal.Stop(signals)

	for range signals {
		session.SendRequest("window-change", false, termSize(fd))
	}
}

func Connector(conn net.Conn, keyPath string) error {
	var bytes []byte
	var err error
	if keyPath == "" {
		bytes, err = Key.ReadFile("key/id_ecdsa")
	} else {
		bytes, err = ioutil.ReadFile(keyPath)
	}
	utils.Err(err)

	privKey, err := ssh.ParsePrivateKey(bytes)
	utils.Err(err)

	sshConfig := &ssh.ClientConfig{
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(privKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// https://github.com/golang/go/issues/32990
	sshConn, channConn, connRequest, err := ssh.NewClientConn(conn, "localhost", sshConfig)
	utils.Err(err)

	/*
		TODO: make HostKeyCallback
		https://stackoverflow.com/questions/44269142/golang-ssh-getting-must-specify-hoskeycallback-error-despite-setting-it-to-n
	*/
	/*sshConfig := &ssh.ClientConfig{
		Auth: []ssh.AuthMethod{ssh.PublicKeys(privKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", "0.0.0.0:8080", sshConfig)
	utils.Err(err)
	defer client.Close()*/

	client := ssh.NewClient(sshConn, channConn, connRequest)
	defer client.Close()
	//client

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		utils.Err(err)
	}

	defer session.Close()

	fd := int(os.Stdin.Fd())
	state, err := terminal.MakeRaw(fd)
	utils.Err(err)
	defer terminal.Restore(fd, state)

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	w, h, err := terminal.GetSize(fd)
	utils.Err(err)
	err = session.RequestPty("xterm-256color", h, w, modes)
	utils.Err(err)

	///fmt.Println("Setting up STDIN\r")
	stdin, err := session.StdinPipe()
	utils.Err(err)
	stdout, err := session.StdoutPipe()
	utils.Err(err)
	stderr, err := session.StderrPipe()
	utils.Err(err)

	go io.Copy(stdin, os.Stdin)
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)

	go winChanges(session, os.Stdout.Fd())
	print("Call Shell\n\r")
	err = session.Shell()
	utils.Err(err)

	// https://gist.github.com/atotto/ba19155295d95c8d75881e145c751372
	if err := session.Wait(); err != nil {
		if e, ok := err.(*ssh.ExitError); ok {
			switch e.ExitStatus() {
			case 130:
				return nil
			}
		}
		return fmt.Errorf("ssh: %s", err)
	}

	return nil
}
