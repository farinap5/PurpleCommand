package cli

import (
	"testing"

	"purpcmd/pkg/teamapi"

	"github.com/c-bata/go-prompt"
)

func TestCompleteTextUsesSharedPromptDescriptions(t *testing.T) {
	cli := &CLI{mode: modeMain}

	suggestions := cli.completeText("li")
	suggestion, ok := suggestionByText(suggestions, "listener")
	if !ok {
		t.Fatal("missing listener suggestion")
	}
	if suggestion.Description == "" {
		t.Fatal("shared listener suggestion has no description")
	}
}

func TestCompleteTextSuggestsUserMessage(t *testing.T) {
	cli := &CLI{mode: modeMain}

	suggestion, ok := suggestionByText(cli.completeText("mes"), "message")
	if !ok {
		t.Fatal("missing message suggestion")
	}
	if suggestion.Description == "" {
		t.Fatal("message suggestion has no description")
	}
}

func TestCompleteTextSuggestsRemoteResourcesAfterSpace(t *testing.T) {
	cli := &CLI{
		mode: modeListener,
		snapshot: teamapi.Snapshot{Listeners: []teamapi.Listener{
			{Name: "http", Host: "127.0.0.1", Port: "8080"},
		}},
	}

	suggestions := cli.completeText("interact ")
	suggestion, ok := suggestionByText(suggestions, "http")
	if !ok {
		t.Fatal("missing listener name suggestion")
	}
	if suggestion.Description != "127.0.0.1:8080" {
		t.Fatalf("unexpected listener description %q", suggestion.Description)
	}
}

func TestCompleteTextAddsCommandsForSelectedRemotePayload(t *testing.T) {
	cli := &CLI{
		mode:            modeSession,
		selectedSession: "agent-1",
		snapshot: teamapi.Snapshot{
			Sessions: []teamapi.Session{{Name: "agent-1", PayloadType: "linux"}},
			Commands: []teamapi.Command{
				{PayloadType: "linux", Name: "whoami", Description: "Show current user"},
				{PayloadType: "windows", Name: "powershell", Description: "Run PowerShell"},
			},
		},
	}

	if _, ok := suggestionByText(cli.completeText("who"), "whoami"); !ok {
		t.Fatal("missing command for selected payload")
	}
	if _, ok := suggestionByText(cli.completeText("pow"), "powershell"); ok {
		t.Fatal("suggested command for a different payload")
	}
}

func suggestionByText(suggestions []prompt.Suggest, text string) (prompt.Suggest, bool) {
	for _, suggestion := range suggestions {
		if suggestion.Text == text {
			return suggestion, true
		}
	}
	return prompt.Suggest{}, false
}
