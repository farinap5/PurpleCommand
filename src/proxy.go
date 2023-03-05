// https://groups.google.com/g/gorilla-web/c/VjXmApL1qA8

package src

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"
	"github.com/gorilla/websocket"
)


func CallWSServer() {
	var t time.Duration = 1
	var c int = 0
	for {
		err := wsclient()
		if err != nil {
			println(c," sleep:",t)
			time.Sleep(t * time.Millisecond)
			if t >= 131072 {
				continue
			} else {
				t *= 2 
			}
		} else {
			break
		}
		if c == 25 {
			break
		}
		c++
	}
}

func wsclient() error {
	fmt.Println("new client")

	wclient, _, err := websocket.DefaultDialer.Dial("ws://0.0.0.0:8081/",nil)
	if err != nil {
		return err
	}
	defer wclient.Close()

	conn, err := net.Dial("unix", "/tmp/ssh.sock")
	if err != nil {
		println(err.Error())
		os.Exit(1)
		return err
	}

	// create new connect file
	z := New(wclient)
	fmt.Println("proxy connected")
	go copyIO(conn, z)
	copyIO(z, conn)
	return nil
}


func copyIO(src, dest net.Conn) {
	defer src.Close()
	defer dest.Close()
	io.Copy(src, dest)
}
