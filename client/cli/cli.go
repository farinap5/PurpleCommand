package cli

import (
	"context"
	"fmt"
	"os"
	"sort"
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
		eventHandler(event)
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
		return errorsNew("listener commands: list, new, interact, options, set, start, stop, restart, delete, back")
	}
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
		name, err := cli.profileName(fields[1:])
		if err != nil {
			return err
		}
		var build teamapi.Build
		if err := cli.request(teamapi.AskBuildCreate, teamapi.BuildRequest{Profile: name}, &build); err != nil {
			return err
		}
		fmt.Println("build queued:", build.ID)
	case "builds":
		var items []teamapi.Build
		if err := cli.request(teamapi.AskBuildList, struct{}{}, &items); err != nil {
			return err
		}
		table := tabby.New()
		table.AddHeader("ID", "PROFILE", "STATUS", "ARTIFACT", "ERROR")
		for _, item := range items {
			table.AddLine(item.ID, item.Profile, item.Status, item.ArtifactName, item.Error)
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
		return errorsNew("implant commands: list, new, select, options, set, generate, builds, download, delete, back")
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
		if argumentIndex == 1 && isOneOf(command, "interact", "select", "options", "start", "run", "stop", "restart", "delete") {
			suggestions = nil
			for _, item := range cli.snapshot.Listeners {
				suggestions = append(suggestions, prompt.Suggest{Text: item.Name, Description: item.Host + ":" + item.Port})
			}
			return prompt.FilterHasPrefix(suggestions, prefix, true)
		}
		suggestions = append(suggestions,
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
			prompt.Suggest{Text: "builds", Description: "List implant builds"},
			prompt.Suggest{Text: "download", Description: "Download a completed build"},
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
	switch cli.mode {
	case modeListener:
		entries = append(entries,
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
			core.HelpEntry{Command: "builds", Description: "List implant builds."},
			core.HelpEntry{Command: "download", Description: "Download a build. Use `download <id> <destination>`."},
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
	table.AddHeader("NAME", "TYPE", "LHOST", "OS/ARCH", "URI", "OUTPUT", "TEMPLATE")
	for _, item := range items {
		table.AddLine(item.Name, item.Type, item.LHOST, item.OS+"/"+item.ARCH, item.URI, item.Output, item.Template)
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
