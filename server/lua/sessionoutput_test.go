package lua

import (
	"strings"
	"testing"

	"purpcmd/pkg/teamapi"
	serverimplant "purpcmd/server/implant"
	"purpcmd/server/runtimeevents"

	lua "github.com/yuin/gopher-lua"
)

func TestSessionPrintPublishesExplicitSessionAndTask(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	previousImplants := serverimplant.ImplantMAP
	previousCurrent := serverimplant.CurrentImplant
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	serverimplant.CurrentImplant = "none"
	t.Cleanup(func() {
		serverimplant.ImplantMAP = previousImplants
		serverimplant.CurrentImplant = previousCurrent
		runtimeevents.SetPublisher(nil)
	})

	profile := loadCommandTestProfile(t, "session-output.lua", "")
	item := serverimplant.ImplantNew("677222")
	item.ImplantAddImplant()
	profile.executionSession = item.Name
	if err := profile.state.DoString(`task_id, task_err, task_session_id = add_task(7, "")`); err != nil {
		t.Fatal(err)
	}
	profile.executionSession = ""
	taskID := profile.state.GetGlobal("task_id").String()

	var eventType string
	var output teamapi.SessionOutput
	runtimeevents.SetPublisher(func(gotType string, value any) {
		eventType = gotType
		var ok bool
		output, ok = value.(teamapi.SessionOutput)
		if !ok {
			t.Fatalf("session output event value = %T", value)
		}
	})

	if err := profile.state.DoString(`session_print(677222, "Current directory: /tmp", task_id)`); err != nil {
		t.Fatal(err)
	}
	if eventType != teamapi.EventSessionOutput {
		t.Fatalf("event type = %q", eventType)
	}
	want := teamapi.SessionOutput{
		Session: item.Name,
		TaskID:  taskID,
		Message: "Current directory: /tmp",
		Source:  luaSessionOutputSource,
	}
	if output != want {
		t.Fatalf("session output = %#v, want %#v", output, want)
	}
}

func TestSessionPrintRejectsTaskFromAnotherSession(t *testing.T) {
	isolateLuaCommands(t)
	isolateLuaDatabase(t)
	previousImplants := serverimplant.ImplantMAP
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	t.Cleanup(func() {
		serverimplant.ImplantMAP = previousImplants
		runtimeevents.SetPublisher(nil)
	})

	profile := loadCommandTestProfile(t, "session-output-owner.lua", "")
	first := serverimplant.ImplantNew("first")
	first.ImplantAddImplant()
	second := serverimplant.ImplantNew("second")
	second.ImplantAddImplant()
	profile.executionSession = first.Name
	if err := profile.state.DoString(`task_id = add_task(7, "")`); err != nil {
		t.Fatal(err)
	}
	profile.executionSession = ""

	published := false
	runtimeevents.SetPublisher(func(string, any) { published = true })
	if err := profile.state.DoString(`ok, call_err = pcall(session_print, "second", "wrong session", task_id)`); err != nil {
		t.Fatal(err)
	}
	if profile.state.GetGlobal("ok").String() != "false" {
		t.Fatal("session_print accepted a task from another session")
	}
	if got := profile.state.GetGlobal("call_err").String(); !strings.Contains(got, "does not belong") {
		t.Fatalf("session_print error = %q", got)
	}
	if published {
		t.Fatal("invalid session output was published")
	}
}

func TestSessionPrintRejectsOversizedMessages(t *testing.T) {
	isolateLuaCommands(t)
	profile := loadCommandTestProfile(t, "session-output-size.lua", "")
	profile.state.SetGlobal("oversized", profile.state.NewFunction(func(state *lua.LState) int {
		state.Push(lua.LString(strings.Repeat("x", teamapi.MaxSessionOutputMessage+1)))
		return 1
	}))
	if err := profile.state.DoString(`ok, call_err = pcall(session_print, "session", oversized(), "12345678")`); err != nil {
		t.Fatal(err)
	}
	if profile.state.GetGlobal("ok").String() != "false" {
		t.Fatal("session_print accepted an oversized message")
	}
	if got := profile.state.GetGlobal("call_err").String(); !strings.Contains(got, "must not exceed") {
		t.Fatalf("session_print error = %q", got)
	}
}
