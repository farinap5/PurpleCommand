package core

import (
	"os"
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
		CmdHelp()
	}
	return 0
}

func runExit(cmds []string,profile *types.Profile) int {
	// Exits the program
	HandleExit()
	os.Exit(0)
	return 0 // wont run
}



func runListener(cmds []string,profile *types.Profile) int {
	return 0
}
