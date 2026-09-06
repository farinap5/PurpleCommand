package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	clientapi "purpcmd/client/api"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/core"
	"purpcmd/server/log"
	serverssh "purpcmd/server/ssh"
	"purpcmd/server/types"

	"github.com/c-bata/go-prompt"
	"github.com/cheynewallace/tabby"
)

type mode string

const (
	modeMain     mode = "main"
	modeListener mode = "listener"
	modeSession  mode = "session"
	modeScript   mode = "script"
	modeLoot     mode = "loot"
	modeProfile  mode = "implant"
)

type CLI struct {
	client           *clientapi.Client
	mu               sync.RWMutex
	mode             mode
	selectedListener string
	selectedSession  string
	selectedProfile  string
	snapshot         teamapi.Snapshot
}

func New(client *clientapi.Client) *CLI {
	return &CLI{client: client, mode: modeMain}
}

func (cli *CLI) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hello, err := cli.client.Hello(ctx)
	if err != nil {
		return err
	}
	if hello.Protocol != teamapi.Version {
		return fmt.Errorf("protocol mismatch: server=%d client=%d", hello.Protocol, teamapi.Version)
	}
	if err := cli.refresh(); err != nil {
		return err
	}

	instance := prompt.New(
		cli.execute,
		cli.complete,
		prompt.OptionPrefix(""),
		prompt.OptionLivePrefix(cli.livePrefix),
		prompt.OptionCompletionOnDown(),
		prompt.OptionMaxSuggestion(8),
		prompt.OptionAddKeyBind(prompt.KeyBind{Key: prompt.ControlD, Fn: func(*prompt.Buffer) { cli.shutdown() }}),
		prompt.OptionAddKeyBind(prompt.KeyBind{Key: prompt.ControlQ, Fn: func(*prompt.Buffer) { cli.shutdown() }}),
	)
	go cli.eventLoop()
	instance.Run()
	return nil
}

func (cli *CLI) shutdown() {
	core.HandleExit()
	_ = cli.client.Close()
	os.Exit(0)
}

func (cli *CLI) eventLoop() {
	for event := range cli.client.Events() {
		/*message := event.Type
		if len(event.Data) > 0 {
			var values map[string]any
			if json.Unmarshal(event.Data, &values) == nil {
				if name, ok := values["name"].(string); ok {
					message += " " + name
				}
			}
		}
		log.AsyncWriteStdoutInfo(message + "\n")*/
		// handle and show information
		eventHandler(cli, event)
		_ = cli.refresh()
	}
}

func (cli *CLI) refresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snapshot, err := cli.client.Snapshot(ctx)
	if err != nil {
		return err
	}
	cli.mu.Lock()
	cli.snapshot = snapshot
	cli.mu.Unlock()
	return nil
}

func (cli *CLI) livePrefix() (string, bool) {
	cli.mu.RLock()
	defer cli.mu.RUnlock()
	switch cli.mode {
	case modeListener:
		return "(listener - " + selected(cli.selectedListener) + ")>> ", true
	case modeSession:
		return "(session - " + selected(cli.selectedSession) + ")>> ", true
	case modeScript:
		return "(script)>> ", true
	case modeLoot:
		return "(loot)>> ", true
	case modeProfile:
		return "(implant - " + selected(cli.selectedProfile) + ")>> ", true
	default:
		return fmt.Sprintf("[PURPC L:%d S:%d]>> ", len(cli.snapshot.Listeners), len(cli.snapshot.Sessions)), true
	}
}

func selected(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func (cli *CLI) execute(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}
	fields := strings.Fields(input)
	command := strings.ToLower(fields[0])
	if command == "exit" || command == "quit" {
		cli.shutdown()
	}
	if command == "back" {
		cli.mu.Lock()
		cli.mode = modeMain
		cli.mu.Unlock()
		return
	}
	if command == "help" {
		cli.help()
		return
	}
	if command == "message" {
		message := strings.TrimSpace(strings.TrimPrefix(input, fields[0]))
		if err := cli.sendUserMessage(message); err != nil {
			log.PrintErr(err)
		}
		return
	}

	cli.mu.RLock()
	currentMode := cli.mode
	cli.mu.RUnlock()
	var err error
	switch currentMode {
	case modeMain:
		err = cli.executeMain(fields)
	case modeListener:
		err = cli.executeListener(fields)
	case modeSession:
		err = cli.executeSession(input, fields)
	case modeScript:
		err = cli.executeScript(fields)
	case modeLoot:
		err = cli.executeLoot(fields)
	case modeProfile:
		err = cli.executeProfile(fields)
	}
	if err != nil {
		log.PrintErr(err)
	}
	_ = cli.refresh()
}

func (cli *CLI) executeMain(fields []string) error {
	cli.mu.Lock()
	defer cli.mu.Unlock()
	switch strings.ToLower(fields[0]) {
	case "listener":
		cli.mode = modeListener
	case "session":
		cli.mode = modeSession
	case "script":
		cli.mode = modeScript
	case "loot":
		cli.mode = modeLoot
	case "implant", "profile":
		cli.mode = modeProfile
	default:
		return errorsNew("Unknow command: type `help`")
	}
	return nil
}

func (cli *CLI) executeListener(fields []string) error {
	command := strings.ToLower(fields[0])
	switch command {
	case "list":
		var items []teamapi.Listener
		if err := cli.request(teamapi.AskListenerList, struct{}{}, &items); err != nil {
			return err
		}
		printListeners(items)
	case "new":
		if len(fields) < 2 || len(fields) > 4 {
			return errorsNew("usage: new name [host] [port]")
		}
		request := teamapi.ListenerCreateRequest{Name: fields[1]}
		if len(fields) > 2 {
			request.Host = fields[2]
		}
		if len(fields) > 3 {
			request.Port = fields[3]
		}
		var item teamapi.Listener
		if err := cli.request(teamapi.AskListenerCreate, request, &item); err != nil {
			return err
		}
		cli.mu.Lock()
		cli.selectedListener = item.Name
		cli.mu.Unlock()
		printListeners([]teamapi.Listener{item})
	case "interact", "select":
		if len(fields) != 2 {
			return errorsNew("usage: interact name")
		}
		var item teamapi.Listener
		if err := cli.request(teamapi.AskListenerGet, teamapi.NameRequest{Name: fields[1]}, &item); err != nil {
			return err
		}
		cli.mu.Lock()
		cli.selectedListener = item.Name
		cli.mu.Unlock()
	case "options":
		name, err := cli.listenerName(fields[1:])
		if err != nil {
			return err
		}
		var item teamapi.Listener
		if err := cli.request(teamapi.AskListenerGet, teamapi.NameRequest{Name: name}, &item); err != nil {
			return err
		}
		printListeners([]teamapi.Listener{item})
	case "set":
		if len(fields) != 3 {
			return errorsNew("usage: set option value")
		}
		name, err := cli.listenerName(nil)
		if err != nil {
			return err
		}
		var item teamapi.Listener
		return cli.request(teamapi.AskListenerUpdate, teamapi.ListenerUpdateRequest{Name: name, Key: fields[1], Value: fields[2]}, &item)
	case "host":
		return cli.executeListenerHost(fields)
	case "start", "run", "stop", "restart", "delete":
		name, err := cli.listenerName(fields[1:])
		if err != nil {
			return err
		}
		operation := map[string]string{"start": teamapi.AskListenerStart, "run": teamapi.AskListenerStart, "stop": teamapi.AskListenerStop, "restart": teamapi.AskListenerRestart, "delete": teamapi.AskListenerDelete}[command]
		var result any
		if err := cli.request(operation, teamapi.NameRequest{Name: name}, &result); err != nil {
			return err
		}
		if command == "delete" {
			cli.mu.Lock()
			if cli.selectedListener == name {
				cli.selectedListener = ""
			}
			cli.mu.Unlock()
		}
	default:
		return errorsNew("listener commands: list, new, interact, options, set, host, start, stop, restart, delete, back")
	}
	return nil
}

func (cli *CLI) executeListenerHost(fields []string) error {
	if len(fields) < 2 {
		return errorsNew("usage: host list|add|remove|not-found|clear-not-found")
	}
	name, err := cli.listenerName(nil)
	if err != nil {
		return err
	}
	switch strings.ToLower(fields[1]) {
	case "list":
		if len(fields) != 2 {
			return errorsNew("usage: host list")
		}
		var hosted teamapi.ListenerHostedConfiguration
		if err := cli.request(teamapi.AskListenerHosted, teamapi.NameRequest{Name: name}, &hosted); err != nil {
			return err
		}
		return printListenerHostedFiles(hosted)
	case "add":
		if len(fields) < 4 {
			return errorsNew("usage: host add url-path source-path [status] [headers-json]")
		}
		status := 0
		headerIndex := 5
		if len(fields) >= 5 {
			if strings.HasPrefix(fields[4], "{") {
				headerIndex = 4
			} else {
				status, err = strconv.Atoi(fields[4])
				if err != nil {
					return errorsNew("hosted-file status must be a number")
				}
			}
		}
		var headerArguments []string
		if len(fields) > headerIndex {
			headerArguments = fields[headerIndex:]
		}
		headers, err := parseListenerHostedHeaders(headerArguments)
		if err != nil {
			return err
		}
		var hosted teamapi.ListenerHostedConfiguration
		return cli.request(teamapi.AskListenerHostedAdd, teamapi.ListenerHostedAddRequest{
			Name: name, URLPath: fields[2],
			File: teamapi.HTTPHostedFile{SourcePath: fields[3], Status: status, Headers: headers},
		}, &hosted)
	case "remove":
		if len(fields) != 3 {
			return errorsNew("usage: host remove url-path")
		}
		var hosted teamapi.ListenerHostedConfiguration
		return cli.request(teamapi.AskListenerHostedRemove, teamapi.ListenerHostedRemoveRequest{
			Name: name, URLPath: fields[2],
		}, &hosted)
	case "not-found":
		if len(fields) < 3 {
			return errorsNew("usage: host not-found source-path [headers-json]")
		}
		headers, err := parseListenerHostedHeaders(fields[3:])
		if err != nil {
			return err
		}
		var hosted teamapi.ListenerHostedConfiguration
		return cli.request(teamapi.AskListenerHostedNotFoundSet, teamapi.ListenerHostedNotFoundSetRequest{
			Name: name, File: teamapi.HTTPHostedFile{SourcePath: fields[2], Headers: headers},
		}, &hosted)
	case "clear-not-found":
		if len(fields) != 2 {
			return errorsNew("usage: host clear-not-found")
		}
		var hosted teamapi.ListenerHostedConfiguration
		return cli.request(teamapi.AskListenerHostedNotFoundClear, teamapi.ListenerHostedNotFoundClearRequest{
			Name: name,
		}, &hosted)
	default:
		return errorsNew("usage: host list|add|remove|not-found|clear-not-found")
	}
}

func listenerHostedFileEntries(options map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage)
	if raw := options["hosted_files"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("decode hosted_files: %w", err)
		}
		if result == nil {
			result = make(map[string]json.RawMessage)
		}
	}
	return result, nil
}

func parseListenerHostedHeaders(arguments []string) (map[string]string, error) {
	if len(arguments) == 0 {
		return nil, nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(strings.Join(arguments, " ")), &headers); err != nil {
		return nil, fmt.Errorf("decode hosted-file headers: %w", err)
	}
	return headers, nil
}

func printListenerHostedFiles(hosted teamapi.ListenerHostedConfiguration) error {
	table := tabby.New()
	table.AddHeader("URL", "SOURCE", "STATUS", "HEADERS")
	paths := make([]string, 0, len(hosted.HostedFiles))
	for urlPath := range hosted.HostedFiles {
		paths = append(paths, urlPath)
	}
	sort.Strings(paths)
	for _, urlPath := range paths {
		file := hosted.HostedFiles[urlPath]
		status := file.Status
		if status == 0 {
			status = http.StatusOK
		}
		headers, _ := json.Marshal(file.Headers)
		table.AddLine(urlPath, file.SourcePath, status, string(headers))
	}
	if hosted.NotFoundPage != nil {
		headers, _ := json.Marshal(hosted.NotFoundPage.Headers)
		table.AddLine("<default 404>", hosted.NotFoundPage.SourcePath, http.StatusNotFound, string(headers))
	}
	table.Print()
	return nil
}

func (cli *CLI) listenerName(arguments []string) (string, error) {
	if len(arguments) > 0 {
		return arguments[0], nil
	}
	cli.mu.RLock()
	defer cli.mu.RUnlock()
	if cli.selectedListener == "" {
		return "", errorsNew("select a listener first")
	}
	return cli.selectedListener, nil
}

func (cli *CLI) executeSession(input string, fields []string) error {
	command := strings.ToLower(fields[0])
	switch command {
	case "list":
		var items []teamapi.Session
		if err := cli.request(teamapi.AskSessionList, struct{}{}, &items); err != nil {
			return err
		}
		printSessions(items)
	case "interact", "select":
		if len(fields) != 2 {
			return errorsNew("usage: interact name")
		}
		var item teamapi.Session
		if err := cli.request(teamapi.AskSessionGet, teamapi.NameRequest{Name: fields[1]}, &item); err != nil {
			return err
		}
		cli.mu.Lock()
		cli.selectedSession = item.Name
		cli.mu.Unlock()
	case "tasks":
		session, err := cli.sessionName()
		if err != nil {
			return err
		}
		var tasks []teamapi.Task
		if err := cli.request(teamapi.AskTaskList, teamapi.TaskListRequest{Session: session}, &tasks); err != nil {
			return err
		}
		printTasks(tasks)
	case "delete":
		session, err := cli.sessionName()
		if err != nil {
			return err
		}
		operation := teamapi.AskSessionDelete
		if len(fields) == 2 && (fields[1] == "terminate" || fields[1] == "--terminate") {
			operation = teamapi.AskSessionTerminate
		}
		var result any
		if err := cli.request(operation, teamapi.NameRequest{Name: session}, &result); err != nil {
			return err
		}
		if operation == teamapi.AskSessionDelete {
			cli.mu.Lock()
			cli.selectedSession = ""
			cli.mu.Unlock()
		}
	case "interactive", "ssh":
		return cli.openInteractive()
	default:
		session, err := cli.sessionName()
		if err != nil {
			return err
		}
		arguments := strings.TrimSpace(strings.TrimPrefix(input, fields[0]))
		attachments, err := cli.readAttachments(fields[1:])
		if err != nil {
			return err
		}
		var reply teamapi.CommandExecuteReply
		if err := cli.request(teamapi.AskCommandExecute, teamapi.CommandExecuteRequest{
			Session: session, Name: fields[0], Arguments: arguments, Attachments: attachments,
		}, &reply); err != nil {
			return err
		}
		if reply.Message != "" {
			fmt.Println(reply.Message)
		}
		if len(reply.TaskIDs) > 0 {
			fmt.Println("Queued task:", strings.Join(reply.TaskIDs, ", "))
		}
	}
	return nil
}

func (cli *CLI) sessionName() (string, error) {
	cli.mu.RLock()
	defer cli.mu.RUnlock()
	if cli.selectedSession == "" {
		return "", errorsNew("select a session first")
	}
	return cli.selectedSession, nil
}

func (cli *CLI) openInteractive() error {
	session, err := cli.sessionName()
	if err != nil {
		return err
	}
	var reply teamapi.InteractiveOpenReply
	if err := cli.request(teamapi.AskInteractiveOpen, teamapi.InteractiveOpenRequest{Session: session}, &reply); err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(context.Background(), reply.Expires)
	defer cancel()
	connection, err := cli.client.Interactive(ctx, reply.URL)
	if err != nil {
		return err
	}
	defer connection.Close()
	return serverssh.Tunnel(connection)
}

func (cli *CLI) executeScript(fields []string) error {
	switch strings.ToLower(fields[0]) {
	case "list":
		var items []teamapi.Script
		if err := cli.request(teamapi.AskScriptList, struct{}{}, &items); err != nil {
			return err
		}
		table := tabby.New()
		table.AddHeader("PATH", "LOADED", "SHA256")
		for _, item := range items {
			table.AddLine(item.Path, item.Loaded, shorten(item.SHA256, 12))
		}
		table.Print()
	case "load":
		if len(fields) != 2 {
			return errorsNew("usage: load local-script.lua")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		serverPath, err := cli.client.UploadScript(ctx, fields[1])
		if err != nil {
			return err
		}
		var item teamapi.Script
		return cli.request(teamapi.AskScriptLoad, teamapi.ScriptLoadRequest{Path: serverPath}, &item)
	case "unload":
		if len(fields) != 2 {
			return errorsNew("usage: unload server-path")
		}
		var result any
		return cli.request(teamapi.AskScriptUnload, teamapi.NameRequest{Name: fields[1]}, &result)
	default:
		return errorsNew("script commands: list, load, unload, back")
	}
	return nil
}

func (cli *CLI) executeLoot(fields []string) error {
	switch strings.ToLower(fields[0]) {
	case "list":
		var items []teamapi.Loot
		if err := cli.request(teamapi.AskLootList, struct{}{}, &items); err != nil {
			return err
		}
		table := tabby.New()
		table.AddHeader("UUID", "SESSION", "FILE", "SIZE", "SHA256")
		for _, item := range items {
			table.AddLine(item.UUID, item.Session, item.FileName, item.Size, shorten(item.SHA256, 12))
		}
		table.Print()
	case "export":
		if len(fields) != 3 {
			return errorsNew("usage: export uuid destination")
		}
		var reply teamapi.LootGetReply
		if err := cli.request(teamapi.AskLootGet, teamapi.LootRequest{UUID: fields[1]}, &reply); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return cli.client.Download(ctx, reply.DownloadURL, fields[2])
	case "view":
		if len(fields) != 2 {
			return errorsNew("usage: view uuid")
		}
		var reply teamapi.LootGetReply
		if err := cli.request(teamapi.AskLootGet, teamapi.LootRequest{UUID: fields[1]}, &reply); err != nil {
			return err
		}
		file, err := os.CreateTemp("", "purpcmd-loot-view-")
		if err != nil {
			return err
		}
		name := file.Name()
		_ = file.Close()
		defer os.Remove(name)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := cli.client.Download(ctx, reply.DownloadURL, name); err != nil {
			return err
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		fmt.Println(string(content))
	case "delete":
		if len(fields) != 2 {
			return errorsNew("usage: delete uuid")
		}
		var item teamapi.Loot
		return cli.request(teamapi.AskLootDelete, teamapi.LootRequest{UUID: fields[1]}, &item)
	default:
		return errorsNew("loot commands: list, view, export, delete, back")
	}
	return nil
}

func (cli *CLI) executeProfile(fields []string) error {
	command := strings.ToLower(fields[0])
	switch command {
	case "list":
		var items []teamapi.Profile
		if err := cli.request(teamapi.AskProfileList, struct{}{}, &items); err != nil {
			return err
		}
		printProfiles(items)
	case "new":
		name := ""
		if len(fields) == 3 && fields[1] == "profile" {
			name = fields[2]
		}
		if len(fields) == 2 {
			name = fields[1]
		}
		if name == "" {
			return errorsNew("usage: new profile name")
		}
		var item teamapi.Profile
		if err := cli.request(teamapi.AskProfileCreate, teamapi.Profile{Name: name}, &item); err != nil {
			return err
		}
		cli.mu.Lock()
		cli.selectedProfile = item.Name
		cli.mu.Unlock()
	case "select", "interact":
		if len(fields) != 2 {
			return errorsNew("usage: select name")
		}
		var item teamapi.Profile
		if err := cli.request(teamapi.AskProfileGet, teamapi.NameRequest{Name: fields[1]}, &item); err != nil {
			return err
		}
		cli.mu.Lock()
		cli.selectedProfile = item.Name
		cli.mu.Unlock()
	case "options":
		name, err := cli.profileName(fields[1:])
		if err != nil {
			return err
		}
		var item teamapi.Profile
		if err := cli.request(teamapi.AskProfileGet, teamapi.NameRequest{Name: name}, &item); err != nil {
			return err
		}
		printProfiles([]teamapi.Profile{item})
	case "set":
		if len(fields) != 3 {
			return errorsNew("usage: set option value")
		}
		name, err := cli.profileName(nil)
		if err != nil {
			return err
		}
		var item teamapi.Profile
		return cli.request(teamapi.AskProfileUpdate, teamapi.ProfileUpdateRequest{Name: name, Key: fields[1], Value: fields[2]}, &item)
	case "generate":
		if len(fields) > 3 {
			return errorsNew("usage: generate [profile] [builder]")
		}
		name, err := cli.profileName(fields[1:])
		if err != nil {
			return err
		}
		builder := ""
		if len(fields) == 3 {
			builder = fields[2]
		}
		var build teamapi.Build
		if err := cli.request(teamapi.AskBuildCreate, teamapi.BuildRequest{Profile: name, Builder: builder}, &build); err != nil {
			return err
		}
		fmt.Println("build queued:", build.ID)
	case "builds":
		var items []teamapi.Build
		if err := cli.request(teamapi.AskBuildList, struct{}{}, &items); err != nil {
			return err
		}
		table := tabby.New()
		table.AddHeader("ID", "PROFILE", "BUILDER", "STATUS", "ARTIFACT", "CREATED", "COMPLETED", "ERROR")
		for _, item := range items {
			completed := ""
			if !item.CompletedAt.IsZero() {
				completed = item.CompletedAt.Format(time.RFC3339)
			}
			table.AddLine(item.ID, item.Profile, item.Builder, item.Status, item.ArtifactName,
				item.CreatedAt.Format(time.RFC3339), completed, item.Error)
		}
		table.Print()
	case "builders":
		var items []teamapi.PayloadBuilder
		if err := cli.request(teamapi.AskPayloadBuilderList, struct{}{}, &items); err != nil {
			return err
		}
		table := tabby.New()
		table.AddHeader("NAME", "DESCRIPTION", "SOURCE")
		for _, item := range items {
			table.AddLine(item.Name, item.Description, item.Source)
		}
		table.Print()
	case "download":
		if len(fields) != 3 {
			return errorsNew("usage: download build-id destination")
		}
		var build teamapi.Build
		if err := cli.request(teamapi.AskBuildGet, teamapi.NameRequest{Name: fields[1]}, &build); err != nil {
			return err
		}
		if build.DownloadURL == "" {
			return errorsNew("build is not complete")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		return cli.client.Download(ctx, build.DownloadURL, fields[2])
	case "delete-build":
		if len(fields) != 2 {
			return errorsNew("usage: delete-build build-id")
		}
		var build teamapi.Build
		return cli.request(teamapi.AskBuildDelete, teamapi.BuildDeleteRequest{ID: fields[1]}, &build)
	case "delete":
		name, err := cli.profileName(fields[1:])
		if err != nil {
			return err
		}
		var result any
		if err := cli.request(teamapi.AskProfileDelete, teamapi.NameRequest{Name: name}, &result); err != nil {
			return err
		}
		cli.mu.Lock()
		if cli.selectedProfile == name {
			cli.selectedProfile = ""
		}
		cli.mu.Unlock()
	default:
		return errorsNew("implant commands: list, new, select, options, set, generate, builders, builds, download, delete-build, delete, back")
	}
	return nil
}

func (cli *CLI) profileName(arguments []string) (string, error) {
	if len(arguments) > 0 {
		return arguments[0], nil
	}
	cli.mu.RLock()
	defer cli.mu.RUnlock()
	if cli.selectedProfile == "" {
		return "", errorsNew("select a profile first")
	}
	return cli.selectedProfile, nil
}

func (cli *CLI) request(operation string, request, response any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return cli.client.Request(ctx, operation, request, response)
}

func (cli *CLI) sendUserMessage(message string) error {
	if message == "" {
		return errorsNew("usage: message text")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := cli.client.SendUserMessage(ctx, message)
	return err
}

func (cli *CLI) complete(document prompt.Document) []prompt.Suggest {
	return cli.completeText(document.TextBeforeCursor())
}

func (cli *CLI) completeText(input string) []prompt.Suggest {
	words := strings.Fields(input)
	trailingSpace := len(strings.TrimRight(input, " \t\r\n")) != len(input)
	prefix := ""
	if len(words) > 0 && !trailingSpace {
		prefix = words[len(words)-1]
	}

	cli.mu.RLock()
	defer cli.mu.RUnlock()

	state := stateForMode(cli.mode)
	suggestions := core.PromptSuggestions(state)
	suggestions = append(suggestions, prompt.Suggest{Text: "message", Description: "Broadcast a message to connected users"})
	argumentIndex := len(words) - 1
	if trailingSpace {
		argumentIndex++
	}
	argumentPosition := argumentIndex > 0
	command := ""
	if len(words) > 0 {
		command = strings.ToLower(words[0])
	}

	switch cli.mode {
	case modeListener:
		if command == "host" && argumentIndex == 1 {
			return prompt.FilterHasPrefix([]prompt.Suggest{
				{Text: "list", Description: "List hosted files and the default 404 page"},
				{Text: "add", Description: "Add or replace a hosted URL"},
				{Text: "remove", Description: "Remove a hosted URL"},
				{Text: "not-found", Description: "Configure the default 404 page"},
				{Text: "clear-not-found", Description: "Remove the default 404 page"},
			}, prefix, true)
		}
		if command == "host" && argumentIndex == 2 && len(words) > 1 && strings.EqualFold(words[1], "remove") {
			suggestions = nil
			for _, item := range cli.snapshot.Listeners {
				if item.Name != cli.selectedListener {
					continue
				}
				var options map[string]json.RawMessage
				if json.Unmarshal(item.Options, &options) != nil {
					break
				}
				files, err := listenerHostedFileEntries(options)
				if err != nil {
					break
				}
				for urlPath := range files {
					suggestions = append(suggestions, prompt.Suggest{Text: urlPath, Description: "Hosted URL path"})
				}
				break
			}
			return prompt.FilterHasPrefix(suggestions, prefix, true)
		}
		if argumentIndex == 1 && isOneOf(command, "interact", "select", "options", "start", "run", "stop", "restart", "delete") {
			suggestions = nil
			for _, item := range cli.snapshot.Listeners {
				suggestions = append(suggestions, prompt.Suggest{Text: item.Name, Description: item.Host + ":" + item.Port})
			}
			return prompt.FilterHasPrefix(suggestions, prefix, true)
		}
		suggestions = append(suggestions,
			prompt.Suggest{Text: "host", Description: "Manage HTTP listener hosted files"},
			prompt.Suggest{Text: "start", Description: "Start listener"},
			prompt.Suggest{Text: "select", Description: "Select a listener"},
		)
	case modeSession:
		if argumentIndex == 1 && isOneOf(command, "interact", "select") {
			suggestions = nil
			for _, item := range cli.snapshot.Sessions {
				suggestions = append(suggestions, prompt.Suggest{Text: item.Name, Description: item.PayloadType + " " + item.Hostname + "@" + item.User})
			}
			return prompt.FilterHasPrefix(suggestions, prefix, true)
		}
		if argumentIndex == 1 && command == "delete" {
			return prompt.FilterHasPrefix([]prompt.Suggest{{Text: "terminate", Description: "Terminate a live implant before deleting its session"}}, prefix, true)
		}
		suggestions = append(suggestions,
			prompt.Suggest{Text: "select", Description: "Select a session"},
			prompt.Suggest{Text: "tasks", Description: "List tasks for the selected session"},
			prompt.Suggest{Text: "interactive", Description: "Open an interactive SSH session"},
			prompt.Suggest{Text: "ssh", Description: "Open an interactive SSH session"},
		)
		payloadType := cli.selectedPayloadType()
		for _, command := range cli.snapshot.Commands {
			if command.PayloadType == payloadType {
				suggestions = append(suggestions, prompt.Suggest{Text: command.Name, Description: command.Description})
			}
		}
	case modeScript:
		if argumentIndex == 1 && command == "unload" {
			suggestions = nil
			for _, item := range cli.snapshot.Scripts {
				suggestions = append(suggestions, prompt.Suggest{Text: item.Path, Description: shorten(item.SHA256, 12)})
			}
			return prompt.FilterHasPrefix(suggestions, prefix, true)
		}
	case modeProfile:
		if argumentIndex == 1 && isOneOf(command, "download", "delete-build") {
			suggestions = nil
			for _, item := range cli.snapshot.Builds {
				suggestions = append(suggestions, prompt.Suggest{Text: item.ID, Description: item.Profile + " " + item.Status})
			}
			return prompt.FilterHasPrefix(suggestions, prefix, true)
		}
		if argumentIndex == 1 && isOneOf(command, "select", "interact", "generate", "delete", "options") {
			suggestions = nil
			for _, item := range cli.snapshot.Profiles {
				suggestions = append(suggestions, prompt.Suggest{Text: item.Name, Description: item.Type + " " + item.OS + "/" + item.ARCH})
			}
			return prompt.FilterHasPrefix(suggestions, prefix, true)
		}
		if argumentIndex == 1 && command == "new" {
			return prompt.FilterHasPrefix([]prompt.Suggest{{Text: "profile", Description: "Create a named implant profile"}}, prefix, true)
		}
		suggestions = append(suggestions,
			prompt.Suggest{Text: "interact", Description: "Select a profile"},
			prompt.Suggest{Text: "builders", Description: "List registered payload builders"},
			prompt.Suggest{Text: "builds", Description: "List implant builds"},
			prompt.Suggest{Text: "download", Description: "Download a completed build"},
			prompt.Suggest{Text: "delete-build", Description: "Delete a completed or failed build"},
		)
	}

	if argumentPosition {
		return nil
	}
	return prompt.FilterHasPrefix(suggestions, prefix, true)
}

func (cli *CLI) selectedPayloadType() string {
	for _, session := range cli.snapshot.Sessions {
		if session.Name == cli.selectedSession {
			return session.PayloadType
		}
	}
	return ""
}

func stateForMode(current mode) int {
	switch current {
	case modeListener:
		return types.LISTENER
	case modeSession:
		return types.SESSION
	case modeScript:
		return types.SCRIPT
	case modeLoot:
		return types.LOOT
	case modeProfile:
		return types.IMPLANT_BUILD
	default:
		return types.NIL
	}
}

func (cli *CLI) help() {
	cli.mu.RLock()
	defer cli.mu.RUnlock()

	entries := core.HelpEntries(stateForMode(cli.mode))
	entries = append(entries, core.HelpEntry{Command: "message <text>", Description: "Broadcast a message to connected users."})
	switch cli.mode {
	case modeListener:
		entries = append(entries,
			core.HelpEntry{Command: "host list", Description: "List hosted URLs and the default 404 page."},
			core.HelpEntry{Command: "host add", Description: "Use `host add <url-path> <source-path> [status] [headers-json]`."},
			core.HelpEntry{Command: "host remove", Description: "Remove a hosted URL path."},
			core.HelpEntry{Command: "host not-found", Description: "Use `host not-found <source-path> [headers-json]`."},
			core.HelpEntry{Command: "host clear-not-found", Description: "Remove the configured default 404 page."},
			core.HelpEntry{Command: "restart", Description: "Restart a listener."},
			core.HelpEntry{Command: "select", Description: "Alias for `interact <name>`."},
		)
	case modeSession:
		entries = append(entries,
			core.HelpEntry{Command: "tasks", Description: "List tasks for the selected session."},
			core.HelpEntry{Command: "interactive/ssh", Description: "Open an interactive SSH session."},
		)
	case modeProfile:
		entries = append(entries,
			core.HelpEntry{Command: "builders", Description: "List registered Lua payload builders."},
			core.HelpEntry{Command: "builds", Description: "List implant builds."},
			core.HelpEntry{Command: "download", Description: "Download a build. Use `download <id> <destination>`."},
			core.HelpEntry{Command: "delete-build", Description: "Delete build history and its stored artifact."},
			core.HelpEntry{Command: "interact", Description: "Alias for `select <name>`."},
		)
	}

	table := tabby.New()
	table.AddHeader("GENERIC COMMAND", "DESCRIPTION")
	for _, entry := range entries {
		table.AddLine(entry.Command, entry.Description)
	}
	fmt.Println()
	table.Print()
	fmt.Println()

	if cli.mode == modeSession {
		commands := tabby.New()
		commands.AddHeader("AVAILABLE COMMAND", "DESCRIPTION")
		payloadType := cli.selectedPayloadType()
		for _, command := range cli.snapshot.Commands {
			if command.PayloadType == payloadType {
				commands.AddLine(command.Name, command.Description)
			}
		}
		commands.Print()
		fmt.Println()
	}
}

func printListeners(items []teamapi.Listener) {
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	table := tabby.New()
	table.AddHeader("NAME", "HOST", "PORT", "RUNNING", "PERSIST", "SESSIONS")
	for _, item := range items {
		table.AddLine(item.Name, item.Host, item.Port, item.Running, item.Persistent, item.Associations)
	}
	table.Print()
}
func printSessions(items []teamapi.Session) {
	table := tabby.New()
	table.AddHeader("NAME", "TYPE", "USER", "HOST", "PID", "ALIVE", "LAST SEEN")
	for _, item := range items {
		table.AddLine(item.Name, item.PayloadType, item.User, item.Hostname, item.PID, item.Alive, item.LastSeen.Format(time.RFC3339))
	}
	table.Print()
}
func printTasks(items []teamapi.Task) {
	table := tabby.New()
	table.AddHeader("ID", "CODE", "STATUS", "ATTEMPTS", "REGISTERED")
	for _, item := range items {
		table.AddLine(item.ID, item.Code, item.Status, item.Attempts, item.Registered.Format(time.RFC3339))
	}
	table.Print()
}
func printProfiles(items []teamapi.Profile) {
	table := tabby.New()
	table.AddHeader("NAME", "TYPE", "LHOST", "OS/ARCH", "BUILDER", "OUTPUT", "TEMPLATE")
	for _, item := range items {
		builder := item.Builder
		if builder == "" {
			builder = "legacy"
		}
		table.AddLine(item.Name, item.Type, item.LHOST, item.OS+"/"+item.ARCH, builder, item.Output, item.Template)
	}
	table.Print()
}
func wordsToSuggestions(words ...string) []prompt.Suggest {
	result := make([]prompt.Suggest, 0, len(words))
	for _, word := range words {
		result = append(result, prompt.Suggest{Text: word})
	}
	return result
}
func isOneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
func shorten(value string, length int) string {
	if len(value) <= length {
		return value
	}
	return value[:length]
}
func errorsNew(message string) error { return fmt.Errorf("%s", message) }

func (cli *CLI) readAttachments(arguments []string) (map[string]string, error) {
	attachments := make(map[string]string)
	for _, argument := range arguments {
		if !strings.HasPrefix(argument, "@") || len(argument) == 1 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		uploadID, err := cli.client.UploadAttachment(ctx, strings.TrimPrefix(argument, "@"))
		cancel()
		if err != nil {
			return nil, fmt.Errorf("upload attachment %s: %w", argument, err)
		}
		attachments[argument] = uploadID
	}
	return attachments, nil
}
