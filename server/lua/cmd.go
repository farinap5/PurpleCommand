package lua

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

var (
	CMDMAP = make(map[string]*command_def)
)

func command(L *lua.LState) int {
	impl := L.CheckString(1)
	name := L.CheckString(2)
	desc := L.CheckString(3)
	fn := L.CheckFunction(4)  // Get function reference

	CMDMAP[impl + "." + name] = &command_def{
		Impl: impl,
		Name: name,
		Description: desc,
		ptr: fn,
	}

	return 0
}

func CallCommand(name, impl string) (string, error) {
	cmdStr, exists := CMDMAP[impl + "." + name]
	if !exists {
		return "", fmt.Errorf("command %s for %s not found", name, impl)
	}


	L := ScriptMAP[cmdStr.ScriptName].state
	L.Push(*cmdStr.ptr)
	err := L.PCall(0, 1, nil)
	if err != nil {
		return "", err
	}

	ret := L.ToString(-1)
	L.Pop(1)

	return ret, nil
}