package lua

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/log"

	"github.com/cheynewallace/tabby"
)

func (profile *LuaProfile) LuaRunMain() {
	profile.stateMu.Lock()
	defer profile.stateMu.Unlock()
	if profile.state == nil {
		return
	}
	if err := profile.state.DoString("if Main then Main() end"); err != nil {
		log.PrintErr(err.Error())
	}
}

func APIListScripts() []teamapi.Script {
	scriptMapMu.RLock()
	result := make([]teamapi.Script, 0, len(ScriptMAP))
	for path, profile := range ScriptMAP {
		result = append(result, scriptDTO(path, profile.Running))
	}
	scriptMapMu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func APILoadScript(path string) (teamapi.Script, error) {
	if path == "" {
		return teamapi.Script{}, errors.New("script path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return teamapi.Script{}, err
	}
	if _, err := loadScript(absolute, true); err != nil {
		return teamapi.Script{}, err
	}
	return scriptDTO(absolute, true), nil
}

func APIUnloadScript(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return unloadScript(absolute, true)
}

func scriptDTO(path string, loaded bool) teamapi.Script {
	item := teamapi.Script{Name: filepath.Base(path), Path: path, Loaded: loaded}
	content, err := os.ReadFile(path)
	if err == nil {
		sum := sha256.Sum256(content)
		item.SHA256 = hex.EncodeToString(sum[:])
	}
	return item
}

func ScriptList() {
	scripts := APIListScripts()
	if len(scripts) == 0 {
		log.PrintAlert("no script")
	}
	table := tabby.New()
	table.AddHeader("N", "PATH", "LOADED")
	for index, script := range scripts {
		table.AddLine(index+1, script.Path, script.Loaded)
	}
	print("\n")
	table.Print()
	print("\n")
}
