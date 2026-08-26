package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"

	_ "github.com/mattn/go-sqlite3"
)

func TestRunRetentionCleanupPrunesImmediately(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	previous := db.DBMS
	db.DBMS = db.DBDef{DBConn: connection}
	t.Cleanup(func() {
		db.DBMS = previous
		_ = connection.Close()
	})
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
	if err := db.DBEventRetentionSet(teamapi.EventSessionCheckin, "test", time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DBEventInsert(
		teamapi.EventSessionCheckin,
		json.RawMessage(`{"session":"old"}`),
		time.Now().UTC().Add(-time.Hour),
	); err != nil {
		t.Fatal(err)
	}

	type cleanupResult struct {
		deleted int64
		err     error
	}
	resultChannel := make(chan cleanupResult, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go New().RunRetentionCleanup(ctx, time.Hour, 10, func(deleted int64, err error) {
		resultChannel <- cleanupResult{deleted: deleted, err: err}
	})

	select {
	case result := <-resultChannel:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.deleted != 1 {
			t.Fatalf("cleanup deleted %d events, want 1", result.deleted)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retention cleanup did not run immediately")
	}
	events, err := db.DBEventList(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expired events were retained: %#v", events)
	}
}
