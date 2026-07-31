package lua

import (
	"fmt"
	"os"
	"sort"
	"sync"

	"purpcmd/internal"
	"purpcmd/server/implant"
	"purpcmd/server/log"

	lua "github.com/yuin/gopher-lua"
)

var (
	CMDMAP   = make(map[commandKey]*commandDef)
	cmdMapMu sync.RWMutex
)

// LuaGetCommandDescriptions returns only the commands registered for the exact
// payload type of the selected session. Payload type matching is case-sensitive.
func LuaGetCommandDescriptions(payloadType string) [][]string {
	if internal.ValidatePayloadType(payloadType) != nil {
		return nil
	}

	cmdMapMu.RLock()
	defer cmdMapMu.RUnlock()

	var aux [][]string
	for key, command := range CMDMAP {
		if key.Type != payloadType {
			continue
		}
		aux = append(aux, []string{
			command.Name, command.Description,
		})
	}
	sort.Slice(aux, func(i, j int) bool {
		return aux[i][0] < aux[j][0]
	})
	return aux
}

// LuaGetCommandDesc is retained for compatibility with integrations using the
// old API. The obsolete command argument is ignored.
func LuaGetCommandDesc(payloadType string, _ ...string) [][]string {
	return LuaGetCommandDescriptions(payloadType)
}

func (l *LuaProfile) command(L *lua.LState) int {
	payloadType := L.CheckString(1)
	name := L.CheckString(2)
	desc := L.CheckString(3)
	fn := L.CheckFunction(4) // Get function reference

	if err := internal.ValidatePayloadType(payloadType); err != nil {
		L.ArgError(1, err.Error())
		return 0
	}
	if err := internal.ValidateCommandName(name); err != nil {
		L.ArgError(2, err.Error())
		return 0
	}

	key := commandKey{Type: payloadType, Name: name}
	cmdMapMu.Lock()
	defer cmdMapMu.Unlock()
	if existing := CMDMAP[key]; existing != nil {
		L.RaiseError("command %q for payload type %q is already registered by %s", name, payloadType, existing.ScriptName)
		return 0
	}
	CMDMAP[key] = &commandDef{
		Type:        payloadType,
		Name:        name,
		Description: desc,
		ptr:         fn,
		ScriptName:  l.script,
	}

	return 0
}

func removeCommandsForScript(scriptName string) {
	cmdMapMu.Lock()
	defer cmdMapMu.Unlock()
	for key, command := range CMDMAP {
		if command.ScriptName == scriptName {
			delete(CMDMAP, key)
		}
	}
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
// Usage from Lua: register_task_callback(task_id, function(task_id, response,
// name, uuid, hostname, user, payload_type) ... end)
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

func CallCommand(name, payloadType, payload string) (string, error) {
	if err := internal.ValidatePayloadType(payloadType); err != nil {
		return "", err
	}
	if err := internal.ValidateCommandName(name); err != nil {
		return "", err
	}

	cmdMapMu.RLock()
	cmdStr, exists := CMDMAP[commandKey{Type: payloadType, Name: name}]
	cmdMapMu.RUnlock()
	if !exists {
		return "", fmt.Errorf("command %q for payload type %q not found", name, payloadType)
	}

	profile := ScriptMAP[cmdStr.ScriptName]
	if profile == nil {
		return "", fmt.Errorf("script %q for command %q is not loaded", cmdStr.ScriptName, name)
	}
	L := profile.state
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
