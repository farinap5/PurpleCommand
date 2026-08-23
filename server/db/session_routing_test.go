package db

import (
	"database/sql"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"

	_ "github.com/mattn/go-sqlite3"
)

func TestSessionRoutingColumnsMigrateAndPersist(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })

	if _, err := connection.Exec(`
CREATE TABLE Sessions (
    Name TEXT PRIMARY KEY,
    Uuid TEXT NOT NULL,
    PayloadType TEXT NOT NULL,
    Metadata BLOB NOT NULL,
    Alive BOOLEAN NOT NULL,
    Terminating BOOLEAN NOT NULL,
    FirstSeen TEXT NOT NULL,
    LastSeen TEXT NOT NULL
);`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatalf("migrate session routing: %v", err)
	}
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatalf("repeat session migration: %v", err)
	}

	now := time.Now().UTC()
	if err := DBSessionSave(teamapi.Session{
		Name: "bind-session", UUID: "uuid", PayloadType: "bind.impl",
		Transport: teamapi.SessionTransportSpeaker, Speaker: "bind-http",
		Alive: true, FirstSeen: now, LastSeen: now,
	}, []byte(`{"type":"bind.impl"}`)); err != nil {
		t.Fatal(err)
	}
	sessions, err := DBSessionList()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Session.Transport != teamapi.SessionTransportSpeaker || sessions[0].Session.Speaker != "bind-http" {
		t.Fatalf("persisted route = %#v", sessions)
	}
}
