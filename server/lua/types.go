package lua

import (
	"context"
	"sync"

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

	TaskCallbacks      map[string]*lua.LFunction
	TaskCallbacksMutex sync.RWMutex
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
