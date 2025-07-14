package core

import (
	"fmt"
	"purpcmd/server/lua"
	"purpcmd/server/types"

	"github.com/cheynewallace/tabby"
)

func CmdHelp(p *types.Profile) {
	t := tabby.New()
	t.AddHeader("GENERIC COMMAND", "DESCRIPTION")
	t.AddLine("help", "Show help menu. Use `help <cmd>`.") //
	t.AddLine("exit", "Exit from purpcmd.") //

	switch p.STATE {
	case types.LISTENER:
		t.AddLine("new", "Create new listener. Use `new <name>`.") //
		t.AddLine("delete", "Delete listener.") //
		t.AddLine("options", "Show options.") //
		t.AddLine("set", "Set option. Use `set <key> <value>`.") //
		t.AddLine("run/start", "Start listener.") //
		t.AddLine("stop", "Stop a listener.") //
		t.AddLine("list", "List listeners.") //
		t.AddLine("interact", "Interact with a listener. Use `interact <name>`.") //
		t.AddLine("back", "Exit listener mode.") //
	case types.SESSION:
		t.AddLine("delete", "Delete session.") //
		t.AddLine("list", "List sessions.") //
		t.AddLine("interact", "Interact with a session. Use `interact <name>`.") //
		t.AddLine("back", "Exit session mode.") //
	case types.SCRIPT:
		t.AddLine("load", "Load script.") //
		t.AddLine("unload", "Unload script.") //
		t.AddLine("list", "List scripts.") //
		t.AddLine("back", "Exit script mode.") //
	default:
		t.AddLine("listener", "Enter listener mode. Use `help <cmd>`.")
		t.AddLine("session", "Enter session mode. Use `help <cmd>`.")		
	}

	print("\n")
	t.Print()
	print("\n")

	if p.STATE == types.SESSION {
		t1 := tabby.New()
		cmdlist := lua.LuaGetCommandDesc("a","a")
		t1.AddHeader("IMPL COMMAND", "DESCRIPTION")
		for _,j := range cmdlist {
			t1.AddLine(j[0], j[1])
		}
		t1.Print()
		print("\n")
	}
}

func usageHelp(cmds []string) {
	print("\n")
	println("HELP:")
	println("    `help` Show help menu.")
	println("    `help <cmd>` Show help menu for that command.")
	println("    `help <cmd> arg1 arg2` Arguments are accepted if implemented for that command.")
	print("\n")
}

func Banner() {
	var b string

	b = `
     ██▓███   █    ██  ██▀███   ██▓███   ▄████▄  
    ▓██░  ██▒ ██  ▓██▒▓██ ▒ ██▒▓██░  ██▒▒██▀ ▀█  
    ▓██░ ██▓▒▓██  ▒██░▓██ ░▄█ ▒▓██░ ██▓▒▒▓█    ▄ 
    ▒██▄█▓▒ ▒▓▓█  ░██░▒██▀▀█▄  ▒██▄█▓▒ ▒▒▓▓▄ ▄██▒
    ▒██▒ ░  ░▒▒█████▓ ░██▓ ▒██▒▒██▒ ░  ░▒ ▓███▀ ░
    ▒▓▒░ ░  ░░▒▓▒ ▒ ▒ ░ ▒▓ ░▒▓░▒▓▒░ ░  ░░ ░▒ ▒  ░
    ░▒ ░     ░░▒░ ░ ░   ░▒ ░ ▒░░▒ ░       ░  ▒   
    ░░        ░░░ ░ ░   ░░   ░ ░░       ░        
                ░        ░              ░ ░      
                                        ░               	

`
	fmt.Print(b)
}