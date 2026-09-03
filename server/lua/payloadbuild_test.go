package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/runtimeevents"

	lua "github.com/yuin/gopher-lua"
)

func TestLuaPayloadBuilderRendersProfileAndPublicKey(t *testing.T) {
	isolateLuaDatabase(t)
	previousProfiles := implantbuilder.ProfileMap
	previousCurrentName := implantbuilder.CurrentName
	implantbuilder.ProfileMap = make(map[string]*implantbuilder.Profile)
	implantbuilder.CurrentName = ""
	t.Cleanup(func() {
		implantbuilder.ProfileMap = previousProfiles
		implantbuilder.CurrentName = previousCurrentName
	})

	templateDirectory := t.TempDir()
	output := filepath.Join(t.TempDir(), "lua-payload")
	publicKey, err := filepath.Abs(filepath.Join("..", "..", "server.pub"))
	if err != nil {
		t.Fatal(err)
	}
	implantbuilder.ProfileMap["lua-profile"] = &implantbuilder.Profile{
		Type: "impl", LHOST: "10.20.30.40:4444", OS: "linux", ARCH: "amd64",
		OSOptions: []string{"linux"}, ARCHOptions: []string{"amd64"},
		Output: output, Template: templateDirectory, PublicKey: publicKey, Builder: "lua-test-builder",
	}

	scriptPath := filepath.Join(t.TempDir(), "builder.lua")
	script := `
function test_build(profile_name)
    local profile = implant_profile(profile_name)
    assert(profile.name == "lua-profile")
    assert(profile.builder == "lua-test-builder")
    assert(profile.os == "linux" and profile.arch == "amd64")
    assert(profile.build_id == "lua-build-123")
    local source_path = profile.output .. ".source/nested/main.go"
    local write_err = os.write([[
package main
var publicKeyDER []byte
var lhost = "LHOST"
var payloadType = "IMPLANT_TYPE"
]], source_path)
    assert(write_err == nil, write_err)
    local expected_error = os.write("package main", profile.template)
    assert(type(expected_error) == "string")
    os.exec("pwd > " .. os.quote(profile.output .. ".workspace"))
    os.exec("cp " .. os.quote(source_path) .. " " .. os.quote(profile.output))
    os.exec("printf 'lua build output'")
end
payload_build("lua-test-builder", "test Lua payload builder", test_build)
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	profile, err := LuaNew(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		runtimeevents.SetPublisher(nil)
		implantbuilder.UnregisterPayloadBuilders(scriptPath)
		profile.state.Close()
	})
	var buildOutput teamapi.BuildOutput
	runtimeevents.SetPublisher(func(eventType string, value any) {
		if eventType == teamapi.EventBuildOutput {
			buildOutput = value.(teamapi.BuildOutput)
		}
	})

	if err := implantbuilder.APIGenerateProfileForBuild("lua-profile", "lua-build-123"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	if !strings.Contains(source, `"10.20.30.40:4444"`) || strings.Contains(source, `"LHOST"`) {
		t.Fatalf("LHOST was not rendered:\n%s", source)
	}
	if !strings.Contains(source, `"impl"`) || strings.Contains(source, `"IMPLANT_TYPE"`) {
		t.Fatalf("payload type was not rendered:\n%s", source)
	}
	if !strings.Contains(source, "var publicKeyDER = []byte{") {
		t.Fatalf("public key was not embedded:\n%s", source)
	}
	if buildOutput.BuildID != "lua-build-123" || buildOutput.Profile != "lua-profile" ||
		buildOutput.Builder != "lua-test-builder" || buildOutput.Message != "lua build output" {
		t.Fatalf("Lua build output event = %#v", buildOutput)
	}
	workspaceData, err := os.ReadFile(output + ".workspace")
	if err != nil {
		t.Fatal(err)
	}
	workspace := strings.TrimSpace(string(workspaceData))
	if workspace != templateDirectory {
		t.Fatalf("Lua build working directory = %q, want %q", workspace, templateDirectory)
	}
	sourcePath := output + ".source/nested/main.go"
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("caller-selected source path was not retained: %v", err)
	}
	if err := profile.state.DoString(`os.write("outside build")`); err == nil || !strings.Contains(err.Error(), "only available") {
		t.Fatalf("os.write outside build error = %v", err)
	}
}

func TestLuaPayloadBuilderCommandFailureFailsBuild(t *testing.T) {
	isolateLuaDatabase(t)
	previousProfiles := implantbuilder.ProfileMap
	implantbuilder.ProfileMap = make(map[string]*implantbuilder.Profile)
	t.Cleanup(func() { implantbuilder.ProfileMap = previousProfiles })

	templateDirectory := t.TempDir()
	implantbuilder.ProfileMap["failure-profile"] = &implantbuilder.Profile{
		Type: "impl", LHOST: "127.0.0.1:4444", OS: "linux", ARCH: "amd64",
		Output: filepath.Join(t.TempDir(), "missing"), Template: templateDirectory, Builder: "failure-builder",
	}
	scriptPath := filepath.Join(t.TempDir(), "failure.lua")
	if err := os.WriteFile(scriptPath, []byte(`
function failed_build(profile_name)
    os.exec("exit 7")
end
payload_build("failure-builder", "", failed_build)
`), 0600); err != nil {
		t.Fatal(err)
	}
	profile, err := LuaNew(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		implantbuilder.UnregisterPayloadBuilders(scriptPath)
		profile.state.Close()
	})
	if err := implantbuilder.APIGenerateProfile("failure-profile"); err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("failed Lua build error = %v", err)
	}
}

func TestBundledLuaPayloadBuilderCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Go compilation in short mode")
	}

	isolateLuaCommands(t)
	isolateLuaDatabase(t)

	previousProfiles := implantbuilder.ProfileMap
	previousCurrentName := implantbuilder.CurrentName
	implantbuilder.ProfileMap = make(map[string]*implantbuilder.Profile)
	implantbuilder.CurrentName = ""
	t.Cleanup(func() {
		implantbuilder.ProfileMap = previousProfiles
		implantbuilder.CurrentName = previousCurrentName
	})

	scriptPath, err := filepath.Abs("../../script/main.lua")
	if err != nil {
		t.Fatalf("resolve bundled Lua script: %v", err)
	}
	implantbuilder.UnregisterPayloadBuilders(scriptPath)

	profile, err := LuaNew(scriptPath)
	if err != nil {
		t.Fatalf("load bundled Lua script: %v", err)
	}
	t.Cleanup(func() {
		removeCommandsForScript(scriptPath)
		implantbuilder.UnregisterPayloadBuilders(scriptPath)
		profile.state.Close()
	})
	temporarySourceBase := filepath.Join(t.TempDir(), "bundled-random-source")
	osTable, ok := profile.state.GetGlobal("os").(*lua.LTable)
	if !ok {
		t.Fatal("Lua os library is unavailable")
	}
	profile.state.SetField(osTable, "tmpname", profile.state.NewFunction(func(state *lua.LState) int {
		state.Push(lua.LString(temporarySourceBase))
		return 1
	}))

	templateDirectory, err := filepath.Abs("../../template")
	if err != nil {
		t.Fatalf("resolve template directory: %v", err)
	}
	publicKey, err := filepath.Abs("../../server.pub")
	if err != nil {
		t.Fatalf("resolve public key: %v", err)
	}
	output := filepath.Join(t.TempDir(), "implant")

	updates := []teamapi.ProfileUpdateRequest{
		{Name: "linux-impl", Key: "LHOST", Value: "127.0.0.1:4444"},
		{Name: "linux-impl", Key: "OUTPUT", Value: output},
		{Name: "linux-impl", Key: "TEMPLATE", Value: templateDirectory},
		{Name: "linux-impl", Key: "PUBLICKEY", Value: publicKey},
	}
	for _, update := range updates {
		if _, err := implantbuilder.APIUpdateProfile(update); err != nil {
			t.Fatalf("update profile %s: %v", update.Key, err)
		}
	}

	if err := implantbuilder.APIGenerateProfile("linux-impl"); err != nil {
		t.Fatalf("generate profile with bundled Lua builder: %v", err)
	}

	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("stat generated implant: %v", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("generated implant is not a non-empty regular file: mode=%s size=%d", info.Mode(), info.Size())
	}
	if _, err := os.Stat(temporarySourceBase + ".go"); !os.IsNotExist(err) {
		t.Fatalf("temporary Lua build source was not removed: %v", err)
	}
}
