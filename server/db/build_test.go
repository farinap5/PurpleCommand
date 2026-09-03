package db

import (
	"database/sql"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestBuildJobBuilderColumnMigratesAndPersists(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })

	if _, err := connection.Exec(`
CREATE TABLE BuildJobs (
    ID TEXT PRIMARY KEY,
    Profile TEXT NOT NULL,
    Status TEXT NOT NULL,
    ArtifactName TEXT,
    Error TEXT,
    CreatedAt TEXT NOT NULL,
    CompletedAt TEXT
);
INSERT INTO BuildJobs (ID, Profile, Status, ArtifactName, Error, CreatedAt)
VALUES ('legacy', 'linux', 'completed', '', '', '2026-08-27T00:00:00Z');
`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatalf("migrate build jobs: %v", err)
	}
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatalf("repeat build job migration: %v", err)
	}

	jobs, err := DBBuildList()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "legacy" || jobs[0].Builder != "" {
		t.Fatalf("migrated build jobs = %#v", jobs)
	}

	job := teamapi.Build{
		ID: uuid.NewString(), Profile: "linux", Builder: "linux-builder", Status: "queued",
		CreatedAt: time.Now().UTC(),
	}
	if err := DBBuildSave(job); err != nil {
		t.Fatal(err)
	}
	jobs, err = DBBuildList()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].ID != job.ID || jobs[0].Builder != "linux-builder" {
		t.Fatalf("persisted build jobs = %#v", jobs)
	}
}
