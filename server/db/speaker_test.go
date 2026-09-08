package db

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"

	_ "github.com/mattn/go-sqlite3"
)

func TestSpeakerPersistenceAndOptimisticVersioning(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}

	enabled := false
	item := teamapi.Speaker{
		Name: "bind", UUID: "stable-uuid", Persistent: true,
		DesiredState: "stopped", ConfigVersion: 1,
		Config: teamapi.SpeakerConfig{
			Client: teamapi.SpeakerHTTPClientConfig{
				BaseURL: "https://implant.example", Headers: map[string][]string{"Authorization": {"secret"}},
			},
			Healthcheck: &teamapi.SpeakerHealthcheckConfig{Enabled: &enabled, Interval: time.Minute},
		},
	}
	if err := DBSpeakerInsert(item); err != nil {
		t.Fatal(err)
	}
	stored, err := DBSpeakerList()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].UUID != item.UUID || stored[0].Config.Client.Headers.Get("Authorization") != "secret" || stored[0].Config.Healthcheck == nil || *stored[0].Config.Healthcheck.Enabled {
		t.Fatalf("stored speaker = %#v", stored)
	}

	updated := item
	updated.Name = "renamed"
	updated.DesiredState = "running"
	updated.ConfigVersion = 2
	if err := DBSpeakerUpdate(item.Name, 1, updated); err != nil {
		t.Fatal(err)
	}
	if err := DBSpeakerUpdate(updated.Name, 1, updated); !errors.Is(err, ErrSpeakerConfigVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	stored, err = DBSpeakerList()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Name != "renamed" || stored[0].ConfigVersion != 2 {
		t.Fatalf("updated speaker = %#v", stored)
	}
	if err := DBSpeakerDelete(updated.Name, updated.UUID); err != nil {
		t.Fatal(err)
	}
	stored, err = DBSpeakerList()
	if err != nil || len(stored) != 0 {
		t.Fatalf("speakers after delete = %#v, %v", stored, err)
	}
}
