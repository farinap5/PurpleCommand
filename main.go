package main

import (
	"flag"
	"fmt"
	"os"
	"embed"

	"purpcmd/agent"
	"purpcmd/server"
)

//go:embed utils/key
var key embed.FS

var Usage = func() {
	fmt.Printf("Usage of %s:  \n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	var l = flag.Bool("l",false, "Listen for incomming connections.")
	var c = flag.Bool("c",false, "Connect to the server.")
	//var t = flag.Bool("t",false, "SSH client connector.")
	var a = flag.String("a","0.0.0.0:8081","Address")
	flag.Usage = Usage
	flag.Parse()

	if *c {
		go agent.CallWSServer(*a) // everse connection
		agent.Listen(key) // Listen ssh
	} else if *l {
		profile := new(server.ServerProfile)
		profile.HTTPAddress = *a
		server.WSServe(*a, key) // tcp listener and websocket
	} else {
		Usage()
	}
}