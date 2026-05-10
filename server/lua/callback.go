/*
	TODO: send implant type too
*/

package lua

import (
	"fmt"
	"purpcmd/server/implant"

	lua "github.com/yuin/gopher-lua"
)

func LuaOnRegister(i implant.Implant) {
	for _, v := range ScriptMAP {
		fn := v.state.GetGlobal("OnRegister")
		if fn.Type() != lua.LTFunction {
			continue
		}

		v.state.Push(fn)

		v.state.Push(lua.LString(i.Name))
		v.state.Push(lua.LString(i.UUID))
		v.state.Push(lua.LString(i.Metadata.Hostname))
		v.state.Push(lua.LString(i.Metadata.User))
		v.state.Push(lua.LString(i.Metadata.Socket))
		v.state.Push(lua.LString(fmt.Sprintf("%d", i.Metadata.SessionID)))
		//v.state.Push(lua.LString(i.Metadata.IP))
		//v.state.Push(lua.LString(i.Metadata.Sleep))
		//v.state.Push(lua.LString(i.Metadata.PID))
		//v.state.Push(lua.LString(i.Metadata.Arch))

		v.state.PCall(6, 0, nil)
	}
}

func LuaOnCheck(tid [8]byte, data string, i implant.Implant) {
	for _, v := range ScriptMAP {
		fn := v.state.GetGlobal("OnCheck")
		if fn.Type() != lua.LTFunction {
			continue
		}

		v.state.Push(fn)

		v.state.Push(lua.LString(i.Name))
		v.state.Push(lua.LString(i.UUID))
		v.state.Push(lua.LString(i.Metadata.Hostname))
		v.state.Push(lua.LString(i.Metadata.User))
		v.state.Push(lua.LString(i.Metadata.Socket))
		v.state.Push(lua.LString(fmt.Sprintf("%d", i.Metadata.SessionID)))
		v.state.Push(lua.LString(string(tid[:])))
		v.state.Push(lua.LString(data))

		v.state.PCall(8, 0, nil)
	}
}

func LuaOnResponse(tid [8]byte, data string, i implant.Implant) {
	taskIDStr := string(tid[:])

	for _, v := range ScriptMAP {
		// Check for task-specific callback first
		v.TaskCallbacksMutex.RLock()
		taskCallback, hasTaskCallback := v.TaskCallbacks[taskIDStr]
		v.TaskCallbacksMutex.RUnlock()

		if hasTaskCallback {
			// Call task-specific callback
			v.state.Push(taskCallback)
			v.state.Push(lua.LString(taskIDStr))
			v.state.Push(lua.LString(data))
			v.state.Push(lua.LString(i.Name))
			v.state.Push(lua.LString(i.UUID))
			v.state.Push(lua.LString(i.Metadata.Hostname))
			v.state.Push(lua.LString(i.Metadata.User))
			v.state.PCall(6, 0, nil)

			// Remove the callback after execution (one-time use)
			v.TaskCallbacksMutex.Lock()
			delete(v.TaskCallbacks, taskIDStr)
			v.TaskCallbacksMutex.Unlock()
			continue
		}

		// Fall back to global OnResponse callback
		fn := v.state.GetGlobal("OnResponse")
		if fn.Type() != lua.LTFunction {
			continue
		}

		v.state.Push(fn)

		v.state.Push(lua.LString(i.Name))
		v.state.Push(lua.LString(i.UUID))
		v.state.Push(lua.LString(i.Metadata.Hostname))
		v.state.Push(lua.LString(i.Metadata.User))
		v.state.Push(lua.LString(i.Metadata.Socket))
		v.state.Push(lua.LString(fmt.Sprintf("%d", i.Metadata.SessionID)))
		v.state.Push(lua.LString(taskIDStr))
		v.state.Push(lua.LString(data))

		v.state.PCall(8, 0, nil)
	}

}


