package core

import (
	"os"
	"purpcmd/server/implant"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/listener"
	"purpcmd/server/log"
	"purpcmd/server/loot"
	"purpcmd/server/lua"
	"purpcmd/server/types"
	"strings"
)

const (
	ErrStateNotNil = "must back to main menu: type `back` or press `ctrl b`"
	ErrStateNil    = "already in main menu"
)

func runHelp(cmds []string, p *types.Profile) int {
	if len(cmds) >= 2 {
		cmdPtr := commandMap[cmds[1]]
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
	if profile.STATE == types.NIL {
		profile.STATE = types.LISTENER
		profile.Prompt = "(listener - " + listener.CurrentListener + ")>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	} else {
		log.PrintErr(ErrStateNotNil)
	}
	return 0
}

func runSession(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.NIL {
		profile.STATE = types.SESSION
		profile.Prompt = "(session - " + implant.CurrentImplant + ")>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	} else {
		log.PrintErr(ErrStateNotNil)
	}
	return 0
}

func runScript(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.NIL {
		profile.STATE = types.SCRIPT
		profile.Prompt = "(script)>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	} else {
		log.PrintErr(ErrStateNotNil)
	}
	return 0
}

func runLoot(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.NIL {
		profile.STATE = types.LOOT
		profile.Prompt = "(loot)>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	} else {
		log.PrintErr(ErrStateNotNil)
	}
	return 0
}

func runImplantMenu(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.NIL {
		profile.STATE = types.IMPLANT_BUILD
		profile.Prompt = "(implant)>> "
		LivePrefixState.LivePrefix = profile.Prompt
		LivePrefixState.IsEnable = true
	} else {
		log.PrintErr(ErrStateNotNil)
	}
	return 0
}

func runGenerate(cmds []string, profile *types.Profile) int {
	if profile.STATE != types.IMPLANT_BUILD {
		return 0
	}
	var err error
	if len(cmds) == 2 {
		err = implantbuilder.GenerateByName(cmds[1])
	} else {
		err = implantbuilder.Generate()
	}
	if err != nil {
		log.PrintErr(err.Error())
		return 1
	}
	return 0
}

func runSelectProfile(cmds []string, profile *types.Profile) int {
	if profile.STATE != types.IMPLANT_BUILD {
		return 0
	}
	if len(cmds) != 2 {
		println("usage: select <profile-name>")
		return 1
	}
	err := implantbuilder.SelectProfile(cmds[1])
	if err != nil {
		log.PrintErr(err.Error())
		return 1
	}
	return 0
}

func runNew(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
		if len(cmds) == 2 {
			err := listener.ListenerNew(cmds[1])
			if err != nil {
				log.PrintErr(err.Error())
				return 1
			}

			profile.Prompt = "(listener - " + listener.CurrentListener + ")>> "
			LivePrefixState.LivePrefix = profile.Prompt
			LivePrefixState.IsEnable = true
		} else {
			println("error")
		}
	} else if profile.STATE == types.IMPLANT_BUILD {
		// syntax: new profile <name>
		if len(cmds) == 3 && cmds[1] == "profile" {
			if err := implantbuilder.NewProfile(cmds[2]); err != nil {
				log.PrintErr(err.Error())
				return 1
			}
		} else {
			println("usage: new profile <name>")
			return 1
		}
	}

	return 0
}

func runOptions(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
		listener.ListenerShowOptions()
	} else if profile.STATE == types.IMPLANT_BUILD {
		implantbuilder.ShowOptions()
	}

	return 0
}

func runList(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
		listener.ListenerList()
	} else if profile.STATE == types.SESSION {
		implant.ImplantList()
	} else if profile.STATE == types.SCRIPT {
		lua.ScriptList()
	} else if profile.STATE == types.LOOT {
		loot.List()
	} else if profile.STATE == types.IMPLANT_BUILD {
		implantbuilder.ListProfiles()
	}

	return 0
}

func runSet(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
		if len(cmds) == 3 {
			err := listener.ListenerSetOptions(cmds[1], cmds[2])
			if err != nil {
				log.PrintErr(err.Error())
				return 1
			}
		} else {
			println("error")
		}
	} else if profile.STATE == types.IMPLANT_BUILD {
		if len(cmds) == 3 {
			err := implantbuilder.SetOption(cmds[1], cmds[2])
			if err != nil {
				log.PrintErr(err.Error())
				return 1
			}
		} else {
			println("usage: set <option> <value>")
		}
	}

	return 0
}

func runRun(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
		listener.ListenerStart()
	}

	return 0
}

func runStop(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
		listener.ListenerStop()
	}

	return 0
}

func runRestart(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
		listener.ListenerRestart()
	}

	return 0
}

func runInteract(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LISTENER {
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
	} else if profile.STATE == types.SESSION {
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
	if profile.STATE == types.LISTENER {
		err := listener.ListenerDelete()
		if err != nil {
			println(err.Error())
		} else {
			profile.Prompt = "(listener - " + listener.CurrentListener + ")>> "
			LivePrefixState.LivePrefix = profile.Prompt
			LivePrefixState.IsEnable = true
		}
	} else if profile.STATE == types.IMPLANT_BUILD {
		if len(cmds) != 2 {
			println("usage: delete <profile-name>")
			return 1
		}
		if err := implantbuilder.DeleteProfile(cmds[1]); err != nil {
			log.PrintErr(err.Error())
			return 1
		}
	}

	return 0
}

func runBack(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.NIL {
		log.PrintErr(ErrStateNil)
		return 1
	}

	profile.STATE = types.NIL
	profile.Prompt = CreateDefaultPrompt()
	LivePrefixState.LivePrefix = profile.Prompt
	LivePrefixState.IsEnable = false

	return 0
}

/*func runPing(cmds []string, profile *types.Profile) int {
	if !profile.Session {
		return 1
	}

	implant.ImplantAddTask()

	return 0
}*/

func runLoad(cmds []string, profile *types.Profile) int {
	if profile.STATE != types.SCRIPT {
		return 1
	}

	if len(cmds) == 2 {
		lua.LuaLoad(cmds[1])
	}

	return 0
}

func runUnload(cmds []string, profile *types.Profile) int {
	if profile.STATE != types.SCRIPT {
		return 1
	}

	if len(cmds) == 2 {
		lua.LuaUnload(cmds[1])
	}

	return 0
}

func runExport(cmds []string, profile *types.Profile) int {
	if profile.STATE == types.LOOT {
		if len(cmds) != 3 {
			log.PrintErr("type: export <uuid> </path/to/save>")
			return 1
		}
		err := loot.Export(cmds[1], cmds[2])
		if err != nil {
			log.PrintErr(err.Error())
			return 1
		}
	}
	return 0
}

func runTaskCall(cmds []string) {
	_, err := lua.CallCommand(cmds[0], implant.ImplantGetType(), strings.Join(cmds[1:], " "))
	if err != nil {
		log.PrintErr(err.Error())
	}
}
