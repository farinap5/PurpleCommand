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
	l.state.SetGlobal("command", l.state.NewFunction(l.command))
	l.state.DoFile(path)

	err := l.state.DoFile(path)
	return l, err
}

func LuaLoad(path string) {
	l, err := LuaNew(path)
	if err != nil {
		println(err.Error())
		return
	}
	l.Running = true
	ScriptMAP[path] = l

	log.PrintInfo("Loading script ", path)
	go ScriptMAP[path].LuaRunMain()
}

func (l *LuaProfile)LuaRunMain() {
	err := l.state.DoString("Main()")
	if err != nil {
		println(err.Error())
		return
	}
}

func (l *LuaProfile) command(L *lua.LState) int {
	name := L.CheckString(1)
	desc := L.CheckString(2)
	//fn := L.CheckFunction(2)  // Get function reference

	println(name,desc)
	return 0
}