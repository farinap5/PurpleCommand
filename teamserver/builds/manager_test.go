package builds

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/runtimeevents"
	"purpcmd/teamserver/events"

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

func TestManagerPublishesCorrelatedBuildLifecycle(t *testing.T) {
	buildTestDatabase(t)
	runtimeevents.SetPublisher(nil)
	previousProfiles := implantbuilder.ProfileMap
	implantbuilder.ProfileMap = make(map[string]*implantbuilder.Profile)
	t.Cleanup(func() {
		runtimeevents.SetPublisher(nil)
		implantbuilder.ProfileMap = previousProfiles
	})

	const builderSource = "build-manager-events.lua"
	implantbuilder.UnregisterPayloadBuilders(builderSource)
	t.Cleanup(func() { implantbuilder.UnregisterPayloadBuilders(builderSource) })
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	buildCalls := 0
	output := filepath.Join(t.TempDir(), "implant")
	if err := implantbuilder.RegisterPayloadBuilder(
		"event-builder", "event test", builderSource,
		func(_ string, profile implantbuilder.Profile) error {
			buildCalls++
			entered <- struct{}{}
			if buildCalls == 1 {
				<-release
			}
			implantbuilder.PublishBuildOutput(profile, profile.Builder, "compiler output")
			if buildCalls == 2 {
				return errors.New("compiler failed")
			}
			return os.WriteFile(profile.Output, []byte("artifact"), 0600)
		},
	); err != nil {
		t.Fatal(err)
	}
	implantbuilder.ProfileMap["event-profile"] = &implantbuilder.Profile{
		Type: "impl", LHOST: "127.0.0.1:4444", OS: "linux", ARCH: "amd64",
		Template: t.TempDir(), Output: output, Builder: "event-builder",
	}

	bus := events.New()
	records, unsubscribe := bus.Subscribe(16)
	t.Cleanup(unsubscribe)
	runtimeevents.SetPublisher(func(eventType string, value any) { _, _ = bus.Publish(eventType, value) })
	manager := New(bus, t.TempDir())
	job, err := manager.Create("event-profile", "")
	if err != nil {
		t.Fatal(err)
	}

	queued := nextBuildEvent(t, records)
	if queued.Type != teamapi.EventBuildQueued {
		t.Fatalf("first build event = %q, want queued", queued.Type)
	}
	assertBuildEvent(t, queued, job.ID, "queued")
	started := nextBuildEvent(t, records)
	if started.Type != teamapi.EventBuildStarted {
		t.Fatalf("second build event = %q, want started", started.Type)
	}
	assertBuildEvent(t, started, job.ID, "running")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("payload builder did not start")
	}
	failedJob, err := manager.Create("event-profile", "")
	if err != nil {
		t.Fatal(err)
	}
	secondQueued := nextBuildEvent(t, records)
	if secondQueued.Type != teamapi.EventBuildQueued {
		t.Fatalf("third build event = %q, want second queued", secondQueued.Type)
	}
	assertBuildEvent(t, secondQueued, failedJob.ID, "queued")
	if current, err := manager.Get(failedJob.ID); err != nil || current.Status != "queued" {
		t.Fatalf("waiting build = %#v, %v", current, err)
	}
	close(release)

	outputEvent := nextBuildEvent(t, records)
	if outputEvent.Type != teamapi.EventBuildOutput {
		t.Fatalf("fourth build event = %q, want output", outputEvent.Type)
	}
	var buildOutput teamapi.BuildOutput
	if err := json.Unmarshal(outputEvent.Data, &buildOutput); err != nil {
		t.Fatal(err)
	}
	if buildOutput.BuildID != job.ID || buildOutput.Profile != "event-profile" ||
		buildOutput.Builder != "event-builder" || buildOutput.Message != "compiler output" {
		t.Fatalf("build output = %#v", buildOutput)
	}
	completed := nextBuildEvent(t, records)
	if completed.Type != teamapi.EventBuildCompleted {
		t.Fatalf("fifth build event = %q, want completed", completed.Type)
	}
	assertBuildEvent(t, completed, job.ID, "completed")

	secondStarted := nextBuildEvent(t, records)
	if secondStarted.Type != teamapi.EventBuildStarted {
		t.Fatalf("sixth build event = %q, want second started", secondStarted.Type)
	}
	assertBuildEvent(t, secondStarted, failedJob.ID, "running")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second payload build did not start")
	}
	secondOutput := nextBuildEvent(t, records)
	if secondOutput.Type != teamapi.EventBuildOutput {
		t.Fatalf("seventh build event = %q, want second output", secondOutput.Type)
	}
	var failedOutput teamapi.BuildOutput
	if err := json.Unmarshal(secondOutput.Data, &failedOutput); err != nil {
		t.Fatal(err)
	}
	if failedOutput.BuildID != failedJob.ID {
		t.Fatalf("failed build output = %#v", failedOutput)
	}
	failed := nextBuildEvent(t, records)
	if failed.Type != teamapi.EventBuildFailed {
		t.Fatalf("eighth build event = %q, want failed", failed.Type)
	}
	assertBuildEvent(t, failed, failedJob.ID, "failed")
	var failedBuild teamapi.Build
	if err := json.Unmarshal(failed.Data, &failedBuild); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(failedBuild.Error, "compiler failed") {
		t.Fatalf("failed build error = %q", failedBuild.Error)
	}
}

func nextBuildEvent(t *testing.T, records <-chan teamapi.EventRecord) teamapi.EventRecord {
	t.Helper()
	select {
	case event := <-records:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for build event")
		return teamapi.EventRecord{}
	}
}

func assertBuildEvent(t *testing.T, event teamapi.EventRecord, id, status string) {
	t.Helper()
	var build teamapi.Build
	if err := json.Unmarshal(event.Data, &build); err != nil {
		t.Fatal(err)
	}
	if build.ID != id || build.Profile != "event-profile" || build.Builder != "event-builder" || build.Status != status {
		t.Fatalf("build lifecycle event = %#v", build)
	}
}
