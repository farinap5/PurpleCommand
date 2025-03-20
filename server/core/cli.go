package core

import (
	"os"
	"os/exec"
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

func InitCLI() {
	paux := new(ProfileAux)
	prom := prompt.New(
		paux.Execute,
		paux.completer,
		prompt.OptionPrefix("[prc]>> "),
		//prompt.OptionLivePrefix(changeLivePrefix),
		prompt.OptionCompletionOnDown(),
		prompt.OptionMaxSuggestion(3),
		prompt.OptionAddKeyBind(prompt.KeyBind{prompt.ControlQ, exitFunct}),
	)
	prom.Run()
}

func (paux *ProfileAux) completer(d prompt.Document) []prompt.Suggest {
	inputs := strings.Split(d.TextBeforeCursor(), " ")
	//length := len(inputs)

	promptSuggestions := []prompt.Suggest {
		{Text: "help",    	Description: "Show help menu"},
		{Text: "session",   Description: "Manage sessions"},
		{Text: "listener",  Description: "Manage listeners"},
		{Text: "exit", 		Description: "Exit from the prompt"},
	}

	return prompt.FilterHasPrefix(promptSuggestions, inputs[0], true)
}