package src

import (
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

// WebSocket Server Works with SSH local mirror
func WSServe() {
	addrTCP :=  "0.0.0.0:8080"
	adds := "0.0.0.0:8081"

	up := websocket.Upgrader{}
	http.HandleFunc("/",func(w http.ResponseWriter, r *http.Request){
		conn,err := up.Upgrade(w,r,nil)
		if err != nil {
			fmt.Println(err.Error())
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