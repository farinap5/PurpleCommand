package core

import (
	"purpcmd/internal/encrypt"
	"purpcmd/server/db"
	"purpcmd/server/listener"
	"purpcmd/server/log"
	"purpcmd/server/lua"
)

// RSAKeyPath is the path to the server RSA private key PEM file.
// Override before calling Start() if using a non-default location.
var RSAKeyPath = "server.key"

func Start() {
	if err := encrypt.LoadServerRSAKey(RSAKeyPath); err != nil {
		log.PrintAlert("RSA key not loaded: " + err.Error())
		log.PrintAlert("Generate a key with: openssl genrsa -out server.key 2048")
	}

	err := db.CheckDB()
	if err != nil {
		log.PrintAlert(err.Error())
		return
	}
	lua.ScriptsReloadFromDB()
	err = listener.ListenerInitFromDB()
	if err != nil {
		log.PrintAlert(err.Error())
		return
	}

	Banner()
	InitCLI()
}
