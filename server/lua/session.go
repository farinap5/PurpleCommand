package lua

import (
	"math"
	"strconv"
	"strings"
	"time"

	"purpcmd/server/implant"

	lua "github.com/yuin/gopher-lua"
)

func (profile *LuaProfile) session(state *lua.LState) int {
	name := luaSessionName(state)
	item, err := implant.APIGetSession(name)
	if err != nil {
		state.Push(lua.LNil)
		state.Push(lua.LString(err.Error()))
		return 2
	}

	table := state.NewTable()
	setSessionString(table, "id", item.Name)
	setSessionString(table, "name", item.Name)
	setSessionString(table, "session_id", item.Name)
	setSessionString(table, "uuid", item.UUID)
	setSessionString(table, "payload_type", item.PayloadType)
	setSessionString(table, "transport", item.Transport)
	setSessionString(table, "speaker", item.Speaker)
	setSessionString(table, "user", item.User)
	setSessionString(table, "hostname", item.Hostname)
	setSessionString(table, "process", item.Process)
	setSessionString(table, "socket", item.Socket)
	table.RawSetString("pid", lua.LNumber(item.PID))
	table.RawSetString("sleep", lua.LNumber(item.Sleep))
	table.RawSetString("alive", lua.LBool(item.Alive))
	table.RawSetString("terminating", lua.LBool(item.Terminating))
	status := "dead"
	if item.Terminating {
		status = "terminating"
	} else if item.Alive {
		status = "alive"
	}
	setSessionString(table, "status", status)
	setSessionTime(table, "first_seen", item.FirstSeen)
	setSessionTime(table, "last_seen", item.LastSeen)

	state.Push(table)
	state.Push(lua.LNil)
	return 2
}

func luaSessionName(state *lua.LState) string {
	value := state.CheckAny(1)
	var name string
	switch typed := value.(type) {
	case lua.LString:
		name = string(typed)
	case lua.LNumber:
		number := float64(typed)
		if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || math.Trunc(number) != number {
			state.ArgError(1, "session ID must be a string or non-negative integer")
			return ""
		}
		name = strconv.FormatFloat(number, 'f', -1, 64)
	default:
		state.ArgError(1, "session ID must be a string or non-negative integer")
		return ""
	}
	name = strings.TrimSpace(name)
	if name == "" {
		state.ArgError(1, "session ID is required")
	}
	return name
}

func setSessionString(table *lua.LTable, key, value string) {
	table.RawSetString(key, lua.LString(value))
}

func setSessionTime(table *lua.LTable, key string, value time.Time) {
	table.RawSetString(key, lua.LString(value.Format(time.RFC3339Nano)))
}
