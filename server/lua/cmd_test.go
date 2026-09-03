package lua

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	serverimplant "purpcmd/server/implant"
	"purpcmd/server/implantbuilder"

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

func isolateLuaDatabase(t *testing.T) {
	t.Helper()
	previousPath := db.DatabasePath
	previousDatabase := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "lua.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTeamserverSchema(); err != nil {
		_ = db.DBMS.DBConn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DBMS = previousDatabase
		db.DatabasePath = previousPath
	})
}

func loadCommandTestProfile(t *testing.T, scriptName, source string) *LuaProfile {
	t.Helper()
	state := glua.NewState()
	profile := &LuaProfile{
		script:        scriptName,
		state:         state,
		TaskCallbacks: make(map[taskCallbackKey]taskCallbackRegistration),
	}
	state.SetGlobal("command", state.NewFunction(profile.command))
	state.SetGlobal("add_task", state.NewFunction(profile.implantAddGenericTask))
	state.SetGlobal("register_task_callback", state.NewFunction(profile.registerTaskCallback))
	state.SetGlobal("session", state.NewFunction(profile.session))
	state.SetGlobal("session_print", state.NewFunction(profile.sessionPrint))
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

func TestSpeakerBackedSessionUsesLuaTaskPipeline(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	previousImplants := serverimplant.ImplantMAP
	previousCurrent := serverimplant.CurrentImplant
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	serverimplant.CurrentImplant = "none"
	t.Cleanup(func() {
		serverimplant.ImplantMAP = previousImplants
		serverimplant.CurrentImplant = previousCurrent
	})

	profile := loadCommandTestProfile(t, "speaker.lua", `
function speaker_echo(payload)
    return add_task(42, payload)
end
command("bind.impl", "echo", "Speaker echo", speaker_echo)
`)
	profile.state.SetGlobal("add_task", profile.state.NewFunction(profile.implantAddGenericTask))

	session := serverimplant.ImplantNew("bind-session")
	session.Metadata.Type = "bind.impl"
	session.ImplantSetSpeaker("bind-http")
	session.ImplantAddImplant()

	reply, err := APIExecuteCommand(teamapi.CommandExecuteRequest{
		Session:   session.Name,
		Name:      "echo",
		Arguments: "hello through speaker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.TaskIDs) != 1 {
		t.Fatalf("created task IDs = %#v", reply.TaskIDs)
	}
	tasks, err := serverimplant.APIListTasks(session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != reply.TaskIDs[0] || tasks[0].Code != 42 {
		t.Fatalf("speaker session tasks = %#v", tasks)
	}
	if string(session.Task[0].Payload) != "hello through speaker" {
		t.Fatalf("task payload = %q", session.Task[0].Payload)
	}
	select {
	case <-session.TaskReady():
	default:
		t.Fatal("speaker dispatcher was not notified about the Lua task")
	}
	apiSession, err := serverimplant.APIGetSession(session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if apiSession.Transport != teamapi.SessionTransportSpeaker || apiSession.Speaker != "bind-http" {
		t.Fatalf("speaker session route = %#v", apiSession)
	}
}

func TestTaskCallbackUsesSessionMetadataAndDefaultTimeout(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	previousImplants := serverimplant.ImplantMAP
	previousCurrent := serverimplant.CurrentImplant
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	serverimplant.CurrentImplant = "none"
	t.Cleanup(func() {
		serverimplant.ImplantMAP = previousImplants
		serverimplant.CurrentImplant = previousCurrent
	})

	profile := loadCommandTestProfile(t, "task-callback.lua", `
callback_called = false
callback_task = ""
callback_session = ""
function OnResponse(...) global_response_called = true end
`)
	item := serverimplant.ImplantNew("677222")
	item.Metadata.Sleep = 10
	item.Metadata.PID = 1234
	item.Metadata.Type = "impl"
	item.Metadata.Hostname = "workstation"
	item.Metadata.User = "operator"
	item.Metadata.Proc = "agent"
	item.ImplantAddImplant()

	profile.executionSession = item.Name
	registeredAt := time.Now()
	err := profile.state.DoString(`
task_id, task_err, task_session_id = add_task(42, "payload")
session_info, session_err = session(tonumber(task_session_id))
missing_session_info, missing_session_err = session("missing-session")
register_task_callback(task_id, function(task_id, response, name, uuid, hostname, user, payload_type)
    callback_called = true
    callback_task = task_id
    callback_response = response
    callback_session = name
    callback_type = payload_type
end)
`)
	profile.executionSession = ""
	if err != nil {
		t.Fatal(err)
	}

	taskID := profile.state.GetGlobal("task_id").String()
	if len(taskID) != 8 {
		t.Fatalf("task ID = %q", taskID)
	}
	if profile.state.GetGlobal("task_err") != glua.LNil {
		t.Fatalf("task error = %s", profile.state.GetGlobal("task_err"))
	}
	if got := profile.state.GetGlobal("task_session_id").String(); got != item.Name {
		t.Fatalf("returned session ID = %q", got)
	}
	info, ok := profile.state.GetGlobal("session_info").(*glua.LTable)
	if !ok {
		t.Fatalf("session metadata = %s", profile.state.GetGlobal("session_info"))
	}
	if got := info.RawGetString("sleep").String(); got != "10" {
		t.Fatalf("session sleep = %q", got)
	}
	if got := info.RawGetString("hostname").String(); got != "workstation" {
		t.Fatalf("session hostname = %q", got)
	}
	if got := info.RawGetString("pid").String(); got != "1234" {
		t.Fatalf("session PID = %q", got)
	}
	if got := info.RawGetString("session_id").String(); got != item.Name {
		t.Fatalf("session metadata ID = %q", got)
	}
	if got := info.RawGetString("status").String(); got != "alive" {
		t.Fatalf("session status = %q", got)
	}
	if profile.state.GetGlobal("session_err") != glua.LNil {
		t.Fatalf("session error = %s", profile.state.GetGlobal("session_err"))
	}
	if profile.state.GetGlobal("missing_session_info") != glua.LNil || profile.state.GetGlobal("missing_session_err") == glua.LNil {
		t.Fatalf("missing session result = %s, %s", profile.state.GetGlobal("missing_session_info"), profile.state.GetGlobal("missing_session_err"))
	}

	key := taskCallbackKey{Session: item.Name, TaskID: taskID}
	profile.TaskCallbacksMutex.RLock()
	registration, found := profile.TaskCallbacks[key]
	profile.TaskCallbacksMutex.RUnlock()
	if !found {
		t.Fatal("task callback was not registered")
	}
	wantExpiry := registeredAt.Add(25 * time.Second)
	if registration.ExpiresAt.Before(wantExpiry.Add(-time.Second)) || registration.ExpiresAt.After(wantExpiry.Add(time.Second)) {
		t.Fatalf("callback expiry = %s, want approximately %s", registration.ExpiresAt, wantExpiry)
	}
	item.Metadata.Sleep = 0
	if got := defaultTaskCallbackTimeout(item.Name); got != zeroSleepCallbackTimeout {
		t.Fatalf("zero-sleep callback timeout = %s", got)
	}

	var responseID [8]byte
	copy(responseID[:], taskID)
	LuaOnResponse(responseID, "done", *item)
	if profile.state.GetGlobal("callback_called") != glua.LTrue {
		t.Fatal("task callback was not called")
	}
	if got := profile.state.GetGlobal("callback_task").String(); got != taskID {
		t.Fatalf("callback task ID = %q", got)
	}
	if got := profile.state.GetGlobal("callback_response").String(); got != "done" {
		t.Fatalf("callback response = %q", got)
	}
	if got := profile.state.GetGlobal("callback_session").String(); got != item.Name {
		t.Fatalf("callback session = %q", got)
	}
	if got := profile.state.GetGlobal("callback_type").String(); got != "impl" {
		t.Fatalf("callback payload type = %q", got)
	}
	if profile.state.GetGlobal("global_response_called") == glua.LTrue {
		t.Fatal("global response callback ran instead of the task callback")
	}
	profile.TaskCallbacksMutex.RLock()
	_, found = profile.TaskCallbacks[key]
	profile.TaskCallbacksMutex.RUnlock()
	if found {
		t.Fatal("used task callback was not removed")
	}
}

func TestTaskCallbackExplicitTimeoutExpiresWithoutResponse(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	previousImplants := serverimplant.ImplantMAP
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	t.Cleanup(func() { serverimplant.ImplantMAP = previousImplants })

	profile := loadCommandTestProfile(t, "callback-timeout.lua", `
callback_called = false
global_response_called = false
function OnResponse(...) global_response_called = true end
`)
	item := serverimplant.ImplantNew("timeout-session")
	item.Metadata.Sleep = 60
	item.ImplantAddImplant()
	profile.executionSession = item.Name
	registeredAt := time.Now()
	err := profile.state.DoString(`
task_id, task_err, task_session_id = add_task(7, "")
register_task_callback(task_id, function() callback_called = true end, 1.5)
`)
	profile.executionSession = ""
	if err != nil {
		t.Fatal(err)
	}

	taskID := profile.state.GetGlobal("task_id").String()
	key := taskCallbackKey{Session: item.Name, TaskID: taskID}
	profile.TaskCallbacksMutex.RLock()
	registration, found := profile.TaskCallbacks[key]
	profile.TaskCallbacksMutex.RUnlock()
	if !found {
		t.Fatal("explicit-timeout callback was not registered")
	}
	wantExpiry := registeredAt.Add(1500 * time.Millisecond)
	if registration.ExpiresAt.Before(wantExpiry.Add(-time.Second)) || registration.ExpiresAt.After(wantExpiry.Add(time.Second)) {
		t.Fatalf("explicit callback expiry = %s, want approximately %s", registration.ExpiresAt, wantExpiry)
	}
	if removed := profile.expireTaskCallbacks(registration.ExpiresAt); removed != 1 {
		t.Fatalf("expired callbacks removed = %d", removed)
	}
	profile.TaskCallbacksMutex.RLock()
	remaining := len(profile.TaskCallbacks)
	profile.TaskCallbacksMutex.RUnlock()
	if remaining != 0 {
		t.Fatalf("expired callback remains in memory: %d", remaining)
	}
	if len(item.Task) != 1 || item.Task[0].Done {
		t.Fatalf("callback expiration changed underlying task: %#v", item.Task)
	}
	var responseID [8]byte
	copy(responseID[:], taskID)
	LuaOnResponse(responseID, "late", *item)
	if profile.state.GetGlobal("callback_called") == glua.LTrue {
		t.Fatal("expired task callback was called")
	}
	if profile.state.GetGlobal("global_response_called") != glua.LTrue {
		t.Fatal("late response did not fall back to global OnResponse")
	}
}

func TestBundledLuaScriptRegistersDefaultPayloadCommands(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	path := filepath.Join("..", "..", "script", "main.lua")
	profile, err := LuaNew(path)
	if err != nil {
		t.Fatalf("load bundled Lua script: %v", err)
	}
	t.Cleanup(profile.state.Close)
	t.Cleanup(func() { removeCommandsForScript(path) })
	t.Cleanup(func() { implantbuilder.UnregisterPayloadBuilders(path) })

	commands := LuaGetCommandDescriptions("impl")
	if len(commands) != 11 {
		t.Fatalf("bundled impl commands = %#v", commands)
	}
	if commands[0][0] != "cat" || commands[len(commands)-1][0] != "upload" {
		t.Fatalf("bundled commands are not sorted: %#v", commands)
	}
	builders := implantbuilder.PayloadBuilderDescriptions()
	if len(builders) != 1 || builders[0][0] != "implant-builder-linux-amd64" {
		t.Fatalf("bundled payload builders = %#v", builders)
	}
	buildProfile, err := implantbuilder.APIGetProfile("linux-impl")
	if err != nil {
		t.Fatal(err)
	}
	if buildProfile.Builder != "implant-builder-linux-amd64" {
		t.Fatalf("bundled profile builder = %q", buildProfile.Builder)
	}
}
