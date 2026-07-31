package loot

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"purpcmd/server/db"
)

func setupLootTestDB(t *testing.T) {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`
		CREATE TABLE Loot (
			lid INTEGER PRIMARY KEY AUTOINCREMENT,
			Uuid TEXT NOT NULL UNIQUE,
			Session TEXT NOT NULL,
			FileName TEXT NOT NULL
		);`); err != nil {
		t.Fatal(err)
	}

	previousDB := db.DBMS
	previousStorageDir := StorageDir
	db.DBMS = db.DBDef{DBConn: conn}
	StorageDir = filepath.Join(t.TempDir(), "loot")
	t.Cleanup(func() {
		_ = conn.Close()
		db.DBMS = previousDB
		StorageDir = previousStorageDir
	})
}

func TestSaveCreatesStorageAndExportTruncates(t *testing.T) {
	setupLootTestDB(t)
	entry := New("session-1", "evidence.txt", []byte("evidence"))
	if err := entry.SaveData(); err != nil {
		t.Fatalf("save loot: %v", err)
	}

	path := filepath.Join(StorageDir, entry.UUID)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored loot: %v", err)
	}
	if string(content) != "evidence" {
		t.Fatalf("stored content = %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("loot permissions = %o, want 600", info.Mode().Perm())
	}
	directoryInfo, err := os.Stat(StorageDir)
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0700 {
		t.Fatalf("loot directory permissions = %o, want 700", directoryInfo.Mode().Perm())
	}

	destination := filepath.Join(t.TempDir(), "export.txt")
	if err := os.WriteFile(destination, []byte("long stale destination content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Export(entry.UUID[:8], destination); err != nil {
		t.Fatalf("export loot: %v", err)
	}
	exported, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(exported) != "evidence" {
		t.Fatalf("export appended instead of truncating: %q", exported)
	}
}

func TestAmbiguousLootFragmentIsRejected(t *testing.T) {
	setupLootTestDB(t)
	first := "aaaaaaaa-0000-4000-8000-000000000001"
	second := "aaaaaaaa-0000-4000-8000-000000000002"
	if err := db.DBLootInsert(first, "one", "one.txt"); err != nil {
		t.Fatal(err)
	}
	if err := db.DBLootInsert(second, "two", "two.txt"); err != nil {
		t.Fatal(err)
	}

	if _, _, err := db.DBLootGetByUUID("aaaaaaaa"); !errors.Is(err, db.ErrAmbiguousLootUUID) {
		t.Fatalf("expected ambiguous UUID error, got %v", err)
	}
	name, fullID, err := db.DBLootGetByUUID(first)
	if err != nil || name != "one.txt" || fullID != first {
		t.Fatalf("exact UUID did not take precedence: name=%q id=%q err=%v", name, fullID, err)
	}
}

func TestSaveRemovesFileWhenDatabaseInsertFails(t *testing.T) {
	setupLootTestDB(t)
	entry := New("session-1", "duplicate.txt", []byte("data"))
	if err := db.DBLootInsert(entry.UUID, entry.Session, entry.FileName); err != nil {
		t.Fatal(err)
	}

	if err := entry.SaveData(); err == nil {
		t.Fatal("expected duplicate database insert to fail")
	}
	if _, err := os.Stat(filepath.Join(StorageDir, entry.UUID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphaned loot file remained after DB failure: %v", err)
	}
}
