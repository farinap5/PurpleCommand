package lua

import (
	"context"
	"errors"
	"math"
	"time"

	"purpcmd/server/implant"

	lua "github.com/yuin/gopher-lua"
)

const (
	taskCallbackSweepInterval = time.Second
	zeroSleepCallbackTimeout  = 30 * time.Second
)

func (profile *LuaProfile) registerTaskCallback(state *lua.LState) int {
	taskID := state.CheckString(1)
	callback := state.CheckFunction(2)
	sessionName := profile.taskSession(state)

	task, err := implant.APIGetTask(sessionName, taskID)
	if err != nil {
		state.RaiseError("cannot register callback: %v", err)
		return 0
	}
	if task.Status == "completed" {
		state.RaiseError("cannot register callback: task %q is already completed", taskID)
		return 0
	}

	timeout := defaultTaskCallbackTimeout(sessionName)
	if state.GetTop() >= 3 && state.Get(3) != lua.LNil {
		timeout, err = callbackTimeoutFromSeconds(float64(state.CheckNumber(3)))
		if err != nil {
			state.ArgError(3, err.Error())
			return 0
		}
	}

	key := taskCallbackKey{Session: sessionName, TaskID: taskID}
	profile.TaskCallbacksMutex.Lock()
	if profile.TaskCallbacks == nil {
		profile.TaskCallbacks = make(map[taskCallbackKey]taskCallbackRegistration)
	}
	profile.TaskCallbacks[key] = taskCallbackRegistration{
		Function:  callback,
		ExpiresAt: time.Now().Add(timeout),
	}
	profile.TaskCallbacksMutex.Unlock()
	return 0
}

func callbackTimeoutFromSeconds(seconds float64) (time.Duration, error) {
	maximumSeconds := float64(math.MaxInt64) / float64(time.Second)
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > maximumSeconds {
		return 0, errors.New("timeout must be a positive number of seconds")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func defaultTaskCallbackTimeout(sessionName string) time.Duration {
	session, err := implant.APIGetSession(sessionName)
	if err != nil || session.Sleep == 0 {
		return zeroSleepCallbackTimeout
	}
	timeout, err := callbackTimeoutFromSeconds(float64(session.Sleep) * 2.5)
	if err != nil {
		return zeroSleepCallbackTimeout
	}
	return timeout
}

func (profile *LuaProfile) takeTaskCallback(sessionName, taskID string, now time.Time) *lua.LFunction {
	key := taskCallbackKey{Session: sessionName, TaskID: taskID}
	profile.TaskCallbacksMutex.Lock()
	registration, found := profile.TaskCallbacks[key]
	if found {
		delete(profile.TaskCallbacks, key)
	}
	profile.TaskCallbacksMutex.Unlock()
	if !found || !now.Before(registration.ExpiresAt) {
		return nil
	}
	return registration.Function
}

func (profile *LuaProfile) expireTaskCallbacks(now time.Time) int {
	removed := 0
	profile.TaskCallbacksMutex.Lock()
	for key, registration := range profile.TaskCallbacks {
		_, sessionErr := implant.APIGetSession(key.Session)
		if !now.Before(registration.ExpiresAt) || sessionErr != nil {
			delete(profile.TaskCallbacks, key)
			removed++
		}
	}
	profile.TaskCallbacksMutex.Unlock()
	return removed
}

func (profile *LuaProfile) startTaskCallbackCleaner() {
	profile.ctx, profile.cancel = context.WithCancel(context.Background())
	profile.done = make(chan struct{})
	go func() {
		defer close(profile.done)
		ticker := time.NewTicker(taskCallbackSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				profile.expireTaskCallbacks(now)
			case <-profile.ctx.Done():
				return
			}
		}
	}()
}

func (profile *LuaProfile) stopTaskCallbackCleaner() {
	profile.closing.Do(func() {
		if profile.cancel != nil {
			profile.cancel()
		}
		if profile.done != nil {
			<-profile.done
		}
		profile.TaskCallbacksMutex.Lock()
		profile.TaskCallbacks = make(map[taskCallbackKey]taskCallbackRegistration)
		profile.TaskCallbacksMutex.Unlock()
	})
}
