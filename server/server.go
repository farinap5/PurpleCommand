package server

import (
	"embed"
	"flag"
	"fmt"
	"log"

	//"net"
	"net/http"
	"purpcmd/utils"

	"github.com/gorilla/websocket"
)

var Key embed.FS

func (profile *ServerProfile)websockhand(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{}
	conn,err := up.Upgrade(w,r,nil)
	if err != nil {
		fmt.Println(err.Error())
		return
	}


	/*log.Printf("TCP Listener up on %s", profile.TCPDefaultAddress)
	ln, err := net.Listen("tcp", profile.TCPDefaultAddress)
	if err != nil {
		panic(err)
	}
	log.Printf("+ Connect to %s with SSH", profile.TCPDefaultAddress)
	defer ln.Close()
	channel, err := ln.Accept()
	if err != nil {
		panic(err)
	}
	defer channel.Close()*/

	
	webSockConn := utils.New(conn) // New addapter
	log.Println("Proxy connected", profile.TCPDefaultAddress)
	Connector(webSockConn)
	/*defer webSockConn.Close()
	go utils.CopyIO(channel, webSockConn)
	utils.CopyIO(webSockConn, channel)*/
}

// WebSocket Server Works with SSH local mirror
func WSServe(args []string, key embed.FS) error {
	Key = key
	profile := new(ServerProfile)
	profile.TCPDefaultAddress = "0.0.0.0:8080"

	flags := flag.NewFlagSet("server", flag.ContinueOnError)

	flags.StringVar(&profile.HTTPAddress, "a", "", "")
	var uri = flags.String("uri","/","URI")

	flags.Usage = utils.Usage
	flags.Parse(args)


	ServerMux := http.NewServeMux()

	ServerMux.HandleFunc(*uri, profile.websockhand)

	log.Printf("Listening on ws://%s%s", profile.HTTPAddress, *uri)
	server := http.Server{
		Addr: profile.HTTPAddress,
		Handler: ServerMux,
	}

	return server.ListenAndServe()
}