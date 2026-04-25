package ssh

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "unsafe"

	"github.com/c-bata/go-prompt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

// Map the local variable "consoleWriter" to the one of go-prompt
//
//go:linkname consoleWriter github.com/c-bata/go-prompt.consoleWriter
var consoleWriter prompt.ConsoleWriter

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

func Connector(conn net.Conn) {
	consoleWriter.EraseLine() // Erase current line
	consoleWriter.EraseDown() // Required to remove the completions menu
	consoleWriter.EraseScreen()
	time.Sleep(1 * time.Second)
	if err := tunnel(conn); err != nil {
		fmt.Println("SSH tunnel error:", err)
	}
	syscall.Kill(syscall.Getpid(), syscall.SIGWINCH) // Required to force the re-render of the prompt
}

func tunnel(conn net.Conn) error {
	keyPath := "cmd/key/id_ecdsa"
	keuBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("tunnel: could not read SSH key from %s: %w", keyPath, err)
	}
	privKey, err := ssh.ParsePrivateKey(keuBytes)
	if err != nil {
		return fmt.Errorf("tunnel: could not parse SSH key: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(privKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// https://github.com/golang/go/issues/32990
	sshConn, channConn, connRequest, err := ssh.NewClientConn(conn, "localhost", sshConfig)
	if err != nil {
		return fmt.Errorf("tunnel: SSH handshake failed: %w", err)
	}

	client := ssh.NewClient(sshConn, channConn, connRequest)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("tunnel: could not open session: %w", err)
	}
	defer session.Close()

	fd := int(os.Stdin.Fd())
	state, err := terminal.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("tunnel: MakeRaw: %w", err)
	}
	defer terminal.Restore(fd, state)

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	w, h, err := terminal.GetSize(fd)
	if err != nil {
		return fmt.Errorf("tunnel: GetSize: %w", err)
	}
	err = session.RequestPty("xterm-256color", h, w, modes)
	if err != nil {
		return fmt.Errorf("tunnel: RequestPty: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("tunnel: StdinPipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("tunnel: StdoutPipe: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("tunnel: StderrPipe: %w", err)
	}

	go io.Copy(stdin, os.Stdin)
	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)

	go winChanges(session, os.Stdout.Fd())
	err = session.Shell()
	//utils.Err(err, 15)

	// https://gist.github.com/atotto/ba19155295d95c8d75881e145c751372
	/*
		From tests, it was seen that the session.shell() keeps waiting until the shell process exit
		and the channel is over so it jumps to the following Wait without even need this. So I will
		keep it commented.

			if err := session.Wait(); err != nil {
				if e, ok := err.(*ssh.ExitError); ok {
					switch e.ExitStatus() {
					case 130:
						return nil
					}
				}
				return fmt.Errorf("ssh: %s", err)
			}*/

	return nil
}
