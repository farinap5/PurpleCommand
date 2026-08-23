package builds

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func buildTestDatabase(t *testing.T) {
	t.Helper()
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	previous := db.DBMS
	db.DBMS = db.DBDef{DBConn: connection}
	t.Cleanup(func() {
		db.DBMS = previous
		_ = connection.Close()
	})
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerListsNewestFirstAndDeletesBuildArtifact(t *testing.T) {
	buildTestDatabase(t)
	artifactDirectory := t.TempDir()
	older := teamapi.Build{
		ID: uuid.NewString(), Profile: "linux", Status: "failed",
		CreatedAt: time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC),
	}
	newer := teamapi.Build{
		ID: uuid.NewString(), Profile: "windows", Status: "completed", ArtifactName: "agent.exe",
		CreatedAt: time.Date(2026, time.August, 21, 10, 0, 0, 0, time.UTC),
	}
	for _, job := range []teamapi.Build{older, newer} {
		if err := db.DBBuildSave(job); err != nil {
			t.Fatal(err)
		}
	}

	manager := New(nil, artifactDirectory)
	source := filepath.Join(t.TempDir(), "agent.exe")
	if err := os.WriteFile(source, []byte("artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.archive(newer.ID, source); err != nil {
		t.Fatal(err)
	}
	items := manager.List()
	if len(items) != 2 || items[0].ID != newer.ID || items[1].ID != older.ID {
		t.Fatalf("build list = %#v", items)
	}
	job, artifactPath, err := manager.Artifact(newer.ID)
	if err != nil || job.ID != newer.ID {
		t.Fatalf("artifact = %#v, %q, %v", job, artifactPath, err)
	}
	if content, err := os.ReadFile(artifactPath); err != nil || string(content) != "artifact" {
		t.Fatalf("artifact content = %q, %v", content, err)
	}

	deleted, err := manager.Delete(newer.ID)
	if err != nil || deleted.ID != newer.ID {
		t.Fatalf("delete = %#v, %v", deleted, err)
	}
	if _, err := os.Stat(artifactPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted artifact stat error = %v", err)
	}
	if _, err := manager.Get(newer.ID); !errors.Is(err, ErrBuildNotFound) {
		t.Fatalf("deleted build lookup error = %v", err)
	}
	persisted, err := db.DBBuildList()
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].ID != older.ID {
		t.Fatalf("persisted builds = %#v", persisted)
	}
}

func TestManagerRejectsActiveBuildDeletion(t *testing.T) {
	buildTestDatabase(t)
	manager := New(nil, t.TempDir())
	job := teamapi.Build{ID: uuid.NewString(), Profile: "linux", Status: "running", CreatedAt: time.Now().UTC()}
	if err := db.DBBuildSave(job); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.jobs[job.ID] = job
	manager.mu.Unlock()

	if _, err := manager.Delete(job.ID); !errors.Is(err, ErrBuildActive) {
		t.Fatalf("active deletion error = %v", err)
	}
	if _, err := manager.Get(job.ID); err != nil {
		t.Fatalf("active build was removed: %v", err)
	}
}

func TestManagerMarksInterruptedBuildsFailed(t *testing.T) {
	buildTestDatabase(t)
	job := teamapi.Build{ID: uuid.NewString(), Profile: "linux", Status: "running", CreatedAt: time.Now().UTC()}
	if err := db.DBBuildSave(job); err != nil {
		t.Fatal(err)
	}
	manager := New(nil, t.TempDir())
	restored, err := manager.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "failed" || restored.CompletedAt.IsZero() || restored.Error == "" {
		t.Fatalf("restored interrupted build = %#v", restored)
	}
}
