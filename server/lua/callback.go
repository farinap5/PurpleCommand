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
		v.state.Push(lua.LString(fmt.Sprintf("%d",i.Metadata.SessionID)))
		//v.state.Push(lua.LString(i.Metadata.IP))
		//v.state.Push(lua.LString(i.Metadata.Sleep))
		//v.state.Push(lua.LString(i.Metadata.PID))
		//v.state.Push(lua.LString(i.Metadata.Arch))

		go v.state.PCall(6, 0, nil)
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
		v.state.Push(lua.LString(fmt.Sprintf("%d",i.Metadata.SessionID)))
		v.state.Push(lua.LString(string(tid[:])))
		v.state.Push(lua.LString(data))

		go v.state.PCall(8, 0, nil)
	}
}

func LuaOnResponse(tid [8]byte, data string, i implant.Implant) {
	for _, v := range ScriptMAP {
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
		v.state.Push(lua.LString(fmt.Sprintf("%d",i.Metadata.SessionID)))
		v.state.Push(lua.LString(string(tid[:])))
		v.state.Push(lua.LString(data))

		go v.state.PCall(8, 0, nil)
	}

}