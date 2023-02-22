// https://groups.google.com/g/gorilla-web/c/VjXmApL1qA8

package src

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	//"flag"
	"github.com/gorilla/websocket"
)

/*func main() {
	var x = flag.String("t","client","client/serve")
	flag.Parse()
	if *x == "client" {
		TCPListen()
	} else if *x == "serve" {
		WCServe()
	} else {
		println("No option")
	}
}*/

//var channel net.Conn

// SSH local mirror
/*func TCPListen() {

}*/

// WebSocket Server Works with SSH local mirror
func WSServe() {
	addrTCP :=  "0.0.0.0:8080"
	adds := "0.0.0.0:8081"

	up := websocket.Upgrader{}
	http.HandleFunc("/",func(w http.ResponseWriter, r *http.Request){
		conn,err := up.Upgrade(w,r,nil)
		if err != nil {
			fmt.Println("aaaa")
			return
		}

		println("TCP Listener: "+addrTCP)
		ln, err := net.Listen("tcp",addrTCP)
		if err != nil {
			panic(err)
		}
		defer ln.Close()
		channel, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		defer channel.Close()


		z := New(conn) // New addapter
		fmt.Println("proxy connected")
		go copyIO(channel, z)
		copyIO(z, channel)
	})

	println("Listening Web Sockets on "+adds)
	http.ListenAndServe(adds ,nil)
}

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
