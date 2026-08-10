package lua

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePersistedScriptPathRelocatesMovedCheckout(t *testing.T) {
	workingDirectory := t.TempDir()
	scriptDirectory := filepath.Join(workingDirectory, "script")
	if err := os.Mkdir(scriptDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	wanted := filepath.Join(scriptDirectory, "main.lua")
	if err := os.WriteFile(wanted, []byte("function Main() end\n"), 0600); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolvePersistedScriptPath(
		"/home/pietro/go/src/PurpleCommand/script/main.lua",
		workingDirectory,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wanted {
		t.Fatalf("resolved path = %q, want %q", resolved, wanted)
	}
}

func TestResolvePersistedScriptPathRejectsMissingFile(t *testing.T) {
	if _, err := resolvePersistedScriptPath("/old/script/missing.lua", t.TempDir()); err == nil {
		t.Fatal("missing persisted script resolved successfully")
	}
}
