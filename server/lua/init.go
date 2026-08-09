package lua

import (
	"errors"
	"sync"

	"purpcmd/server/db"
	"purpcmd/server/log"

	lua "github.com/yuin/gopher-lua"
)

var (
	ScriptMAP     = make(map[string]*LuaProfile)
	CurrentScript = "none"
	scriptMapMu   sync.RWMutex
)

func scriptSnapshot() []*LuaProfile {
	scriptMapMu.RLock()
	defer scriptMapMu.RUnlock()
	result := make([]*LuaProfile, 0, len(ScriptMAP))
	for _, profile := range ScriptMAP {
		result = append(result, profile)
	}
	return result
}

func ScriptsReloadFromDB() {
	scripts, err := db.DBScriptGetAll()
	if err != nil {
		log.PrintErr(err.Error())
		return
	}
	for _, path := range scripts {
		if _, err := loadScript(path, false); err != nil {
			log.PrintErr(err.Error())
		}
	}
}

func LuaNew(path string) (*LuaProfile, error) {
	profile := &LuaProfile{
		script:        path,
		state:         lua.NewState(),
		TaskCallbacks: make(map[string]*lua.LFunction),
	}
	profile.state.OpenLibs()
	profile.state.SetGlobal("command", profile.state.NewFunction(profile.command))
	profile.state.SetGlobal("add_task", profile.state.NewFunction(profile.implantAddGenericTask))
	profile.state.SetGlobal("add_task_upload_file", profile.state.NewFunction(profile.implantAddUploadFileCommand))
	profile.state.SetGlobal("add_task_send_buffer", profile.state.NewFunction(profile.implantAddSendBuffer))
	profile.state.SetGlobal("implant_register_profile", profile.state.NewFunction(LuaRegisterImplantProfile))
	profile.state.SetGlobal("register_task_callback", profile.state.NewFunction(profile.registerTaskCallback))
	profile.state.SetGlobal("lua_print", profile.state.NewFunction(LuaPrint))
	if err := profile.state.DoFile(path); err != nil {
		removeCommandsForScript(path)
		profile.state.Close()
		return nil, err
	}
	return profile, nil
}

func loadScript(path string, persist bool) (*LuaProfile, error) {
	scriptMapMu.Lock()
	if ScriptMAP[path] != nil {
		scriptMapMu.Unlock()
		return nil, errors.New("script already loaded")
	}
	profile, err := LuaNew(path)
	if err != nil {
		scriptMapMu.Unlock()
		return nil, err
	}
	if persist {
		if err := db.DBScriptInsert(path); err != nil {
			removeCommandsForScript(path)
			profile.state.Close()
			scriptMapMu.Unlock()
			return nil, err
		}
	}
	profile.Running = true
	ScriptMAP[path] = profile
	scriptMapMu.Unlock()
	go profile.LuaRunMain()
	return profile, nil
}

func LuaLoad(path string) {
	log.PrintInfo("Loading script ", path)
	if _, err := loadScript(path, true); err != nil {
		log.PrintErr(err.Error())
	}
}

func unloadScript(path string, persist bool) error {
	scriptMapMu.Lock()
	profile := ScriptMAP[path]
	if profile == nil {
		scriptMapMu.Unlock()
		return errors.New("script not loaded")
	}
	if persist {
		if err := db.DBScriptDelete(path); err != nil {
			scriptMapMu.Unlock()
			return err
		}
	}
	delete(ScriptMAP, path)
	scriptMapMu.Unlock()

	removeCommandsForScript(path)
	profile.stateMu.Lock()
	profile.Running = false
	if profile.state != nil {
		profile.state.Close()
		profile.state = nil
	}
	profile.stateMu.Unlock()
	return nil
}

func LuaUnload(path string) {
	log.PrintInfo("Unloading script ", path)
	if err := unloadScript(path, true); err != nil {
		log.PrintErr(err.Error())
	}
}
