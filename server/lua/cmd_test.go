package lua

import (
	"path/filepath"
	"strings"
	"testing"

	serverimplant "purpcmd/server/implant"

	glua "github.com/yuin/gopher-lua"
)

func isolateLuaCommands(t *testing.T) {
	t.Helper()
	cmdMapMu.Lock()
	previousCommands := CMDMAP
	CMDMAP = make(map[commandKey]*commandDef)
	cmdMapMu.Unlock()
	previousScripts := ScriptMAP
	ScriptMAP = make(map[string]*LuaProfile)
	t.Cleanup(func() {
		cmdMapMu.Lock()
		CMDMAP = previousCommands
		cmdMapMu.Unlock()
		ScriptMAP = previousScripts
	})
}

func loadCommandTestProfile(t *testing.T, scriptName, source string) *LuaProfile {
	t.Helper()
	state := glua.NewState()
	profile := &LuaProfile{
		script:        scriptName,
		state:         state,
		TaskCallbacks: make(map[string]*glua.LFunction),
	}
	state.SetGlobal("command", state.NewFunction(profile.command))
	if err := state.DoString(source); err != nil {
		state.Close()
		t.Fatal(err)
	}
	ScriptMAP[scriptName] = profile
	t.Cleanup(state.Close)
	return profile
}

func TestCommandsAreFilteredAndDispatchedByPayloadType(t *testing.T) {
	isolateLuaCommands(t)
	loadCommandTestProfile(t, "types.lua", `
function alpha_ping(payload) return "alpha:" .. payload end
function alpha_list(payload) return "list:" .. payload end
function beta_ping(payload) return "beta:" .. payload end
command("alpha", "ping", "Alpha ping", alpha_ping)
command("alpha", "list", "Alpha list", alpha_list)
command("beta", "ping", "Beta ping", beta_ping)
`)

	alpha := LuaGetCommandDescriptions("alpha")
	if len(alpha) != 2 || alpha[0][0] != "list" || alpha[1][0] != "ping" {
		t.Fatalf("alpha suggestions = %#v", alpha)
	}
	beta := LuaGetCommandDescriptions("beta")
	if len(beta) != 1 || beta[0][0] != "ping" || beta[0][1] != "Beta ping" {
		t.Fatalf("beta suggestions = %#v", beta)
	}
	if commands := LuaGetCommandDescriptions("gamma"); len(commands) != 0 {
		t.Fatalf("unknown type suggestions = %#v", commands)
	}

	result, err := CallCommand("ping", "alpha", "hello")
	if err != nil || result != "alpha:hello" {
		t.Fatalf("alpha dispatch = %q, %v", result, err)
	}
	result, err = CallCommand("ping", "beta", "hello")
	if err != nil || result != "beta:hello" {
		t.Fatalf("beta dispatch = %q, %v", result, err)
	}
	if _, err := CallCommand("list", "beta", ""); err == nil {
		t.Fatal("dispatched an alpha-only command to beta")
	}

	cmdMapMu.RLock()
	definition := CMDMAP[commandKey{Type: "alpha", Name: "ping"}]
	cmdMapMu.RUnlock()
	if definition == nil || definition.Type != "alpha" {
		t.Fatalf("command definition type = %#v", definition)
	}
}

func TestDuplicateCommandRegistrationIsRejectedAndUnloadCleansCommands(t *testing.T) {
	isolateLuaCommands(t)
	loadCommandTestProfile(t, "first.lua", `
function ping(payload) return payload end
command("alpha", "ping", "first", ping)
`)

	state := glua.NewState()
	second := &LuaProfile{script: "second.lua", state: state}
	state.SetGlobal("command", state.NewFunction(second.command))
	err := state.DoString(`
function ping(payload) return payload end
command("alpha", "ping", "second", ping)
`)
	state.Close()
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate registration returned %v", err)
	}

	removeCommandsForScript("first.lua")
	if commands := LuaGetCommandDescriptions("alpha"); len(commands) != 0 {
		t.Fatalf("unloaded script left commands: %#v", commands)
	}
}

func TestLifecycleCallbacksReceivePayloadType(t *testing.T) {
	isolateLuaCommands(t)
	profile := loadCommandTestProfile(t, "callbacks.lua", `
function OnRegister(name, uuid, hostname, user, socket, session_id, payload_type)
    seen_register_type = payload_type
end
function OnCheck(name, uuid, hostname, user, socket, session_id, task_id, data, payload_type)
    seen_check_type = payload_type
end
function OnResponse(name, uuid, hostname, user, socket, session_id, task_id, data, payload_type)
    seen_response_type = payload_type
end
`)
	imp := serverimplant.ImplantNew("12345")
	imp.Metadata.Type = "alpha"

	LuaOnRegister(*imp)
	LuaOnCheck([8]byte{'t'}, "task", *imp)
	LuaOnResponse([8]byte{'t'}, "response", *imp)
	for _, global := range []string{"seen_register_type", "seen_check_type", "seen_response_type"} {
		if got := profile.state.GetGlobal(global).String(); got != "alpha" {
			t.Fatalf("%s = %q", global, got)
		}
	}
}

func TestBundledLuaScriptRegistersDefaultPayloadCommands(t *testing.T) {
	isolateLuaCommands(t)
	path := filepath.Join("..", "..", "script", "main.lua")
	profile, err := LuaNew(path)
	if err != nil {
		t.Fatalf("load bundled Lua script: %v", err)
	}
	t.Cleanup(profile.state.Close)
	t.Cleanup(func() { removeCommandsForScript(path) })

	commands := LuaGetCommandDescriptions("impl")
	if len(commands) != 11 {
		t.Fatalf("bundled impl commands = %#v", commands)
	}
	if commands[0][0] != "cat" || commands[len(commands)-1][0] != "upload" {
		t.Fatalf("bundled commands are not sorted: %#v", commands)
	}
}
