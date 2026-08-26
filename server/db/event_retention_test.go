package db

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"

	_ "github.com/mattn/go-sqlite3"
)

func eventRetentionTestDatabase(t *testing.T) {
	t.Helper()
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() {
		DBMS = previous
		_ = connection.Close()
	})
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
}

func TestEventRetentionSchemaMigrationPreservesExistingEvents(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() {
		DBMS = previous
		_ = connection.Close()
	})
	if _, err := connection.Exec(`
		CREATE TABLE Events (
			Sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			Type TEXT NOT NULL,
			CreatedAt TEXT NOT NULL,
			Data BLOB NOT NULL
		);
		INSERT INTO Events (Type, CreatedAt, Data)
		VALUES ('evt.legacy', '2026-08-01T00:00:00Z', '{}');
	`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
	var eventCount, configurationCount int
	if err := connection.QueryRow(`SELECT COUNT(*) FROM Events;`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := connection.QueryRow(`SELECT COUNT(*) FROM EventRetentionConfiguration;`).Scan(&configurationCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("legacy event count = %d, want 1", eventCount)
	}
	if configurationCount != len(defaultEventRetentionConfigurations) {
		t.Fatalf("migrated configuration count = %d, want %d", configurationCount, len(defaultEventRetentionConfigurations))
	}
}

func TestEventRetentionDefaultsAndOverrides(t *testing.T) {
	eventRetentionTestDatabase(t)

	configurations, err := DBEventRetentionList()
	if err != nil {
		t.Fatal(err)
	}
	if len(configurations) != len(defaultEventRetentionConfigurations) {
		t.Fatalf("default configuration count = %d, want %d", len(configurations), len(defaultEventRetentionConfigurations))
	}
	byType := make(map[string]EventRetentionConfiguration, len(configurations))
	for _, configuration := range configurations {
		byType[configuration.EventType] = configuration
	}
	if configuration := byType[teamapi.EventSessionCheckin]; configuration.Tier != EventRetentionTierShort || configuration.Retention != 24*time.Hour {
		t.Fatalf("session check-in retention = %#v", configuration)
	}
	if configuration := byType[teamapi.EventUserMessage]; configuration.Tier != EventRetentionTierArchive || configuration.Retention != 90*24*time.Hour {
		t.Fatalf("user message retention = %#v", configuration)
	}

	if err := DBEventRetentionSet(teamapi.EventSessionCheckin, "custom", 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
	configurations, err = DBEventRetentionList()
	if err != nil {
		t.Fatal(err)
	}
	for _, configuration := range configurations {
		if configuration.EventType == teamapi.EventSessionCheckin {
			if configuration.Tier != "custom" || configuration.Retention != 2*time.Hour {
				t.Fatalf("custom retention was replaced by defaults: %#v", configuration)
			}
			return
		}
	}
	t.Fatal("session check-in retention configuration is missing")
}

func TestEventRetentionPrunesInBatchesAndPreservesSequence(t *testing.T) {
	eventRetentionTestDatabase(t)
	now := time.Date(2026, time.August, 26, 12, 0, 0, 123456789, time.UTC)
	type insertedEvent struct {
		eventType string
		createdAt time.Time
	}
	insertions := []insertedEvent{
		{eventType: teamapi.EventSessionCheckin, createdAt: now.Add(-48 * time.Hour)},
		{eventType: teamapi.EventSessionCheckin, createdAt: now.Add(-47 * time.Hour)},
		{eventType: teamapi.EventSessionCheckin, createdAt: now.Add(-46 * time.Hour)},
		{eventType: teamapi.EventSessionCheckin, createdAt: now.Add(-24 * time.Hour)},
		{eventType: teamapi.EventSessionCheckin, createdAt: now.Add(-12 * time.Hour)},
		{eventType: teamapi.EventUserMessage, createdAt: now.Add(-30 * 24 * time.Hour)},
		{eventType: "evt.extension.unknown", createdAt: now.Add(-365 * 24 * time.Hour)},
		{eventType: teamapi.EventUserMessage, createdAt: now.Add(-100 * 24 * time.Hour)},
	}
	var latest uint64
	for _, insertion := range insertions {
		event, err := DBEventInsert(insertion.eventType, json.RawMessage(`{"test":true}`), insertion.createdAt)
		if err != nil {
			t.Fatal(err)
		}
		latest = event.Sequence
	}

	deleted, err := DBEventPruneExpired(now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 4 {
		t.Fatalf("cleanup deleted %d events, want 4", deleted)
	}
	deleted, err = DBEventPruneExpired(now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("second cleanup deleted %d events, want 0", deleted)
	}

	events, err := DBEventList(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("remaining event count = %d, want 4", len(events))
	}
	for _, event := range events {
		if event.Type == teamapi.EventSessionCheckin && event.Time.Before(now.Add(-24*time.Hour)) {
			t.Fatalf("expired check-in was retained: %#v", event)
		}
	}
	foundUnknown := false
	for _, event := range events {
		foundUnknown = foundUnknown || event.Type == "evt.extension.unknown"
	}
	if !foundUnknown {
		t.Fatalf("unconfigured event was removed: %#v", events)
	}

	if err := DBEventRetentionSet(teamapi.EventSessionCheckin, "test", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := DBEventRetentionSet(teamapi.EventUserMessage, "test", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := DBEventRetentionSet("evt.extension.unknown", "test", time.Second); err != nil {
		t.Fatal(err)
	}
	deleted, err = DBEventPruneExpired(now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 4 {
		t.Fatalf("final cleanup deleted %d events, want 4", deleted)
	}
	if events, err := DBEventList(0, 1000); err != nil || len(events) != 0 {
		t.Fatalf("events after final cleanup = %#v, %v", events, err)
	}
	watermark, err := DBEventLatestSequence()
	if err != nil {
		t.Fatal(err)
	}
	if watermark != latest {
		t.Fatalf("watermark after cleanup = %d, want %d", watermark, latest)
	}
	newEvent, err := DBEventInsert(teamapi.EventSessionCheckin, json.RawMessage(`{}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if newEvent.Sequence <= watermark {
		t.Fatalf("new sequence = %d, previous watermark = %d", newEvent.Sequence, watermark)
	}
}

func TestEventRetentionRejectsInvalidConfiguration(t *testing.T) {
	eventRetentionTestDatabase(t)
	for _, test := range []struct {
		name      string
		eventType string
		tier      string
		retention time.Duration
	}{
		{name: "empty event type", tier: "test", retention: time.Hour},
		{name: "empty tier", eventType: "evt.test", retention: time.Hour},
		{name: "negative", eventType: "evt.test", tier: "test", retention: -time.Second},
		{name: "fractional second", eventType: "evt.test", tier: "test", retention: time.Second + time.Nanosecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := DBEventRetentionSet(test.eventType, test.tier, test.retention); err == nil {
				t.Fatal("invalid event retention configuration was accepted")
			}
		})
	}
	if _, err := DBEventPruneExpired(time.Now(), 0); err == nil {
		t.Fatal("invalid event retention cleanup batch size was accepted")
	}
}
