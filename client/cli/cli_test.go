package cli

import (
	"encoding/json"
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

func TestCompleteTextSuggestsListenerHostingCommandsAndPaths(t *testing.T) {
	cli := &CLI{
		mode: modeListener, selectedListener: "http",
		snapshot: teamapi.Snapshot{Listeners: []teamapi.Listener{{
			Name: "http", Options: json.RawMessage(`{"hosted_files":{"/index.html":{"source_path":"site/index.html"}}}`),
		}}},
	}
	if _, ok := suggestionByText(cli.completeText("host "), "add"); !ok {
		t.Fatal("missing hosted-file add completion")
	}
	if _, ok := suggestionByText(cli.completeText("host remove "), "/index.html"); !ok {
		t.Fatal("missing hosted URL completion")
	}
}

func TestListenerHostedFileHelpers(t *testing.T) {
	entries, err := listenerHostedFileEntries(map[string]json.RawMessage{"hosted_files": json.RawMessage(`null`)})
	if err != nil {
		t.Fatal(err)
	}
	entries["/"] = json.RawMessage(`{"source_path":"index.html"}`)
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	headers, err := parseListenerHostedHeaders([]string{`{"Content-Type":`, `"text/html; charset=utf-8"}`})
	if err != nil {
		t.Fatal(err)
	}
	if headers["Content-Type"] != "text/html; charset=utf-8" {
		t.Fatalf("headers = %#v", headers)
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

func TestCompleteTextSuggestsBuildIDs(t *testing.T) {
	cli := &CLI{
		mode: modeProfile,
		snapshot: teamapi.Snapshot{Builds: []teamapi.Build{
			{ID: "build-1", Profile: "linux", Status: "completed"},
		}},
	}

	for _, input := range []string{"download ", "delete-build "} {
		suggestion, ok := suggestionByText(cli.completeText(input), "build-1")
		if !ok {
			t.Fatalf("missing build suggestion for %q", input)
		}
		if suggestion.Description != "linux completed" {
			t.Fatalf("build suggestion description = %q", suggestion.Description)
		}
	}
}

func TestSessionOutputIsRoutedOnlyToSelectedSession(t *testing.T) {
	cli := &CLI{mode: modeSession, selectedSession: "677222"}
	if !cli.shouldShowSessionOutput("677222") {
		t.Fatal("output for the selected session was hidden")
	}
	if cli.shouldShowSessionOutput("other") {
		t.Fatal("output for another session was shown")
	}
	cli.mode = modeMain
	if cli.shouldShowSessionOutput("677222") {
		t.Fatal("session output was shown outside the session view")
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
