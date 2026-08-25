package lua

import (
	"fmt"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/implant"
	"purpcmd/server/runtimeevents"

	lua "github.com/yuin/gopher-lua"
)

const luaSessionOutputSource = "lua"

// sessionPrint publishes operator-facing Lua output for a specific task and
// session. Lua signature: session_print(session_id, message, task_id).
func (profile *LuaProfile) sessionPrint(state *lua.LState) int {
	sessionID := luaSessionName(state)
	message := state.CheckString(2)
	taskID := state.CheckString(3)

	if len(message) > teamapi.MaxSessionOutputMessage {
		state.ArgError(2, fmt.Sprintf("message must not exceed %d bytes", teamapi.MaxSessionOutputMessage))
		return 0
	}
	if _, err := implant.APIGetSession(sessionID); err != nil {
		state.ArgError(1, fmt.Sprintf("session %q was not found", sessionID))
		return 0
	}
	if _, err := implant.APIGetTask(sessionID, taskID); err != nil {
		state.ArgError(3, fmt.Sprintf("task %q does not belong to session %q: %v", taskID, sessionID, err))
		return 0
	}

	runtimeevents.Publish(teamapi.EventSessionOutput, teamapi.SessionOutput{
		Session: sessionID,
		TaskID:  taskID,
		Message: message,
		Source:  luaSessionOutputSource,
	})
	return 0
}
