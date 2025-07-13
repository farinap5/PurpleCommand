package lua

import (
	"fmt"
	"os"
	"purpcmd/server/implant"

	lua "github.com/yuin/gopher-lua"
)

var (
	CMDMAP = make(map[string]*command_def)
)

func LuaGetCommandDesc(impl, command string) [][]string {
	var aux [][]string
	for _, v := range CMDMAP {
		aux = append(aux, []string{
			v.Name, v.Description,
		})
	}
	return aux
}

func (l LuaProfile) command(L *lua.LState) int {
	impl := L.CheckString(1)
	name := L.CheckString(2)
	desc := L.CheckString(3)
	fn := L.CheckFunction(4) // Get function reference

	CMDMAP[impl+"."+name] = &command_def{
		Impl:        impl,
		Name:        name,
		Description: desc,
		ptr:         fn,
		ScriptName:  l.script,
	}

	return 0
}

func ImplantAddUploadFileCommand(L *lua.LState) int {
	code := L.CheckInt(1)
	fileSrcName := L.CheckString(2)
	fileDstName := L.CheckString(3)

	data, err := os.ReadFile(fileSrcName)
	if err != nil {
		L.Push(lua.LString("could not create task: error reading file"))
		return 0
	}

	errInt := implant.ImplantAddUploadTask(code, fileDstName, data)
	if errInt != 0 {
		//L.Push(lua.LNil)
		L.Push(lua.LString("could not create task"))
		return 0
	}
	L.Push(lua.LNil)

	return 0
}


func ImplantAddUploadCommand(L *lua.LState) int {
	code := L.CheckInt(1)
	data := L.CheckString(2)
	// Lua check string appears to be binary safe, so it must keep even \x00.
	chunk := L.CheckString(3)

	errInt := implant.ImplantAddUploadTask(code, data, []byte(chunk))
	if errInt != 0 {
		L.Push(lua.LString("could not create task"))
		return 0
	}
	L.Push(lua.LNil)

	return 0
}

func ImplantAddGenericCommand(L *lua.LState) int {
	code := L.CheckInt(1)
	payload := L.CheckString(2)

	errInt := implant.ImplantAddGenericTask(code, payload)
	if errInt != 0 {
		//L.Push(lua.LNil)
		L.Push(lua.LString("could not create task"))
		return 0
	}
	L.Push(lua.LNil)

	return 0
}

func CallCommand(name, impl, payload string) (string, error) {
	cmdStr, exists := CMDMAP[impl+"."+name]
	if !exists {
		return "", fmt.Errorf("command %s for %s not found", name, impl)
	}

	L := ScriptMAP[cmdStr.ScriptName].state
	L.Push(cmdStr.ptr)

	L.Push(lua.LString(payload))

	err := L.PCall(1, 1, nil)
	if err != nil {
		return "", err
	}

	ret := L.ToString(-1)
	L.Pop(1)

	return ret, nil
}
