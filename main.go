package main

import (
	"sshtest/src"
	"flag"
)

func main() {
	var ty = flag.String("t","serve","Listen")
	flag.Parse()

	if *ty == "serve" {
		go src.CallWSServer() // everse connection
		src.Listen() // listen ssh
	} else if *ty == "client" {
		src.WSServe() // tcp listener and websocket
	} else {
		println("no")
	}
}