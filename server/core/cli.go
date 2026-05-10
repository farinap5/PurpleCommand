package core

import (
	"fmt"
	"os"
	"os/exec"
	"purpcmd/server/implant"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/listener"
	"purpcmd/server/lua"
	"purpcmd/server/types"
	"strings"

	"github.com/c-bata/go-prompt"
)

func exitFunct(f *prompt.Buffer) {
	HandleExit()
	os.Exit(0)
}

func HandleExit() {
	/*
		it is necessary to deactivate the prompt in an
		appropriate way so as not to misconfigure the user's terminal.
		Reset tty executing stty
		disable raw mode
	*/
	rawoff := exec.Command("/bin/stty", "-raw", "echo")
	rawoff.Stdin = os.Stdin
	_ = rawoff.Run()
	rawoff.Wait()
}

var LivePrefixState struct {
	LivePrefix string
	IsEnable   bool
}

func (paux *ProfileAux) back(f *prompt.Buffer) {
	runBack([]string{}, &paux.Profile)
}
func (paux *ProfileAux) session(f *prompt.Buffer) {
	runSession([]string{}, &paux.Profile)
}
func (paux *ProfileAux) listen(f *prompt.Buffer) {
	runListener([]string{}, &paux.Profile)
}

func changeLivePrefix() (string, bool) {
	return LivePrefixState.LivePrefix, LivePrefixState.IsEnable
}

func CreateDefaultPrompt() string {
	return fmt.Sprintf("[PURPC L:%d S:%d]>> ", listener.ListenerCount(), implant.ImplantCount())
}

func InitCLI() {
	paux := new(ProfileAux)
	prom := prompt.New(
		paux.Execute,
		paux.completer,
		prompt.OptionPrefix(CreateDefaultPrompt()),
		prompt.OptionLivePrefix(changeLivePrefix),
		prompt.OptionCompletionOnDown(),
		prompt.OptionMaxSuggestion(3),

		prompt.OptionAddKeyBind(prompt.KeyBind{Key: prompt.ControlQ, Fn: exitFunct}),
		prompt.OptionAddKeyBind(prompt.KeyBind{Key: prompt.ControlD, Fn: exitFunct}),
		prompt.OptionAddKeyBind(prompt.KeyBind{Key: prompt.ControlB, Fn: paux.back}),
		prompt.OptionAddKeyBind(prompt.KeyBind{Key: prompt.ControlS, Fn: paux.session}),
		prompt.OptionAddKeyBind(prompt.KeyBind{Key: prompt.ControlO, Fn: paux.listen}),
	)
	prom.Run()
}

func (paux *ProfileAux) completer(d prompt.Document) []prompt.Suggest {
	inputs := strings.Split(d.TextBeforeCursor(), " ")
	//length := len(inputs)

	promptSuggestions := []prompt.Suggest{
		{Text: "help", Description: "Show help menu"},
		{Text: "exit", Description: "Exit from the prompt"},
	}

	if paux.Profile.STATE == types.LISTENER { // Options only valid when there is a selected script.
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest{Text: "set", Description: "Set listener options"},
			prompt.Suggest{Text: "run", Description: "Start Listener"},
			prompt.Suggest{Text: "stop", Description: "Stop Listener"},
			prompt.Suggest{Text: "back", Description: "Exit from listener menu"},
			prompt.Suggest{Text: "options", Description: "Show options"},
			prompt.Suggest{Text: "list", Description: "List listeners"},
			prompt.Suggest{Text: "new", Description: "new listener"},
			prompt.Suggest{Text: "interact", Description: "Interact with listener"},
			prompt.Suggest{Text: "delete", Description: "Delete listener"},
			prompt.Suggest{Text: "restart", Description: "Restart listener"},
		)

		// interact with dynamic session id
	} else if paux.Profile.STATE == types.SESSION {
		if inputs[0] == "interact" && len(inputs) > 1 {
			promptSuggestions = []prompt.Suggest{}
			implList := implant.ImplantListForSuggestions()
			for _, j := range implList {
				promptSuggestions = append(promptSuggestions,
					prompt.Suggest{Text: j[0], Description: j[1]},
				)
			}
			return prompt.FilterHasPrefix(promptSuggestions, inputs[1], true)
		}

		promptSuggestions = append(promptSuggestions,
			prompt.Suggest{Text: "back", Description: "Exit from session menu"},
			prompt.Suggest{Text: "list", Description: "List session"},
			prompt.Suggest{Text: "interact", Description: "Interact with session"},
			prompt.Suggest{Text: "delete", Description: "Delete session"},
		)

		cmdList := lua.LuaGetCommandDesc("a", "a")
		for _, j := range cmdList {
			promptSuggestions = append(promptSuggestions,
				prompt.Suggest{Text: j[0], Description: j[1]},
			)
		}

	} else if paux.Profile.STATE == types.SCRIPT {
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest{Text: "back", Description: "Exit from script menu"},
			prompt.Suggest{Text: "list", Description: "List script"},
			prompt.Suggest{Text: "load", Description: "Interact with script"},
			prompt.Suggest{Text: "unload", Description: "Unload and free script"},
		)
	} else if paux.Profile.STATE == types.LOOT {
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest{Text: "back", Description: "Exit from loot menu"},
			prompt.Suggest{Text: "list", Description: "List loot"},
			prompt.Suggest{Text: "view", Description: "View loot file content"},
			prompt.Suggest{Text: "export", Description: "Export loot to file"},
			prompt.Suggest{Text: "delete", Description: "Delete loot file"},
		)
	} else if paux.Profile.STATE == types.IMPLANT_BUILD {
		// Dynamic profile completions for select/generate/delete <name>
		if len(inputs) > 1 && (inputs[0] == "select" || inputs[0] == "generate" || inputs[0] == "delete") {
			promptSuggestions = []prompt.Suggest{}
			for _, p := range implantbuilder.ProfileNamesForSuggestions() {
				promptSuggestions = append(promptSuggestions,
					prompt.Suggest{Text: p[0], Description: p[1]},
				)
			}
			return prompt.FilterHasPrefix(promptSuggestions, inputs[1], true)
		}
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest{Text: "back", Description: "Exit implant menu"},
			prompt.Suggest{Text: "new", Description: "Create new implant profile: new profile <name>"},
			prompt.Suggest{Text: "list", Description: "List all profiles"},
			prompt.Suggest{Text: "select", Description: "Select a profile: select <name>"},
			prompt.Suggest{Text: "options", Description: "Show current profile options"},
			prompt.Suggest{Text: "set", Description: "Set option on current profile"},
			prompt.Suggest{Text: "generate", Description: "Build implant: generate [name]"},
			prompt.Suggest{Text: "delete", Description: "Delete a profile: delete <name>"},
		)
	} else { // Options only valid when there is no selected script.
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest{Text: "listener", Description: "Interact with listeners"},
			prompt.Suggest{Text: "session", Description: "Interact with session"},
			prompt.Suggest{Text: "script", Description: "Interact with script"},
			prompt.Suggest{Text: "loot", Description: "Interact with loot"},
			prompt.Suggest{Text: "implant", Description: "Build new implants"},
		)
	}

	return prompt.FilterHasPrefix(promptSuggestions, inputs[0], true)
}
