package lua

import (
	"fmt"
	"time"

	"purpcmd/server/implant"
	"purpcmd/server/log"

	lua "github.com/yuin/gopher-lua"
)

func callLifecycle(profile *LuaProfile, session string, function *lua.LFunction, arguments ...lua.LValue) {
	profile.stateMu.Lock()
	defer profile.stateMu.Unlock()
	if profile.state == nil {
		return
	}
	restoreExecution := profile.enterSessionExecution(session)
	defer restoreExecution()
	state := profile.state
	state.Push(function)
	for _, argument := range arguments {
		state.Push(argument)
	}
	if err := state.PCall(len(arguments), 0, nil); err != nil {
		log.PrintErr(err.Error())
	}
}

func lifecycleFunction(profile *LuaProfile, name string) *lua.LFunction {
	profile.stateMu.Lock()
	defer profile.stateMu.Unlock()
	if profile.state == nil {
		return nil
	}
	function := profile.state.GetGlobal(name)
	if function.Type() != lua.LTFunction {
		return nil
	}
	return function.(*lua.LFunction)
}

func LuaOnRegister(item implant.Implant) {
	for _, profile := range scriptSnapshot() {
		function := lifecycleFunction(profile, "OnRegister")
		if function == nil {
			continue
		}
		callLifecycle(profile, item.Name, function,
			lua.LString(item.Name),
			lua.LString(item.UUID),
			lua.LString(item.Metadata.Hostname),
			lua.LString(item.Metadata.User),
			lua.LString(item.Metadata.Socket),
			lua.LString(fmt.Sprintf("%d", item.Metadata.SessionID)),
			lua.LString(item.Metadata.Type),
		)
	}
}

func LuaOnCheck(taskID [8]byte, data string, item implant.Implant) {
	for _, profile := range scriptSnapshot() {
		function := lifecycleFunction(profile, "OnCheck")
		if function == nil {
			continue
		}
		callLifecycle(profile, item.Name, function,
			lua.LString(item.Name),
			lua.LString(item.UUID),
			lua.LString(item.Metadata.Hostname),
			lua.LString(item.Metadata.User),
			lua.LString(item.Metadata.Socket),
			lua.LString(fmt.Sprintf("%d", item.Metadata.SessionID)),
			lua.LString(string(taskID[:])),
			lua.LString(data),
			lua.LString(item.Metadata.Type),
		)
	}
}

func LuaOnResponse(taskID [8]byte, data string, item implant.Implant) {
	taskIDString := string(taskID[:])
	for _, profile := range scriptSnapshot() {
		taskCallback := profile.takeTaskCallback(item.Name, taskIDString, time.Now())
		if taskCallback != nil {
			callLifecycle(profile, item.Name, taskCallback,
				lua.LString(taskIDString),
				lua.LString(data),
				lua.LString(item.Name),
				lua.LString(item.UUID),
				lua.LString(item.Metadata.Hostname),
				lua.LString(item.Metadata.User),
				lua.LString(item.Metadata.Type),
			)
			continue
		}

		function := lifecycleFunction(profile, "OnResponse")
		if function == nil {
			continue
		}
		callLifecycle(profile, item.Name, function,
			lua.LString(item.Name),
			lua.LString(item.UUID),
			lua.LString(item.Metadata.Hostname),
			lua.LString(item.Metadata.User),
			lua.LString(item.Metadata.Socket),
			lua.LString(fmt.Sprintf("%d", item.Metadata.SessionID)),
			lua.LString(taskIDString),
			lua.LString(data),
			lua.LString(item.Metadata.Type),
		)
	}
}
