package lua

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	workingDirectory, err := os.Getwd()
	if err != nil {
		log.PrintErr(err.Error())
		return
	}
	loaded := make(map[string]struct{}, len(scripts))
	for _, storedPath := range scripts {
		path, resolveErr := resolvePersistedScriptPath(storedPath, workingDirectory)
		if resolveErr != nil {
			log.PrintErr(resolveErr.Error())
			continue
		}
		if path != storedPath {
			if err := db.DBScriptReplacePath(storedPath, path); err != nil {
				log.PrintErr(fmt.Sprintf("migrate script path %q: %v", storedPath, err))
				continue
			}
			log.PrintInfo("Relocated script ", storedPath, " -> ", path)
		}
		if _, exists := loaded[path]; exists {
			continue
		}
		loaded[path] = struct{}{}
		if _, err := loadScript(path, false); err != nil {
			log.PrintErr(err.Error())
		}
	}
}

func resolvePersistedScriptPath(storedPath, workingDirectory string) (string, error) {
	workingDirectory, err := filepath.Abs(workingDirectory)
	if err != nil {
		return "", err
	}

	candidates := make([]string, 0, 3)
	if filepath.IsAbs(storedPath) {
		candidates = append(candidates, filepath.Clean(storedPath))
	} else {
		candidates = append(candidates, filepath.Join(workingDirectory, storedPath))
	}
	parts := strings.Split(filepath.Clean(storedPath), string(os.PathSeparator))
	for index, part := range parts {
		if part == "script" {
			localParts := append([]string{workingDirectory}, parts[index:]...)
			candidates = append(candidates, filepath.Join(localParts...))
			break
		}
	}
	if base := filepath.Base(storedPath); base != "." && base != string(os.PathSeparator) {
		candidates = append(candidates, filepath.Join(workingDirectory, "script", base))
	}

	seen := make(map[string]struct{}, len(candidates))
	tried := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate, err = filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		tried = append(tried, candidate)
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("persisted script %q was not found (tried %s)", storedPath, strings.Join(tried, ", "))
}

func LuaNew(path string) (*LuaProfile, error) {
	profile := &LuaProfile{
		script:        path,
		state:         lua.NewState(),
		TaskCallbacks: make(map[taskCallbackKey]taskCallbackRegistration),
	}
	profile.state.OpenLibs()
	profile.state.SetGlobal("command", profile.state.NewFunction(profile.command))
	profile.state.SetGlobal("add_task", profile.state.NewFunction(profile.implantAddGenericTask))
	profile.state.SetGlobal("add_task_upload_file", profile.state.NewFunction(profile.implantAddUploadFileCommand))
	profile.state.SetGlobal("add_task_send_buffer", profile.state.NewFunction(profile.implantAddSendBuffer))
	profile.state.SetGlobal("implant_register_profile", profile.state.NewFunction(LuaRegisterImplantProfile))
	profile.state.SetGlobal("register_task_callback", profile.state.NewFunction(profile.registerTaskCallback))
	profile.state.SetGlobal("session", profile.state.NewFunction(profile.session))
	profile.state.SetGlobal("session_print", profile.state.NewFunction(profile.sessionPrint))
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
	profile.startTaskCallbackCleaner()
	ScriptMAP[path] = profile
	scriptMapMu.Unlock()
	go profile.LuaRunMain()
	return profile, nil
}

func LuaLoad(path string) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		log.PrintErr(err.Error())
		return
	}
	log.PrintInfo("Loading script ", absolute)
	if _, err := loadScript(absolute, true); err != nil {
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
	profile.stopTaskCallbackCleaner()
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
	absolute, err := filepath.Abs(path)
	if err != nil {
		log.PrintErr(err.Error())
		return
	}
	log.PrintInfo("Unloading script ", absolute)
	if err := unloadScript(absolute, true); err != nil {
		log.PrintErr(err.Error())
	}
}
