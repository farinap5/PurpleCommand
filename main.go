package main

import (
	"flag"
	"fmt"
	"os"

	"purpcmd/agent"
	"purpcmd/server"
)

var Usage = func() {
	fmt.Printf("Usage of %s:  \n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	var l = flag.Bool("l",false, "Listen for incomming connections.")
	var c = flag.Bool("c",false, "Connect to the server.")
	var r = flag.String("r","0.0.0.0:8081","Remote address")
	var a = flag.String("a","0.0.0.0:8081","Local address")
	flag.Usage = Usage
	flag.Parse()

	if *c {
		go agent.CallWSServer(*r) // everse connection
		agent.Listen() // Listen ssh
	} else if *l {
		server.WSServe(*a) // tcp listener and websocket
	} else {
		Usage()
	}
}