package src

import (
	"encoding/binary"
	"io"
	//"os"
	"os/exec"
	//"syscall"
	//"unsafe"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)


// Hand ssh requests. it is in another file cause it gets bigger
func (s Session) HandServerConn(x string, chans <-chan ssh.NewChannel) {
	// Channel handles window size
	windowS := make(chan Window, 1)

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

				switch req.Type {
				case "shell", "exec":
					println("shell")
					cmd := exec.Command("/bin/bash","-i")

					ptm, err := pty.Start(cmd)
					Err(err)
					go func() {
						for win := range windowS {
							SetWinSize(ptm, win.Height, win.Width)
						}
					}()

					defer func() { ptm.Close() }()

					go io.Copy(ptm,channel)
					io.Copy(channel,ptm)
					
					channel.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
					return

				case "pty-req":
					ptyReq, ok := parsePtyRequest(req.Payload)
					if !ok {
						req.Reply(false, nil)
						continue
					}
					windowS <- ptyReq.Window
					req.Reply(ok,nil)

				case "env":
					req.Reply(true, nil)

				case "window-change":
					//var payload = struct{ Value string }{}
					win, ok := parseWinSizeReq(req.Payload)
					if ok {
						windowS <-win
					}
					req.Reply(true, nil)
				}
			}
		} (req)
	}
}


func parseUint32(in []byte) (uint32, []byte, bool) {
	if len(in) < 4 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(in), in[4:], true
}

func parseWinSizeReq(s []byte) (win Window, ok bool) {
	width32, s, ok := parseUint32(s)
	if width32 < 1 {
		ok = false
	}
	if !ok {
		return
	}
	height32, _, ok := parseUint32(s)
	if height32 < 1 {
		ok = false
	}
	if !ok {
		return
	}
	win = Window{
		Width:  int(width32),
		Height: int(height32),
	}
	println(win.Height, win.Width)
	return
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

func parsePtyRequest(s []byte) (pty Pty, ok bool) {
	term, s, ok := parseString(s)
	if !ok {
		return
	}
	width32, s, ok := parseUint32(s)
	if !ok {
		return
	}
	height32, _, ok := parseUint32(s)
	if !ok {
		return
	}
	pty = Pty{
		Term: term,
		Window: Window{
			Width:  int(width32),
			Height: int(height32),
		},
	}
	return
}