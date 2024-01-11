package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"purpcmd/utils"

	"github.com/gorilla/websocket"
)

// WebSocket Server Works with SSH local mirror
func WSServe(adds string) {
	addrTCP :=  "0.0.0.0:8080"

	up := websocket.Upgrader{}
	http.HandleFunc("/",func(w http.ResponseWriter, r *http.Request){
		conn,err := up.Upgrade(w,r,nil)
		if err != nil {
			fmt.Println(err.Error())
			return
		}

		log.Printf("TCP Listener up on %s", addrTCP)
		ln, err := net.Listen("tcp",addrTCP)
		if err != nil {
			panic(err)
		}
		log.Printf("+ Connect to %s with SSH", addrTCP)
		defer ln.Close()
		channel, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		defer channel.Close()


		webSockConn := utils.New(conn) // New addapter
		log.Println("Proxy connected", addrTCP)
		go utils.CopyIO(channel, webSockConn)
		utils.CopyIO(webSockConn, channel)
	})

	log.Printf("Listening on ws://%s/", adds)
	http.ListenAndServe(adds ,nil)
}