// +build windows

package agent

import (
	//"encoding/binary"
	"io"
	"log"
	"os/exec"
	"purpcmd/utils"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

func (s Session) HandServerConn(x string, chans <-chan ssh.NewChannel) {
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, req, err := newChan.Accept()
		utils.Err(err, 1)

		cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile")

		f, tty, err := pty.Open()
		if err != nil {
			utils.Err(err, 2)
		}
		defer tty.Close()

		cmd.Stdout = tty
		cmd.Stdin = tty
		cmd.Stderr = tty

		err = cmd.Start()
		utils.Err(err, 3)

		go func(in <-chan *ssh.Request) {
			defer channel.Close()
			for req := range in {
				switch req.Type {
				case "shell", "exec":
					log.Println("Client got PowerShell")

					go io.Copy(channel, f)
					go io.Copy(f, channel)

					go func() {
						err := cmd.Wait()
						if err != nil {
							utils.Err(err, 4)
						} else {
							log.Println("PowerShell exited")
						}
						channel.Close()
					}()

				/*case "pty-req":
					newTermLen := req.Payload[3]
					w := binary.BigEndian.Uint32(req.Payload[newTermLen+4:])
					h := binary.BigEndian.Uint32(req.Payload[newTermLen+4:][4:])
					SetWinsize(f.Fd(), w, h)
					req.Reply(true, nil)

				case "env":
					req.Reply(true, nil)

				case "window-change":
					w := binary.BigEndian.Uint32(req.Payload)
					h := binary.BigEndian.Uint32(req.Payload[4:])
					SetWinsize(f.Fd(), w, h)
					req.Reply(true, nil)*/
				}
			}
		}(req)
	}
}
