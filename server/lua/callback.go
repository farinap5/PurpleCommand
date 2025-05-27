package lua

func LuaOnRegister() {
	for _, v := range(ScriptMAP) {
		go v.state.DoString("OnRegister()")
	}
}