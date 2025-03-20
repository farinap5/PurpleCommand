package core

import (
	"purpcmd/server/types"
	"strings"
)

var Mapping = make(map[string]*types.Command)

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

	cmdPtr := Mapping[cmds[0]]
	if cmdPtr != nil {
		functionP := *cmdPtr
		functionP.Call(cmds, &paux.Profile) // paux.p = types.Profile
	} else {
		println("Not a valid command. Type `help`.")
	}
}

func loadFunctions() {
	Mapping["help"] = &types.Command{
		Call:   runHelp,
		Usage:  usageHelp,
		Desc:   "Show help menu.",
		Prompt: nil,
	}

	Mapping["exit"] = &types.Command{
		Call:   runExit,
		Usage:  nil,
		Desc:   "Properly exit the tool.",
		Prompt: nil,
	}

	Mapping["listener"] = &types.Command{
		Call:   runListener,
		Usage:  nil,
		Desc:   "Listener.",
		Prompt: nil,
	}
}