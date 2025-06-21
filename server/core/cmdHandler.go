package core

import (
	"purpcmd/server/types"
	"strings"
)

var commandMap = make(map[string]*types.Command)

func init() {
	loadFunctions()
	/*for k, v := range Mapping {
		HelpSugg = append(HelpSugg, prompt.Suggest{
			Text:        k,
			Description: v.Desc,
		})
	}*/
}

func (paux *ProfileAux) Execute(cmd string) {
	cmd = strings.TrimSpace(cmd)
	cmds := strings.Split(cmd, " ")
	length := len(cmds)

	if length == 0 {
		println("Too few arguments. Try `help cmd`.")
		return
	}

	cmdPtr := commandMap[cmds[0]]
	if cmdPtr != nil {
		functionP := *cmdPtr
		functionP.Call(cmds, &paux.Profile) // paux.p = types.Profile
	} else {
		if paux.Profile.Session  {
			runTaskCall(cmds)
		} else {
			println("Not a valid command. Type `help`.")
		}
	}
}

func loadFunctions() {
	commandMap["help"] = &types.Command{
		Call:   runHelp,
		Usage:  usageHelp,
		Desc:   "Show help menu.",
		Prompt: nil,
	}

	commandMap["exit"] = &types.Command{
		Call:   runExit,
		Usage:  nil,
		Desc:   "Properly exit the tool.",
		Prompt: nil,
	}

	commandMap["back"] = &types.Command{
		Call:   runBack,
		Usage:  nil,
		Desc:   "Exit from resource.",
		Prompt: nil,
	}

	commandMap["listener"] = &types.Command{
		Call:   runListener,
		Usage:  nil,
		Desc:   "Listener.",
		Prompt: nil,
	}
	commandMap["session"] = &types.Command{
		Call:   runSession,
		Usage:  nil,
		Desc:   "session.",
		Prompt: nil,
	}
	commandMap["script"] = &types.Command{
		Call:   runScript,
		Usage:  nil,
		Desc:   "script.",
		Prompt: nil,
	}

	commandMap["new"] = &types.Command{
		Call:   runNew,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["list"] = &types.Command{
		Call:   runList,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["options"] = &types.Command{
		Call:   runOptions,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["set"] = &types.Command{
		Call:   runSet,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["run"] = &types.Command{
		Call:   runRun,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["start"] = &types.Command{
		Call:   runRun,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["stop"] = &types.Command{
		Call:   runStop,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["restart"] = &types.Command{
		Call:   runRestart,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["interact"] = &types.Command{
		Call:   runInteract,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["delete"] = &types.Command{
		Call:   runDelete,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}
	commandMap["load"] = &types.Command{
		Call:   runLoad,
		Usage:  nil,
		Desc:   "",
		Prompt: nil,
	}

}
