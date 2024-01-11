package server

import (
	"embed"
	"fmt"
	"io"
	"os"
	"purpcmd/utils"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)


func Connector(key embed.FS) error {
	bytes, err := key.ReadFile("utils/key/id_ecdsa")
	utils.Err(err)

	privKey, err := ssh.ParsePrivateKey(bytes)
	utils.Err(err)

	/* 
		TODO: make HostKeyCallback 
		https://stackoverflow.com/questions/44269142/golang-ssh-getting-must-specify-hoskeycallback-error-despite-setting-it-to-n
	*/
	sshConfig := &ssh.ClientConfig{
		Auth: []ssh.AuthMethod{ssh.PublicKeys(privKey)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", "0.0.0.0:8080", sshConfig)
	utils.Err(err)
	defer client.Close()

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
	

	fmt.Println("Setting up STDIN")
	stdin, err := session.StdinPipe()
	utils.Err(err)
	go io.Copy(stdin, os.Stdin)

	fmt.Println("Setting up STDOUT")
	stdout, err := session.StdoutPipe()
	utils.Err(err)
	go io.Copy(os.Stdout, stdout)

	fmt.Println("Setting up STDERR")
	stderr, err := session.StderrPipe()
	utils.Err(err)
	go io.Copy(os.Stderr, stderr)

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