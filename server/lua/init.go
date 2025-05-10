package lua

import (
	"purpcmd/server/log"

	"github.com/yuin/gopher-lua"
)

var (
	ScriptMAP = make(map[string]*LuaProfile)
	CurrentScript string = "none"
)

func LuaNew(path string) (*LuaProfile, error) {
	l := new(LuaProfile)
	l.script = path
	l.state = lua.NewState()

	l.state.OpenLibs()
	l.state.SetGlobal("command", l.state.NewFunction(command))
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
		println(err.Error())
		return
	}
	l.Running = true
	ScriptMAP[path] = l

	go ScriptMAP[path].LuaRunMain()
}

func (l *LuaProfile)LuaRunMain() {
	err := l.state.DoString("Main()")
	if err != nil {
		println(err.Error())
		return
	}
}