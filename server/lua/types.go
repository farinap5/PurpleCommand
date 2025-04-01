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