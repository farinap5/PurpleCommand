package core

import (
	"testing"

	"purpcmd/server/types"

	"github.com/c-bata/go-prompt"
)

func TestPromptSuggestionsExposeStateNavigation(t *testing.T) {
	tests := []struct {
		state int
		want  string
	}{
		{state: types.NIL, want: "listener"},
		{state: types.LISTENER, want: "restart"},
		{state: types.SESSION, want: "interact"},
		{state: types.SCRIPT, want: "load"},
		{state: types.LOOT, want: "export"},
		{state: types.IMPLANT_BUILD, want: "generate"},
	}

	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			suggestion, ok := findSuggestion(PromptSuggestions(test.state), test.want)
			if !ok {
				t.Fatalf("missing %q suggestion", test.want)
			}
			if suggestion.Description == "" {
				t.Fatalf("suggestion %q has no description", test.want)
			}
		})
	}
}

func TestHelpEntriesExposeStateNavigation(t *testing.T) {
	for _, entry := range HelpEntries(types.LOOT) {
		if entry.Command == "view" {
			if entry.Description == "" {
				t.Fatal("view help entry has no description")
			}
			return
		}
	}
	t.Fatal("missing view help entry")
}

func findSuggestion(suggestions []prompt.Suggest, text string) (prompt.Suggest, bool) {
	for _, suggestion := range suggestions {
		if suggestion.Text == text {
			return suggestion, true
		}
	}
	return prompt.Suggest{}, false
}
