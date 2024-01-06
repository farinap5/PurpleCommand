package src

import (
	"encoding/binary"
	"io"
	"sync"
	"syscall"
	"unsafe"
	"os/exec"
	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

// Hand ssh requests. it is in another file cause it gets bigger
func (s Session) HandServerConn(x string, chans <-chan ssh.NewChannel) {

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType,"unknow channel type")
			continue
		}

		channel, req, err := newChan.Accept()
		Err(err)

		f, tty, err := pty.Open()
		if err != nil {
			Err(err)
		}


		go func(in <-chan *ssh.Request) {
			defer channel.Close()
			for req := range in {

				switch req.Type {
				// TODO: exec is not needed. Must be created another case for exec call.
				case "shell", "exec":
					println("shell")

					// TODO: use a shell existing in the system / default shell
					cmd := exec.Command("/bin/bash","-i")

					cmd.Stdout = tty
					cmd.Stdin = tty
					cmd.Stderr = tty

					cmd.SysProcAttr = &syscall.SysProcAttr{
						Setctty: true,
						Setsid:  true,
					}

					err = cmd.Start()
					Err(err)

					var once sync.Once
					close := func() {
						channel.Close()
					}

					go func() {
						io.Copy(channel, f)
						once.Do(close)
					}()

					go func() {
						io.Copy(f, channel)
						once.Do(close)
					}()
					
					channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				case "pty-req":
					newTermLen := req.Payload[3]
					w := binary.BigEndian.Uint32(req.Payload[newTermLen+4:])
					h := binary.BigEndian.Uint32(req.Payload[newTermLen+4:][4:])
					SetWinsize(f.Fd(), w, h)
					req.Reply(true,nil)
				case "env":
					req.Reply(true, nil)

				case "window-change":
					w := binary.BigEndian.Uint32(req.Payload)
					h := binary.BigEndian.Uint32(req.Payload[4:])
					SetWinsize(f.Fd(), w, h)
					req.Reply(true, nil)
				}
			}
		} (req)
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