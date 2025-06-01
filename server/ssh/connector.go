package ssh

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"os/signal"
	"purpcmd/server/utils"
	"syscall"
	"time"
	_ "unsafe"

	"github.com/c-bata/go-prompt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

// Map the local variable "consoleWriter" to the one of go-prompt
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
	consoleWriter.EraseScreen()
	consoleWriter.EraseLine() // Erase current line
	consoleWriter.EraseDown() // Required to remove the completions menu
	time.Sleep(1 * time.Second)
	tunnel(conn)
	syscall.Kill(syscall.Getpid(), syscall.SIGWINCH) // Required to force the re-render of the prompt
}

func tunnel(conn net.Conn) error {
	id_ecdsa := `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAaAAAABNlY2RzYS
1zaGEyLW5pc3RwMjU2AAAACG5pc3RwMjU2AAAAQQQ5u5RSQEn7VjPQZsPrEJ4zba+PMF4U
kQ3+N11IW30QU9OY+XWePtqlIT7eYLoJBAkiDczrNpxs9IZAhUxg6jyDAAAAqC+nArwvpw
K8AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBDm7lFJASftWM9Bm
w+sQnjNtr48wXhSRDf43XUhbfRBT05j5dZ4+2qUhPt5gugkECSINzOs2nGz0hkCFTGDqPI
MAAAAgUnybP9gbz6kYON6APaQXd+MVK1jXVSVkMJ+fnUSGq+oAAAALZmFyaW5hcEB4eXoB
AgMEBQ==
-----END OPENSSH PRIVATE KEY-----`
	
	keuBytes := []byte(id_ecdsa)
	privKey, err := ssh.ParsePrivateKey(keuBytes)
	utils.Err(err, 6)

	sshConfig := &ssh.ClientConfig{
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(privKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// https://github.com/golang/go/issues/32990
	sshConn, channConn, connRequest, err := ssh.NewClientConn(conn, "localhost", sshConfig)
	utils.Err(err, 7)

	client := ssh.NewClient(sshConn, channConn, connRequest)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		utils.Err(err, 8)
	}

	defer session.Close()

	fd := int(os.Stdin.Fd())
	state, err := terminal.MakeRaw(fd)
	utils.Err(err, 9)
	defer terminal.Restore(fd, state)

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	w, h, err := terminal.GetSize(fd)
	utils.Err(err, 10)
	err = session.RequestPty("xterm-256color", h, w, modes)
	utils.Err(err, 11)

	stdin, err := session.StdinPipe()
	utils.Err(err, 12)
	stdout, err := session.StdoutPipe()
	utils.Err(err, 13)
	stderr, err := session.StderrPipe()
	utils.Err(err, 14)

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
