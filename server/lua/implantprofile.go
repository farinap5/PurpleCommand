package lua

import (
	"purpcmd/internal"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/log"

	lua "github.com/yuin/gopher-lua"
)

// LuaRegisterImplantProfile exposes implant_register_profile(name, table) to Lua.
//
// Lua usage:
//
//	implant_register_profile("myprofile", {
//	    type     = "impl",
//	    lhost    = "192.168.1.1:4444",
//	    os       = "linux",
//	    arch     = "amd64",
//	    uri      = "/beacon",
//	    ua       = "Mozilla/5.0",
//	    output   = "shell",
//	    template = "./template",
//	})
func LuaRegisterImplantProfile(L *lua.LState) int {
	name := L.CheckString(1)
	tbl := L.CheckTable(2)

	p := implantbuilder.Profile{
		Type:     internal.DefaultPayloadType,
		OS:       "linux",
		ARCH:     "amd64",
		URI:      "/",
		UA:       "Mozilla PurpCMD",
		Output:   "implant",
		Template: "./template",
	}

	if v := tbl.RawGetString("type"); v != lua.LNil {
		p.Type = v.String()
	}
	if v := tbl.RawGetString("lhost"); v != lua.LNil {
		p.LHOST = v.String()
	}
	if v := tbl.RawGetString("os"); v != lua.LNil {
		p.OS = v.String()
	}
	if v := tbl.RawGetString("arch"); v != lua.LNil {
		p.ARCH = v.String()
	}
	if v := tbl.RawGetString("uri"); v != lua.LNil {
		p.URI = v.String()
	}
	if v := tbl.RawGetString("ua"); v != lua.LNil {
		p.UA = v.String()
	}
	if v := tbl.RawGetString("output"); v != lua.LNil {
		p.Output = v.String()
	}
	if v := tbl.RawGetString("template"); v != lua.LNil {
		p.Template = v.String()
	}

	if err := implantbuilder.RegisterProfile(name, p); err != nil {
		log.PrintErr("implant_register_profile: " + err.Error())
		L.Push(lua.LString(err.Error()))
		return 1
	}
	return 0
}
