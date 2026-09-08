package lua

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/log"

	lua "github.com/yuin/gopher-lua"
)

const (
	maxPayloadBuildSource  = 4 << 20
	maxPayloadBuildCommand = 16 << 10
)

// payloadBuild exposes payload_build(name, description, function). The
// registration lives in implantbuilder so build dispatch does not introduce a
// package cycle back into Lua.
func (profile *LuaProfile) payloadBuild(state *lua.LState) int {
	name := state.CheckString(1)
	description := state.CheckString(2)
	function := state.CheckFunction(3)
	err := implantbuilder.RegisterPayloadBuilder(
		name,
		description,
		profile.script,
		func(profileName string, buildProfile implantbuilder.Profile) error {
			return profile.callPayloadBuilder(name, function, profileName, buildProfile)
		},
	)
	if err != nil {
		state.RaiseError("payload_build: %v", err)
	}
	logPayloadBuilderRegistration(name, description, profile.script)
	return 0
}

func (profile *LuaProfile) callPayloadBuilder(
	builderName string,
	function *lua.LFunction,
	profileName string,
	buildProfile implantbuilder.Profile,
) error {
	info, err := os.Stat(buildProfile.Template)
	if err != nil {
		return fmt.Errorf("template directory %s: %w", buildProfile.Template, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("template path %s is not a directory", buildProfile.Template)
	}
	projectRoot := findGoModuleRoot(buildProfile.Template)
	workingDirectory := projectRoot
	if workingDirectory == "" {
		workingDirectory = buildProfile.Template
	}

	profile.stateMu.Lock()
	defer profile.stateMu.Unlock()
	if profile.state == nil {
		return fmt.Errorf("script %q was unloaded", profile.script)
	}
	if profile.executionBuild != nil {
		return errors.New("a payload build is already active in this Lua state")
	}
	profile.executionBuild = &payloadBuildExecution{
		BuilderName: builderName,
		ProfileName: profileName,
		Workspace:   workingDirectory,
		ProjectRoot: projectRoot,
		Profile:     buildProfile,
	}
	defer func() { profile.executionBuild = nil }()

	profile.state.Push(function)
	profile.state.Push(lua.LString(profileName))
	if err := profile.state.PCall(1, 0, nil); err != nil {
		return err
	}
	return nil
}

// implantProfile exposes implant_profile(profile_name) and returns a snapshot
// of the selected profile. During its own build, path fields are absolute and
// workspace identifies the directory used by os.exec.
func (profile *LuaProfile) implantProfile(state *lua.LState) int {
	name := state.CheckString(1)
	item, err := implantbuilder.APIGetProfile(name)
	if err != nil {
		state.RaiseError("implant_profile: %v", err)
		return 0
	}
	if execution := profile.executionBuild; execution != nil && execution.ProfileName == name {
		item.Type = execution.Profile.Type
		item.Mode = execution.Profile.Mode
		item.LHOST = execution.Profile.LHOST
		item.OS = execution.Profile.OS
		item.ARCH = execution.Profile.ARCH
		item.Output = execution.Profile.Output
		item.Template = execution.Profile.Template
		item.PublicKey = execution.Profile.PublicKey
		item.Builder = execution.Profile.Builder
		item.ListenerUUID = execution.Profile.ListenerUUID
	}
	table := luaProfileTable(state, item)
	if execution := profile.executionBuild; execution != nil && execution.ProfileName == name {
		state.SetField(table, "workspace", lua.LString(execution.Workspace))
		state.SetField(table, "project_root", lua.LString(execution.ProjectRoot))
		state.SetField(table, "build_id", lua.LString(execution.Profile.BuildID))
	}
	state.Push(table)
	return 1
}

func luaProfileTable(state *lua.LState, profile teamapi.Profile) *lua.LTable {
	table := state.NewTable()
	state.SetField(table, "name", lua.LString(profile.Name))
	state.SetField(table, "type", lua.LString(profile.Type))
	state.SetField(table, "mode", lua.LString(profile.Mode))
	state.SetField(table, "lhost", lua.LString(profile.LHOST))
	state.SetField(table, "os", lua.LString(profile.OS))
	state.SetField(table, "arch", lua.LString(profile.ARCH))
	state.SetField(table, "output", lua.LString(profile.Output))
	state.SetField(table, "template", lua.LString(profile.Template))
	state.SetField(table, "public_key", lua.LString(profile.PublicKey))
	state.SetField(table, "builder", lua.LString(profile.Builder))
	state.SetField(table, "listener_uuid", lua.LString(profile.ListenerUUID))
	state.SetField(table, "protocol", lua.LString(profile.Protocol))
	state.SetField(table, "options_json", lua.LString(profile.Options))
	state.SetField(table, "os_options", luaStringTable(state, profile.OSOptions))
	state.SetField(table, "arch_options", luaStringTable(state, profile.ARCHOptions))
	state.SetField(table, "workspace", lua.LString(""))
	state.SetField(table, "project_root", lua.LString(""))
	state.SetField(table, "build_id", lua.LString(""))
	return table
}

func luaStringTable(state *lua.LState, values []string) *lua.LTable {
	table := state.NewTable()
	for _, value := range values {
		table.Append(lua.LString(value))
	}
	return table
}

func (profile *LuaProfile) payloadBuildWrite(state *lua.LState) int {
	execution := profile.requirePayloadBuild(state, "os.write")
	if execution == nil {
		return 0
	}
	source := state.CheckString(1)
	if len(source) > maxPayloadBuildSource {
		return pushPayloadBuildWriteError(state, fmt.Errorf("build source must not exceed %d bytes", maxPayloadBuildSource))
	}
	destination := strings.TrimSpace(state.CheckString(2))
	if destination == "" {
		return pushPayloadBuildWriteError(state, errors.New("destination path must not be empty"))
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(execution.Workspace, destination)
	}
	destination = filepath.Clean(destination)
	rendered, err := implantbuilder.RenderGoSource(execution.Profile, source)
	if err != nil {
		return pushPayloadBuildWriteError(state, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return pushPayloadBuildWriteError(state, err)
	}
	if err := os.WriteFile(destination, []byte(rendered), 0600); err != nil {
		return pushPayloadBuildWriteError(state, err)
	}
	state.Push(lua.LNil)
	return 1
}

func pushPayloadBuildWriteError(state *lua.LState, err error) int {
	state.Push(lua.LString(err.Error()))
	return 1
}

func (profile *LuaProfile) payloadBuildExec(state *lua.LState) int {
	execution := profile.requirePayloadBuild(state, "os.exec")
	if execution == nil {
		return 0
	}
	command := strings.TrimSpace(state.CheckString(1))
	if command == "" {
		state.ArgError(1, "build command must not be empty")
		return 0
	}
	if len(command) > maxPayloadBuildCommand {
		state.ArgError(1, fmt.Sprintf("build command must not exceed %d bytes", maxPayloadBuildCommand))
		return 0
	}

	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		process = exec.CommandContext(context.Background(), "cmd.exe", "/C", command)
	} else {
		process = exec.CommandContext(context.Background(), "/bin/sh", "-c", command)
	}
	process.Dir = execution.Workspace
	process.Env = append(os.Environ(),
		"GOOS="+execution.Profile.OS,
		"GOARCH="+execution.Profile.ARCH,
		"CGO_ENABLED=0",
		"PURPCMD_PROFILE="+execution.ProfileName,
		"PURPCMD_BUILDER="+execution.BuilderName,
		"PURPCMD_OUTPUT="+execution.Profile.Output,
		"PURPCMD_TEMPLATE="+execution.Profile.Template,
		"PURPCMD_PUBLIC_KEY="+execution.Profile.PublicKey,
		"PURPCMD_LHOST="+execution.Profile.LHOST,
		"PURPCMD_PAYLOAD_TYPE="+execution.Profile.Type,
		"PURPCMD_MODE="+execution.Profile.Mode,
		"PURPCMD_OTS_TOKEN="+fmt.Sprintf("%x", execution.Profile.OTSToken),
		"PURPCMD_LISTENER_UUID="+execution.Profile.ListenerUUID,
		"PURPCMD_PROJECT_ROOT="+execution.ProjectRoot,
		"PURPCMD_BUILD_ID="+execution.Profile.BuildID,
	)
	var output implantbuilder.BuildOutputCapture
	combined := io.MultiWriter(os.Stdout, &output)
	process.Stdout = combined
	process.Stderr = combined
	err := process.Run()
	message := output.String()
	implantbuilder.PublishBuildOutput(execution.Profile, execution.BuilderName, message)
	if err != nil {
		state.RaiseError("os.exec: %v", err)
		return 0
	}
	state.Push(lua.LString(message))
	return 1
}

func (profile *LuaProfile) requirePayloadBuild(state *lua.LState, function string) *payloadBuildExecution {
	if profile.executionBuild == nil {
		state.RaiseError("%s is only available while a payload builder is running", function)
		return nil
	}
	return profile.executionBuild
}

func findGoModuleRoot(start string) string {
	current := filepath.Clean(start)
	for {
		if info, err := os.Stat(filepath.Join(current, "go.mod")); err == nil && info.Mode().IsRegular() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func payloadBuildShellQuote(state *lua.LState) int {
	value := state.CheckString(1)
	state.Push(lua.LString("'" + strings.ReplaceAll(value, "'", "'\\''") + "'"))
	return 1
}

func installPayloadBuildOSFunctions(profile *LuaProfile) error {
	osTable, ok := profile.state.GetGlobal("os").(*lua.LTable)
	if !ok {
		return errors.New("Lua os library is unavailable")
	}
	profile.state.SetField(osTable, "write", profile.state.NewFunction(profile.payloadBuildWrite))
	profile.state.SetField(osTable, "exec", profile.state.NewFunction(profile.payloadBuildExec))
	profile.state.SetField(osTable, "quote", profile.state.NewFunction(payloadBuildShellQuote))
	return nil
}

func logPayloadBuilderRegistration(name, description, script string) {
	message := fmt.Sprintf("Registered Lua payload builder %q from %s", name, script)
	if description != "" {
		message += ": " + description
	}
	log.PrintInfo(message)
}
