package lua

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"purpcmd/pkg/teamapi"
	serverimplant "purpcmd/server/implant"

	glua "github.com/yuin/gopher-lua"
)

func TestSessionDefaultsToExecutionContextAndPreservesExplicitLookup(t *testing.T) {
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

	profile := loadCommandTestProfile(t, "session-current.lua", "")
	item := serverimplant.ImplantNew("677222")
	item.Metadata.Type = "impl"
	item.Metadata.Hostname = "workstation"
	item.ImplantAddImplant()

	profile.executionSession = item.Name
	if err := profile.state.DoString(`implicit_info, implicit_err = session()`); err != nil {
		t.Fatal(err)
	}
	profile.executionSession = ""

	implicit, ok := profile.state.GetGlobal("implicit_info").(*glua.LTable)
	if !ok {
		t.Fatalf("implicit session = %s", profile.state.GetGlobal("implicit_info"))
	}
	if got := implicit.RawGetString("id").String(); got != item.Name {
		t.Fatalf("implicit session ID = %q", got)
	}
	if got := implicit.RawGetString("hostname").String(); got != item.Metadata.Hostname {
		t.Fatalf("implicit session hostname = %q", got)
	}
	if profile.state.GetGlobal("implicit_err") != glua.LNil {
		t.Fatalf("implicit session error = %s", profile.state.GetGlobal("implicit_err"))
	}

	if err := profile.state.DoString(`explicit_info, explicit_err = session("677222")`); err != nil {
		t.Fatal(err)
	}
	explicit, ok := profile.state.GetGlobal("explicit_info").(*glua.LTable)
	if !ok || explicit.RawGetString("id").String() != item.Name {
		t.Fatalf("explicit session = %s", profile.state.GetGlobal("explicit_info"))
	}
	if profile.state.GetGlobal("explicit_err") != glua.LNil {
		t.Fatalf("explicit session error = %s", profile.state.GetGlobal("explicit_err"))
	}

	// A selected legacy CLI session is intentionally not an execution context.
	serverimplant.CurrentImplant = item.Name
	if err := profile.state.DoString(`outside_info, outside_err = session()`); err != nil {
		t.Fatal(err)
	}
	if profile.state.GetGlobal("outside_info") != glua.LNil {
		t.Fatalf("out-of-context session = %s", profile.state.GetGlobal("outside_info"))
	}
	if got := profile.state.GetGlobal("outside_err").String(); got != "session() has no active session" {
		t.Fatalf("out-of-context error = %q", got)
	}
}

func TestSessionNoArgumentUsesAPICommandTargetWithoutContextLeakage(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	previousImplants := serverimplant.ImplantMAP
	previousCurrent := serverimplant.CurrentImplant
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	serverimplant.CurrentImplant = "unrelated-legacy-selection"
	t.Cleanup(func() {
		serverimplant.ImplantMAP = previousImplants
		serverimplant.CurrentImplant = previousCurrent
	})

	profile := loadCommandTestProfile(t, "session-command.lua", `
function identify(payload)
    local current, err = session()
    if err then return "session error: " .. err end
    if payload == "fail" then error("forced command failure") end
    return current.id .. ":" .. current.transport .. ":" .. payload
end
command("impl", "identify", "Identify command session", identify)
`)
	first := serverimplant.ImplantNew("session-a")
	first.Metadata.Type = "impl"
	first.ImplantAddImplant()
	second := serverimplant.ImplantNew("session-b")
	second.Metadata.Type = "impl"
	second.ImplantSetSpeaker("speaker-b", "speaker-b-uuid")
	second.ImplantAddImplant()

	for _, test := range []struct {
		session   string
		transport string
	}{
		{session: first.Name, transport: teamapi.SessionTransportListener},
		{session: second.Name, transport: teamapi.SessionTransportSpeaker},
	} {
		reply, err := APIExecuteCommand(teamapi.CommandExecuteRequest{
			Session: test.session, Name: "identify", Arguments: "single",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := test.session + ":" + test.transport + ":single"
		if reply.Message != want {
			t.Fatalf("command reply = %q, want %q", reply.Message, want)
		}
	}

	if _, err := APIExecuteCommand(teamapi.CommandExecuteRequest{
		Session: first.Name, Name: "identify", Arguments: "fail",
	}); err == nil || !strings.Contains(err.Error(), "forced command failure") {
		t.Fatalf("failing command error = %v", err)
	}
	profile.stateMu.Lock()
	if profile.executionSession != "" || profile.createdTaskIDs != nil {
		profile.stateMu.Unlock()
		t.Fatalf("context leaked after command failure: %q, %#v", profile.executionSession, profile.createdTaskIDs)
	}
	if err := profile.state.DoString(`after_failure_info, after_failure_err = session()`); err != nil {
		profile.stateMu.Unlock()
		t.Fatal(err)
	}
	if profile.state.GetGlobal("after_failure_info") != glua.LNil ||
		profile.state.GetGlobal("after_failure_err").String() != "session() has no active session" {
		profile.stateMu.Unlock()
		t.Fatalf("session after failure = %s, %s",
			profile.state.GetGlobal("after_failure_info"), profile.state.GetGlobal("after_failure_err"))
	}
	profile.stateMu.Unlock()

	type commandResult struct {
		got  string
		want string
		err  error
	}
	results := make(chan commandResult, 24)
	var wait sync.WaitGroup
	for index := 0; index < 24; index++ {
		item := first
		transport := teamapi.SessionTransportListener
		if index%2 == 1 {
			item = second
			transport = teamapi.SessionTransportSpeaker
		}
		argument := fmt.Sprintf("parallel-%d", index)
		wait.Add(1)
		go func() {
			defer wait.Done()
			reply, err := APIExecuteCommand(teamapi.CommandExecuteRequest{
				Session: item.Name, Name: "identify", Arguments: argument,
			})
			results <- commandResult{
				got: reply.Message, want: item.Name + ":" + transport + ":" + argument, err: err,
			}
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil || result.got != result.want {
			t.Fatalf("parallel command = %q, %v; want %q", result.got, result.err, result.want)
		}
	}
}

func TestSessionNoArgumentIsAvailableToLifecycleAndTaskCallbacks(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	previousImplants := serverimplant.ImplantMAP
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	t.Cleanup(func() { serverimplant.ImplantMAP = previousImplants })

	profile := loadCommandTestProfile(t, "session-callback.lua", `
function capture_session(target)
    local current, err = session()
    if err then error(err) end
    _G[target] = current.id
end
function OnRegister(...) capture_session("register_session") end
function OnCheck(...) capture_session("check_session") end
function OnResponse(...) capture_session("response_session") end
function callback_command(payload)
    local task_id, err = add_task(42, payload)
    if err then error(err) end
    register_task_callback(task_id, function(...)
        capture_session("task_callback_session")
    end)
    return task_id
end
command("impl", "callback-command", "Register callback", callback_command)
`)
	item := serverimplant.ImplantNew("callback-session")
	item.Metadata.Type = "impl"
	item.ImplantAddImplant()

	LuaOnRegister(*item)
	LuaOnCheck([8]byte{'c', 'h', 'e', 'c', 'k', '0', '0', '1'}, "", *item)
	reply, err := APIExecuteCommand(teamapi.CommandExecuteRequest{
		Session: item.Name, Name: "callback-command", Arguments: "payload",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.TaskIDs) != 1 {
		t.Fatalf("created tasks = %#v", reply.TaskIDs)
	}
	var taskID [8]byte
	copy(taskID[:], reply.TaskIDs[0])
	LuaOnResponse(taskID, "done", *item)
	LuaOnResponse([8]byte{'o', 't', 'h', 'e', 'r', '0', '0', '1'}, "done", *item)

	for _, global := range []string{
		"register_session", "check_session", "task_callback_session", "response_session",
	} {
		if got := profile.state.GetGlobal(global).String(); got != item.Name {
			t.Fatalf("%s = %q", global, got)
		}
	}
	if profile.executionSession != "" || profile.createdTaskIDs != nil {
		t.Fatalf("callback context leaked: %q, %#v", profile.executionSession, profile.createdTaskIDs)
	}
}

func TestSessionExecutionContextRestoresAfterSuccessAndFailure(t *testing.T) {
	profile := &LuaProfile{
		executionSession: "outer-session",
		createdTaskIDs:   []string{"outer-task"},
	}

	restore := profile.enterSessionExecution("inner-session")
	if profile.executionSession != "inner-session" || profile.createdTaskIDs != nil {
		t.Fatalf("entered context = %q, %#v", profile.executionSession, profile.createdTaskIDs)
	}
	profile.createdTaskIDs = append(profile.createdTaskIDs, "inner-task")
	restore()
	if profile.executionSession != "outer-session" || len(profile.createdTaskIDs) != 1 || profile.createdTaskIDs[0] != "outer-task" {
		t.Fatalf("restored context = %q, %#v", profile.executionSession, profile.createdTaskIDs)
	}

	func() {
		defer func() { _ = recover() }()
		restoreAfterFailure := profile.enterSessionExecution("failing-session")
		defer restoreAfterFailure()
		panic("representative Lua failure")
	}()
	if profile.executionSession != "outer-session" || len(profile.createdTaskIDs) != 1 || profile.createdTaskIDs[0] != "outer-task" {
		t.Fatalf("context after failure = %q, %#v", profile.executionSession, profile.createdTaskIDs)
	}
}
