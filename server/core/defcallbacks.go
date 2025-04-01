package core

import (
	"os"
	"purpcmd/server/implant"
	"purpcmd/server/listener"
	"purpcmd/server/log"
	"purpcmd/server/lua"
	"purpcmd/server/types"
)

func runHelp(cmds []string, p *types.Profile) int {
	if len(cmds) >= 2 {
		cmdPtr := Mapping[cmds[1]]
		if cmdPtr != nil {
			functionP := *cmdPtr
			if functionP.Usage == nil {
				println("The command does not have a valid usage callback.")
				println(functionP.Desc)
			} else {
				functionP.Usage(cmds)
			}
		} else {
			println("The command does not have help menu.")
		}
	} else {
		CmdHelp(p)
	}
	return 0
}

func runExit(cmds []string, profile *types.Profile) int {
	// Exits the program
	HandleExit()
	os.Exit(0)
	return 0 // wont run
}

func runListener(cmds []string, profile *types.Profile) int {
	if profile.Session {
		println("session is in use")
		return 0
	} else if profile.Script {
		println("script is in use")
		return 0
	}

	if !profile.Listener {
		profile.Listener = true
		profile.Prompt = "(listener - " + listener.CurrentListener + ")>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	}
	return 0
}

func runSession(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		println("listener is in use")
		return 0
	} else if profile.Script {
		println("script is in use")
		return 0
	}

	if !profile.Session {
		profile.Session = true
		profile.Prompt = "(session - " + implant.CurrentImplant + ")>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	}
	return 0
}

func runScript(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		println("listener is in use")
		return 0
	} else if profile.Session {
		println("session is in use")
		return 0
	}

	if !profile.Script {
		profile.Script = true
		profile.Prompt = "(script)>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	}
	return 0
}

func runNew(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		if len(cmds) == 2 {
			listener.ListenerNew(cmds[1])

			profile.Prompt = "(listener - " + listener.CurrentListener + ")>> "
			LivePrefixState.LivePrefix = profile.Prompt
			LivePrefixState.IsEnable = true
		} else {
			println("error")
		}
	}

	return 0
}

func runOptions(cmds []string, profile *types.Profile) int {
	if profile.Listener {
			listener.ListenerShowOptions()
	}

	return 0
}

func runList(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		listener.ListenerList()
	} else if profile.Session {
		implant.ImplantList()
	} else if profile.Script {
		lua.ScriptList()
	}

	return 0
}

func runSet(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		if len(cmds) == 3 {
			listener.ListenerSetOptions(cmds[1], cmds[2])
		} else {
			println("error")
		}
	}

	return 0
}

func runRun(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		listener.ListenerStart()
	}

	return 0
}

func runStop(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		listener.ListenerStop()
	}

	return 0
}

func runInteract(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		if len(cmds) == 2 {
			err := listener.ListenerInteract(cmds[1])
			if err != nil {
				log.PrintErr(err.Error())
				return 1
			}

			profile.Prompt = "(listener - " + listener.CurrentListener + ")>> "
			LivePrefixState.LivePrefix = profile.Prompt
			LivePrefixState.IsEnable = true
		}
	} else if profile.Session {
		if len(cmds) == 2 {
			err := implant.ImplantInteract(cmds[1])
			if err != nil {
				log.PrintErr(err.Error())
				return 1
			}

			profile.Prompt = "(session - " + implant.CurrentImplant + ")>> "
			LivePrefixState.LivePrefix = profile.Prompt
			LivePrefixState.IsEnable = true
		}
	} else {
		println("error")
	}
	return 0
}

func runDelete(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		err := listener.ListenerDelete()
		if err != nil {
			println(err.Error())
		} else {
			profile.Prompt = "(listener - " + listener.CurrentListener + ")>> "
			LivePrefixState.LivePrefix = profile.Prompt
			LivePrefixState.IsEnable = true
		}
	}

	return 0
}

func runBack(cmds []string, profile *types.Profile) int {
	if profile.Listener {
		profile.Listener = false
	} else if profile.Session {
		profile.Session = false
	}

	profile.Prompt = CreateDefaultPrompt()
	LivePrefixState.LivePrefix = profile.Prompt
	LivePrefixState.IsEnable = true

	return 0
}

func runPing(cmds []string, profile *types.Profile) int {
	if !profile.Session {
		return 1
	}

	implant.ImplantAddTask()

	return 0
}


func runLoad(cmds []string, profile *types.Profile) int {
	if !profile.Script {
		return 1
	}

	if len(cmds) == 2 {
		lua.LuaLoad(cmds[1])
	}

	return 0
}
