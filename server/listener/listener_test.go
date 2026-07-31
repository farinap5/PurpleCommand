package listener

import (
	"database/sql"
	"errors"
	"testing"

	"purpcmd/server/db"
)

func setupListenerTestDB(t *testing.T) {
	t.Helper()
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`
		CREATE TABLE Listeners (
			Lid INTEGER PRIMARY KEY AUTOINCREMENT,
			Uuid TEXT NOT NULL UNIQUE,
			Name TEXT NOT NULL UNIQUE,
			Host TEXT NOT NULL,
			Port TEXT NOT NULL,
			Persist BOOLEAN NOT NULL,
			Running BOOLEAN NOT NULL
		);`); err != nil {
		t.Fatal(err)
	}

	previousDB := db.DBMS
	db.DBMS = db.DBDef{DBConn: conn}
	ListenerMAP = make(map[string]*Listener)
	CurrentListener = "none"
	t.Cleanup(func() {
		_ = conn.Close()
		db.DBMS = previousDB
		ListenerMAP = make(map[string]*Listener)
		CurrentListener = "none"
	})
}

func TestListenerPersistenceLifecycle(t *testing.T) {
	setupListenerTestDB(t)

	if err := ListenerNew("alpha"); err != nil {
		t.Fatalf("create listener: %v", err)
	}
	if !db.DBListenerExist("alpha") {
		t.Fatal("new persistent listener was not stored")
	}
	if err := ListenerSetOptions("persist", "false"); err != nil {
		t.Fatalf("disable persistence: %v", err)
	}
	if db.DBListenerExist("alpha") {
		t.Fatal("non-persistent listener remained in the database")
	}
	if err := ListenerSetOptions("host", "127.0.0.1"); err != nil {
		t.Fatalf("update ephemeral listener: %v", err)
	}
	if db.DBListenerExist("alpha") {
		t.Fatal("updating an ephemeral listener recreated a database row")
	}
	if err := ListenerSetOptions("persist", "true"); err != nil {
		t.Fatalf("enable persistence: %v", err)
	}

	var host string
	if err := db.DBMS.DBConn.QueryRow(`SELECT Host FROM Listeners WHERE Name = ?`, "alpha").Scan(&host); err != nil {
		t.Fatalf("read persisted listener: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("persisted listener used stale host %q", host)
	}

	if err := ListenerDelete(); err != nil {
		t.Fatalf("delete listener: %v", err)
	}
	if db.DBListenerExist("alpha") || ListenerMAP["alpha"] != nil {
		t.Fatal("listener deletion did not remove both database and memory state")
	}
}

func TestListenerReloadDoesNotReinsertOrResurrectEphemeralRows(t *testing.T) {
	setupListenerTestDB(t)
	if err := db.DBListenerInsert("persisted", "11111111-1111-1111-1111-111111111111", "127.0.0.1", "4444", true, false); err != nil {
		t.Fatal(err)
	}
	if err := db.DBListenerInsert("stale", "22222222-2222-2222-2222-222222222222", "127.0.0.1", "5555", false, false); err != nil {
		t.Fatal(err)
	}

	if err := ListenerInitFromDB(); err != nil {
		t.Fatalf("reload listeners: %v", err)
	}
	if ListenerMAP["persisted"] == nil {
		t.Fatal("persistent listener was not loaded")
	}
	if ListenerMAP["stale"] != nil || db.DBListenerExist("stale") {
		t.Fatal("non-persistent stale row was resurrected")
	}
}

func TestStartingRunningListenerIsRejected(t *testing.T) {
	l := newListener("running", "33333333-3333-3333-3333-333333333333", "127.0.0.1", "0", false)
	l.SC.running = true
	if err := l.StartHTTP(); !errors.Is(err, ErrListenerRunning) {
		t.Fatalf("expected ErrListenerRunning, got %v", err)
	}
}
