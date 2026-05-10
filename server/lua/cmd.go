package lua

import (
	"fmt"
	"os"
	"purpcmd/server/implant"
	"purpcmd/server/log"

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

func (l *LuaProfile) command(L *lua.LState) int {
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

func ImplantAddSendBuffer(L *lua.LState) int {
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

func ImplantAddGenericTask(L *lua.LState) int {
	code := L.CheckInt(1)
	payload := L.CheckString(2)

	taskID, errInt := implant.ImplantAddGenericTask(code, payload)
	if errInt != 0 {
		L.Push(lua.LNil)
		L.Push(lua.LString("could not create task"))
		return 2
	}
	L.Push(lua.LString(taskID))

	return 1
}

// registerTaskCallback registers a callback function for a specific task ID
// Usage from Lua: register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user) ... end)
func (l *LuaProfile) registerTaskCallback(L *lua.LState) int {
	taskID := L.CheckString(1)
	callback := L.CheckFunction(2)

	l.TaskCallbacksMutex.Lock()
	l.TaskCallbacks[taskID] = callback
	l.TaskCallbacksMutex.Unlock()

	return 0
}

// LuaPrint provides a thread-safe print function for Lua scripts
// Usage from Lua: lua_print("message", var1, var2, ...)
func LuaPrint(L *lua.LState) int {
	args := make([]interface{}, L.GetTop())
	for i := 1; i <= L.GetTop(); i++ {
		args[i-1] = L.Get(i).String()
	}
	log.AsyncWriteStdout(args...)
	return 0
}

func CallCommand(name, implantType, payload string) (string, error) {
	cmdStr, exists := CMDMAP[implantType+"."+name]
	if !exists {
		return "", fmt.Errorf("command %s for %s not found", name, implantType)
	}

	L := ScriptMAP[cmdStr.ScriptName].state
	L.Push(cmdStr.ptr)

	L.Push(lua.LString(payload))
	//L.Push(lua.LString(im))

	err := L.PCall(1, 1, nil)
	if err != nil {
		return "", err
	}

	ret := L.ToString(-1)
	L.Pop(1)

	return ret, nil
}
