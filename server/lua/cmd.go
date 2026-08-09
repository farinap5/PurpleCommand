package lua

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"purpcmd/internal"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/implant"
	"purpcmd/server/log"
	"purpcmd/server/runtimeevents"
	"purpcmd/server/uploads"

	lua "github.com/yuin/gopher-lua"
)

var (
	CMDMAP   = make(map[commandKey]*commandDef)
	cmdMapMu sync.RWMutex
)

func LuaGetCommandDescriptions(payloadType string) [][]string {
	if internal.ValidatePayloadType(payloadType) != nil {
		return nil
	}
	cmdMapMu.RLock()
	defer cmdMapMu.RUnlock()
	var result [][]string
	for key, command := range CMDMAP {
		if key.Type == payloadType {
			result = append(result, []string{command.Name, command.Description})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i][0] < result[j][0] })
	return result
}

func LuaGetCommandDesc(payloadType string, _ ...string) [][]string {
	return LuaGetCommandDescriptions(payloadType)
}

func APIListCommands(payloadType string) []teamapi.Command {
	cmdMapMu.RLock()
	defer cmdMapMu.RUnlock()
	result := make([]teamapi.Command, 0, len(CMDMAP))
	for key, command := range CMDMAP {
		if payloadType == "" || key.Type == payloadType {
			result = append(result, teamapi.Command{
				PayloadType: command.Type,
				Name:        command.Name,
				Description: command.Description,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PayloadType == result[j].PayloadType {
			return result[i].Name < result[j].Name
		}
		return result[i].PayloadType < result[j].PayloadType
	})
	return result
}

func (profile *LuaProfile) command(state *lua.LState) int {
	payloadType := state.CheckString(1)
	name := state.CheckString(2)
	description := state.CheckString(3)
	function := state.CheckFunction(4)
	if err := internal.ValidatePayloadType(payloadType); err != nil {
		state.ArgError(1, err.Error())
		return 0
	}
	if err := internal.ValidateCommandName(name); err != nil {
		state.ArgError(2, err.Error())
		return 0
	}
	key := commandKey{Type: payloadType, Name: name}
	cmdMapMu.Lock()
	defer cmdMapMu.Unlock()
	if existing := CMDMAP[key]; existing != nil {
		state.RaiseError("command %q for payload type %q is already registered by %s", name, payloadType, existing.ScriptName)
		return 0
	}
	CMDMAP[key] = &commandDef{
		Type: payloadType, Name: name, Description: description,
		ptr: function, ScriptName: profile.script,
	}
	return 0
}

func removeCommandsForScript(scriptName string) {
	cmdMapMu.Lock()
	defer cmdMapMu.Unlock()
	for key, command := range CMDMAP {
		if command.ScriptName == scriptName {
			delete(CMDMAP, key)
		}
	}
}

func (profile *LuaProfile) taskSession(state *lua.LState) string {
	if profile.executionSession == "" || profile.executionSession == "none" {
		state.RaiseError("command has no target session")
		return ""
	}
	return profile.executionSession
}

func (profile *LuaProfile) rememberTask(taskID string) {
	if taskID != "" {
		profile.createdTaskIDs = append(profile.createdTaskIDs, taskID)
	}
}

func (profile *LuaProfile) implantAddUploadFileCommand(state *lua.LState) int {
	code := state.CheckInt(1)
	source := state.CheckString(2)
	destination := state.CheckString(3)
	session := profile.taskSession(state)
	content, err := os.ReadFile(source)
	if err != nil {
		state.Push(lua.LNil)
		state.Push(lua.LString("could not read upload source: " + err.Error()))
		return 2
	}
	taskID, result := implant.ImplantAddUploadTaskFor(session, code, destination, content)
	if result != 0 {
		state.Push(lua.LNil)
		state.Push(lua.LString("could not create task"))
		return 2
	}
	profile.rememberTask(taskID)
	state.Push(lua.LString(taskID))
	return 1
}

func (profile *LuaProfile) implantAddSendBuffer(state *lua.LState) int {
	code := state.CheckInt(1)
	destination := state.CheckString(2)
	content := state.CheckString(3)
	session := profile.taskSession(state)
	taskID, result := implant.ImplantAddUploadTaskFor(session, code, destination, []byte(content))
	if result != 0 {
		state.Push(lua.LNil)
		state.Push(lua.LString("could not create task"))
		return 2
	}
	profile.rememberTask(taskID)
	state.Push(lua.LString(taskID))
	return 1
}

func (profile *LuaProfile) implantAddGenericTask(state *lua.LState) int {
	code := state.CheckInt(1)
	payload := state.CheckString(2)
	session := profile.taskSession(state)
	taskID, result := implant.ImplantAddGenericTaskFor(session, code, payload)
	if result != 0 {
		state.Push(lua.LNil)
		state.Push(lua.LString("could not create task"))
		return 2
	}
	profile.rememberTask(taskID)
	state.Push(lua.LString(taskID))
	return 1
}

func (profile *LuaProfile) registerTaskCallback(state *lua.LState) int {
	taskID := state.CheckString(1)
	callback := state.CheckFunction(2)
	profile.TaskCallbacksMutex.Lock()
	profile.TaskCallbacks[taskID] = callback
	profile.TaskCallbacksMutex.Unlock()
	return 0
}

func LuaPrint(state *lua.LState) int {
	args := make([]interface{}, state.GetTop())
	text := ""
	for index := 1; index <= state.GetTop(); index++ {
		value := state.Get(index).String()
		args[index-1] = value
		text += value
	}
	log.AsyncWriteStdout(args...)
	runtimeevents.Publish(teamapi.EventScriptOutput, map[string]string{"message": text})
	return 0
}

func commandDefinition(name, payloadType string) (*commandDef, error) {
	if err := internal.ValidatePayloadType(payloadType); err != nil {
		return nil, err
	}
	if err := internal.ValidateCommandName(name); err != nil {
		return nil, err
	}
	cmdMapMu.RLock()
	command := CMDMAP[commandKey{Type: payloadType, Name: name}]
	cmdMapMu.RUnlock()
	if command == nil {
		return nil, fmt.Errorf("command %q for payload type %q not found", name, payloadType)
	}
	return command, nil
}

func callCommandForSession(session, name, payloadType, payload string) (string, []string, error) {
	command, err := commandDefinition(name, payloadType)
	if err != nil {
		return "", nil, err
	}
	scriptMapMu.RLock()
	profile := ScriptMAP[command.ScriptName]
	scriptMapMu.RUnlock()
	if profile == nil {
		return "", nil, fmt.Errorf("script %q for command %q is not loaded", command.ScriptName, name)
	}
	profile.stateMu.Lock()
	defer profile.stateMu.Unlock()
	if profile.state == nil {
		return "", nil, fmt.Errorf("script %q was unloaded", command.ScriptName)
	}
	profile.executionSession = session
	profile.createdTaskIDs = nil
	defer func() {
		profile.executionSession = ""
		profile.createdTaskIDs = nil
	}()

	state := profile.state
	state.Push(command.ptr)
	state.Push(lua.LString(payload))
	if err := state.PCall(1, 1, nil); err != nil {
		return "", nil, err
	}
	result := state.ToString(-1)
	state.Pop(1)
	taskIDs := append([]string(nil), profile.createdTaskIDs...)
	return result, taskIDs, nil
}

func CallCommand(name, payloadType, payload string) (string, error) {
	result, _, err := callCommandForSession(implant.CurrentImplant, name, payloadType, payload)
	return result, err
}

func APIExecuteCommand(request teamapi.CommandExecuteRequest) (teamapi.CommandExecuteReply, error) {
	cleanup, err := materializeAttachments(&request)
	if err != nil {
		return teamapi.CommandExecuteReply{}, err
	}
	defer cleanup()
	session, err := implant.APIGetSession(request.Session)
	if err != nil {
		return teamapi.CommandExecuteReply{}, err
	}
	result, taskIDs, err := callCommandForSession(request.Session, request.Name, session.PayloadType, request.Arguments)
	if err != nil {
		return teamapi.CommandExecuteReply{}, err
	}
	return teamapi.CommandExecuteReply{TaskIDs: taskIDs, Message: result}, nil
}

func materializeAttachments(request *teamapi.CommandExecuteRequest) (func(), error) {
	if len(request.Attachments) == 0 {
		return func() {}, nil
	}
	paths := make([]string, 0, len(request.Attachments))
	cleanup := func() {
		for _, fileName := range paths {
			_ = os.Remove(fileName)
		}
	}
	for token, uploadID := range request.Attachments {
		if !strings.HasPrefix(token, "@") || len(token) > 4096 {
			cleanup()
			return func() {}, fmt.Errorf("invalid attachment token %q", token)
		}
		fileName, err := uploads.Default.Take(uploadID)
		if err != nil {
			cleanup()
			return func() {}, fmt.Errorf("resolve attachment %q: %w", token, err)
		}
		paths = append(paths, fileName)
		request.Arguments = strings.ReplaceAll(request.Arguments, token, fileName)
	}
	return cleanup, nil
}
