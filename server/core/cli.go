package core

import (
	"fmt"
	"os"
	"os/exec"
	"purpcmd/server/implant"
	"purpcmd/server/listener"
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

func (paux *ProfileAux)back(f *prompt.Buffer) {
	runBack([]string{}, &paux.Profile)
}
func (paux *ProfileAux)session(f *prompt.Buffer) {
	runSession([]string{}, &paux.Profile)
}
func (paux *ProfileAux)listen(f *prompt.Buffer) {
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

		prompt.OptionAddKeyBind(prompt.KeyBind{prompt.ControlQ, exitFunct}),
		prompt.OptionAddKeyBind(prompt.KeyBind{prompt.ControlD, exitFunct}),
		prompt.OptionAddKeyBind(prompt.KeyBind{prompt.ControlB, paux.back}),
		prompt.OptionAddKeyBind(prompt.KeyBind{prompt.ControlS, paux.session}),
		prompt.OptionAddKeyBind(prompt.KeyBind{prompt.ControlO, paux.listen}),
	)
	prom.Run()
}

func (paux *ProfileAux) completer(d prompt.Document) []prompt.Suggest {
	inputs := strings.Split(d.TextBeforeCursor(), " ")
	//length := len(inputs)

	promptSuggestions := []prompt.Suggest {
		{Text: "help",    	Description: "Show help menu"},
		{Text: "exit", 		Description: "Exit from the prompt"},
	}

	if paux.Profile.Listener { // Options only valid when there is a selected script.
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest {Text: "set",     Description: "Set listener options"},
			prompt.Suggest {Text: "run",     Description: "Start Listener"},
			prompt.Suggest {Text: "stop",     Description: "Stop Listener"},
			prompt.Suggest {Text: "back",     Description: "Exit from listener menu"},
			prompt.Suggest {Text: "options",  Description: "Show options"},
			prompt.Suggest {Text: "list",     Description: "List listeners"},
			prompt.Suggest {Text: "new",      Description: "new listener"},
			prompt.Suggest {Text: "interact", Description: "Interact with listener"},
			prompt.Suggest {Text: "delete",   Description: "Delete listener"},
		)
	} else if paux.Profile.Session {
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest {Text: "back",     Description: "Exit from session menu"},
			prompt.Suggest {Text: "list",     Description: "List session"},
			prompt.Suggest {Text: "interact", Description: "Interact with session"},
			prompt.Suggest {Text: "delete",   Description: "Delete session"},
		)
	} else {	// Options only valid when there is no selected script.
		promptSuggestions = append(promptSuggestions,
			prompt.Suggest {Text: "listener", Description: "Interact with listeners"},
			prompt.Suggest {Text: "session", Description: "Interact with session"},
		) 
	}

	return prompt.FilterHasPrefix(promptSuggestions, inputs[0], true)
}