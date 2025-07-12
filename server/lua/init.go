package lua

import (
	"purpcmd/server/db"
	"purpcmd/server/log"

	"github.com/yuin/gopher-lua"
)

var (
	ScriptMAP = make(map[string]*LuaProfile)
	CurrentScript string = "none"
)

func ScriptsReloadFromDB() {
	scripts, err := db.DBScriptGetAll()
	if err != nil {
		log.PrintErr(err.Error())
		return
	}
	for i := range scripts {
		LuaLoad(scripts[i])
	}
}

func LuaNew(path string) (*LuaProfile, error) {
	l := new(LuaProfile)
	l.script = path
	l.state = lua.NewState()

	l.state.OpenLibs()
	l.state.SetGlobal("command", l.state.NewFunction(l.command))
	l.state.SetGlobal("addtask", l.state.NewFunction(ImplantAddGenericCommand))
	l.state.SetGlobal("addtaskupload", l.state.NewFunction(ImplantAddUploadCommand))
	err := l.state.DoFile(path)

	return l, err
}

func LuaLoad(path string) {
	if ScriptMAP[path] != nil {
		log.PrintAlert("Script ", path, " already loaded")
		return
	}
	log.PrintInfo("Loading script ", path)

	l, err := LuaNew(path)
	if err != nil {
		log.PrintErr(err.Error())
		return
	}
	l.Running = true
	ScriptMAP[path] = l

	db.DBScriptInsert(path)

	go ScriptMAP[path].LuaRunMain()
}

func (l *LuaProfile)LuaRunMain() {
	err := l.state.DoString("Main()")
	if err != nil {
		println(err.Error())
		return
	}
}