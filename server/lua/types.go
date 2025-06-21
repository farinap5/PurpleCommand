package lua

import (
	"context"
	"purpcmd/server/types"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type LuaProfile struct {
	script 	string
	state 	*lua.LState
	Profile *types.Profile
	Running  bool

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	closing sync.Once
}


type command_def struct {
	Impl string
	Name string
	Description string
	ScriptName string

	ptr *lua.LFunction
}