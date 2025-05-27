package core

import (
	"purpcmd/server/db"
	"purpcmd/server/listener"
	"purpcmd/server/log"
	"purpcmd/server/lua"
)

func Start() {
	err := db.CheckDB()
	if err != nil {
		log.PrintAlert(err.Error())
		return
	}
	err = listener.ListenerInitFromDB()
	if err != nil {
		log.PrintAlert(err.Error())
		return
	}
	lua.ScriptsReloadFromDB()

	InitCLI()
}