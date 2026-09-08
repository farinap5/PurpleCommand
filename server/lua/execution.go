package lua

// enterSessionExecution installs the session context visible to session(),
// add_task(), and register_task_callback(). The caller must hold stateMu for
// the entire Lua invocation. Returning a restoration closure makes cleanup
// reliable on both normal returns and Lua errors, while preserving any outer
// context if Lua invocation becomes nestable in the future.
func (profile *LuaProfile) enterSessionExecution(session string) func() {
	previousSession := profile.executionSession
	previousTaskIDs := profile.createdTaskIDs
	profile.executionSession = session
	profile.createdTaskIDs = nil
	return func() {
		profile.executionSession = previousSession
		profile.createdTaskIDs = previousTaskIDs
	}
}
