package main

import (
	"embed"
	"flag"
	"os"

	"purpcmd/agent"
	"purpcmd/server"
	"purpcmd/utils"
)

//go:embed key
var key embed.FS

func main() {
	args := flag.Args()
	flag.Usage = utils.Usage
	flag.Parse()

	args = flag.Args()

	action := ""

	if len(args) > 0 {
		action = args[0]
		args = args[1:]
	}

	switch action {
		case "server":
			server.WSServe(args, key) // tcp listener and websocket
		case "client":
			// go agent.CallWSServer(*a) // everse connection
			// agent.Listen(key) // Listen ssh
			agent.CallWSServer(args, key)
		default:
			utils.Usage()
			os.Exit(0)
	}
}