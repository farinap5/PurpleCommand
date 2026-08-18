package core

import (
	"fmt"
	"purpcmd/server/implant"
	"purpcmd/server/lua"
	"purpcmd/server/types"

	"github.com/cheynewallace/tabby"
)

// HelpEntry is a command and description shown in a state-specific help menu.
type HelpEntry struct {
	Command     string
	Description string
}

// HelpEntries returns the shared help menu entries for a CLI state. Session
// payload commands remain dynamic and are rendered by the caller.
func HelpEntries(state int) []HelpEntry {
	entries := []HelpEntry{
		{Command: "help", Description: "Show help menu. Use `help <cmd>`."},
		{Command: "exit", Description: "Exit from purpcmd."},
	}

	switch state {
	case types.LISTENER:
		entries = append(entries,
			HelpEntry{Command: "new", Description: "Create new listener. Use `new <name>`."},
			HelpEntry{Command: "delete", Description: "Delete listener."},
			HelpEntry{Command: "options", Description: "Show options."},
			HelpEntry{Command: "set", Description: "Set option. Use `set <key> <value>`."},
			HelpEntry{Command: "run/start", Description: "Start listener."},
			HelpEntry{Command: "stop", Description: "Stop a listener."},
			HelpEntry{Command: "list", Description: "List listeners."},
			HelpEntry{Command: "interact", Description: "Interact with a listener. Use `interact <name>`."},
			HelpEntry{Command: "back", Description: "Exit listener mode."},
		)
	case types.SESSION:
		entries = append(entries,
			HelpEntry{Command: "delete", Description: "Delete a non-live session. Use `delete terminate` to terminate a live implant first."},
			HelpEntry{Command: "list", Description: "List sessions."},
			HelpEntry{Command: "interact", Description: "Interact with a session. Use `interact <name>`."},
			HelpEntry{Command: "back", Description: "Exit session mode."},
		)
	case types.SCRIPT:
		entries = append(entries,
			HelpEntry{Command: "load", Description: "Load script."},
			HelpEntry{Command: "unload", Description: "Unload script."},
			HelpEntry{Command: "list", Description: "List scripts."},
			HelpEntry{Command: "back", Description: "Exit script mode."},
		)
	case types.LOOT:
		entries = append(entries,
			HelpEntry{Command: "list", Description: "List all downloaded loot files."},
			HelpEntry{Command: "view", Description: "View loot file content. Use `view <uuid>`."},
			HelpEntry{Command: "export", Description: "Export loot to file. Use `export <uuid> <path>`."},
			HelpEntry{Command: "delete", Description: "Delete loot file. Use `delete <uuid>`."},
			HelpEntry{Command: "back", Description: "Exit loot mode."},
		)
	case types.IMPLANT_BUILD:
		entries = append(entries,
			HelpEntry{Command: "new", Description: "Create new profile. Use `new profile <name>`."},
			HelpEntry{Command: "list", Description: "List all implant profiles."},
			HelpEntry{Command: "select", Description: "Select a profile. Use `select <name>`."},
			HelpEntry{Command: "options", Description: "Show current profile options."},
			HelpEntry{Command: "set", Description: "Set option on current profile. Use `set <key> <value>`."},
			HelpEntry{Command: "generate", Description: "Build implant. Use `generate [name]` or `generate` for current."},
			HelpEntry{Command: "delete", Description: "Delete a profile. Use `delete <name>`."},
			HelpEntry{Command: "back", Description: "Exit implant builder mode."},
		)
	default:
		entries = append(entries,
			HelpEntry{Command: "listener", Description: "Enter listener mode. Use `help <cmd>`."},
			HelpEntry{Command: "session", Description: "Enter session mode. Use `help <cmd>`."},
			HelpEntry{Command: "script", Description: "Enter script mode."},
			HelpEntry{Command: "loot", Description: "Enter loot management mode."},
			HelpEntry{Command: "implant", Description: "Enter implant builder mode."},
		)
	}

	return entries
}

func CmdHelp(p *types.Profile) {
	t := tabby.New()
	t.AddHeader("GENERIC COMMAND", "DESCRIPTION")
	for _, entry := range HelpEntries(p.STATE) {
		t.AddLine(entry.Command, entry.Description)
	}

	print("\n")
	t.Print()
	print("\n")

	if p.STATE == types.SESSION {
		t1 := tabby.New()
		cmdlist := lua.LuaGetCommandDescriptions(implant.CurrentPayloadType())
		t1.AddHeader("AVAILABLE COMMAND", "DESCRIPTION")
		for _, j := range cmdlist {
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
