package core

import "github.com/cheynewallace/tabby"

func CmdHelp() {
	print("\n")
	t := tabby.New()
	t.AddHeader("GENERIC COMMAND", "DESCRIPTION")
	t.AddLine("help", "Show help menu. Use `help <cmd>`.") //
	t.AddLine("listener", "Show help menu. Use `help <cmd>`.") //
	t.AddLine("exit", "Show help menu. Use `help <cmd>`.") //
	t.Print()
	print("\n")
}

func usageHelp(cmds []string) {
	println("HELP:")
	println("    `help` Show help menu.")
	println("    `help <cmd>` Show help menu for that command.")
	println("    `help <cmd> arg1 arg2` Arguments are accepted if implemented for that command.")
	print("\n")
}
