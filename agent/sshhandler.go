package agent

import (
	"encoding/binary"
	"io"
	"log"
	"os/exec"
	"purpcmd/utils"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

// Hand ssh requests. it is in another file cause it gets bigger
func (s Session) HandServerConn(x string, chans <-chan ssh.NewChannel) {

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unknow channel type")
			continue
		}

		channel, req, err := newChan.Accept()
		utils.Err(err)

		f, tty, err := pty.Open()
		defer tty.Close()
		if err != nil {
			utils.Err(err)
		}

		go func(in <-chan *ssh.Request) {
			defer channel.Close()
			for req := range in {
				//ok := false
				switch req.Type {
				// TODO: exec is not needed. Must be created another case for exec call.
				case "shell", "exec":
					log.Println("Client got shell")

					// TODO: use a shell existing in the system / default shell
					cmd := exec.Command("/bin/bash", "-i")

					cmd.Stdout = tty
					cmd.Stdin = tty
					cmd.Stderr = tty

					cmd.SysProcAttr = &syscall.SysProcAttr{
						Setctty: true,
						Setsid:  true,
					}

					err = cmd.Start()
					utils.Err(err)

					/*var once sync.Once
					close := func() {
						channel.Close()
						log.Printf("session closed")
					}*/

					go func() {
						io.Copy(channel, f)
						//once.Do(close)
					}()

					go func() {
						io.Copy(f, channel)
						//once.Do(close)
					}()

					go func() {
						err := cmd.Wait()
						if err != nil {
							log.Printf("Shell exited with error %s", err.Error())
						} else {
							log.Println("Shell exited")
						}
						channel.Close()
					}()

					//channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})

				case "pty-req":
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
					req.Reply(true, nil)
					//println("Window")
				}

				//req.Reply(ok, nil)
			}
		}(req)
	}
}

func parseString(in []byte) (out string, rest []byte, ok bool) {
	if len(in) < 4 {
		return
	}
	length := binary.BigEndian.Uint32(in)
	if uint32(len(in)) < 4+length {
		return
	}
	out = string(in[4 : 4+length])
	rest = in[4+length:]
	ok = true
	return
}

func SetWinsize(fd uintptr, w, h uint32) {
	ws := &WindowsConf{Width: uint16(w), Height: uint16(h)}
	syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(ws)))
}
