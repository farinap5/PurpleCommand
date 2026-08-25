package lua

import (
	"context"
	"sync"
	"time"

	"purpcmd/server/types"

	lua "github.com/yuin/gopher-lua"
)

type LuaProfile struct {
	script  string
	state   *lua.LState
	Profile *types.Profile
	Running bool

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	closing sync.Once
	stateMu sync.Mutex

	executionSession string
	createdTaskIDs   []string

	TaskCallbacks      map[taskCallbackKey]taskCallbackRegistration
	TaskCallbacksMutex sync.RWMutex
}

type taskCallbackKey struct {
	Session string
	TaskID  string
}

type taskCallbackRegistration struct {
	Function  *lua.LFunction
	ExpiresAt time.Time
}

type commandKey struct {
	Type string
	Name string
}

type commandDef struct {
	Type        string
	Name        string
	Description string
	ScriptName  string
	ptr         *lua.LFunction
}
