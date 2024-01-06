// https://groups.google.com/g/gorilla-web/c/VjXmApL1qA8

package src

import (
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/gorilla/websocket"
)


func CallWSServer(remoteAdd string) {
	var t time.Duration = 1
	var c int = 0
	for {
		err := wsclient(remoteAdd)
		if err != nil {
			log.Printf("Try %d sleep for %d", c, t)
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

func wsclient(remoteAdd string) error {
	log.Printf("Connecting to ws://%s/", remoteAdd)

	wclient, _, err := websocket.DefaultDialer.Dial("ws://"+remoteAdd+"/",nil)
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
	// "New" from adapter to use websock as net.Conn
	webSockConn := New(wclient)
	log.Println("+ Proxy connected")
	go copyIO(conn, webSockConn)
	copyIO(webSockConn, conn)
	return nil
}

// sync io from those connectios
func copyIO(src, dest net.Conn) {
	defer src.Close()
	defer dest.Close()
	io.Copy(src, dest)
}
