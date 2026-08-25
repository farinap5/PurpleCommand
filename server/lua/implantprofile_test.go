package lua

import (
	"bytes"
	"crypto/sha256"
	"path/filepath"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implantbuilder"

	golua "github.com/yuin/gopher-lua"
)

func TestStructuredImplantDefinitionPersistsAndProjectsToBuilder(t *testing.T) {
	previousDatabasePath := db.DatabasePath
	previousDatabase := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "definitions.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DBMS = previousDatabase
		db.DatabasePath = previousDatabasePath
	})

	previousProfiles := implantbuilder.ProfileMap
	previousCurrentName := implantbuilder.CurrentName
	implantbuilder.ProfileMap = make(map[string]*implantbuilder.Profile)
	implantbuilder.CurrentName = ""
	t.Cleanup(func() {
		implantbuilder.ProfileMap = previousProfiles
		implantbuilder.CurrentName = previousCurrentName
	})

	luaImplantDefinitionsMu.Lock()
	previousDefinitions := luaImplantDefinitions
	luaImplantDefinitions = make(map[string]LuaImplantDefinition)
	luaImplantDefinitionsMu.Unlock()
	t.Cleanup(func() {
		luaImplantDefinitionsMu.Lock()
		luaImplantDefinitions = previousDefinitions
		luaImplantDefinitionsMu.Unlock()
	})

	state := golua.NewState()
	defer state.Close()
	state.SetGlobal("implant_register_profile", state.NewFunction(LuaRegisterImplantProfile))
	if err := state.DoString(`
implant_register_profile("linux-impl", {
    OS = {"linux", "windows"},
    ARCH = {"amd64", "386"},
    PROTOCOL = "http",
    TYPE = "impl",
    OPTIONS = {
        PATH = "/health",
        HEADER = {
            ["User-Agent"] = "",
            ["X-Test"] = "Test",
        },
    },
    OTS = "one-time-secret",
})
`); err != nil {
		t.Fatal(err)
	}

	definition, found := LuaGetImplantDefinition("linux-impl")
	if !found {
		t.Fatal("persisted definition was not found")
	}
	wantHash := sha256.Sum256([]byte("one-time-secret"))
	if !bytes.Equal(definition.OTSHash, wantHash[:]) {
		t.Fatalf("OTS hash = %x", definition.OTSHash)
	}
	if definition.Options.Path != "/health" || definition.Options.Headers["X-Test"] != "Test" {
		t.Fatalf("definition = %#v", definition)
	}
	if len(definition.OperatingSystems) != 2 || definition.OperatingSystems[1] != "windows" ||
		len(definition.Architectures) != 2 || definition.Architectures[1] != "386" {
		t.Fatalf("target suggestions = %#v / %#v", definition.OperatingSystems, definition.Architectures)
	}
	profile, err := implantbuilder.APIGetProfile("linux-impl")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Type != "impl" || profile.OS != "linux" || profile.ARCH != "amd64" ||
		len(profile.OSOptions) != 2 || len(profile.ARCHOptions) != 2 {
		t.Fatalf("builder projection = %#v", profile)
	}
	if profile.Protocol != "http" || !profile.OTSConfigured ||
		!bytes.Contains(profile.Options, []byte(`"X-Test":"Test"`)) {
		t.Fatalf("profile definition reply = %#v", profile)
	}

	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if _, err := implantbuilder.APIUpdateProfile(teamapi.ProfileUpdateRequest{
		Name: "linux-impl", Key: "OPTIONS",
		Value: `{"path":"/managed","header":{"X-UI":"preserved"},"proxy":{"enabled":true}}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := implantbuilder.APIUpdateProfile(teamapi.ProfileUpdateRequest{
		Name: "linux-impl", Key: "OTS", Value: "ui-managed-secret",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := implantbuilder.APIUpdateProfile(teamapi.ProfileUpdateRequest{
		Name: "linux-impl", Key: "OTS_EXPIRES_AT", Value: expiresAt.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.DoString(`
implant_register_profile("linux-impl", {
    OS = {"linux", "windows"}, ARCH = {"amd64", "386"},
    PROTOCOL = "http", TYPE = "impl",
    OPTIONS = { PATH = "/script-default", HEADER = { ["X-Script"] = "default" } },
    OTS = "script-default-secret",
})
`); err != nil {
		t.Fatal(err)
	}
	profile, err = implantbuilder.APIGetProfile("linux-impl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(profile.Options, []byte(`"proxy":{"enabled":true}`)) ||
		bytes.Contains(profile.Options, []byte("script-default")) ||
		profile.OTSExpiresAt == nil || !profile.OTSExpiresAt.Equal(expiresAt) {
		t.Fatalf("script reload replaced managed definition = %#v", profile)
	}
	managedHash := sha256.Sum256([]byte("ui-managed-secret"))
	definition, found = LuaGetImplantDefinition("linux-impl")
	if !found || !bytes.Equal(definition.OTSHash, managedHash[:]) ||
		!bytes.Contains(definition.OptionsJSON, []byte(`"proxy":{"enabled":true}`)) {
		t.Fatalf("managed Lua definition = %#v found=%t", definition, found)
	}

	luaImplantDefinitionsMu.Lock()
	luaImplantDefinitions = make(map[string]LuaImplantDefinition)
	luaImplantDefinitionsMu.Unlock()
	ImplantDefinitionsReloadFromDB()
	if _, found := LuaGetImplantDefinition("linux-impl"); !found {
		t.Fatal("definition did not survive registry reload")
	}

	if err := implantbuilder.APIDeleteProfile("linux-impl"); err != nil {
		t.Fatal(err)
	}
	if _, found := LuaGetImplantDefinition("linux-impl"); found {
		t.Fatal("definition survived builder profile deletion")
	}
}
