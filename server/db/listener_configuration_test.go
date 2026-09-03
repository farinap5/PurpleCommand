package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func listenerConfigurationTestDatabase(t *testing.T, legacy bool) {
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
	if legacy {
		if _, err := connection.Exec(`
CREATE TABLE Listeners (
    Lid INTEGER PRIMARY KEY AUTOINCREMENT,
    Uuid TEXT NOT NULL UNIQUE,
    Name TEXT NOT NULL UNIQUE,
    Host TEXT NOT NULL,
    Port TEXT NOT NULL,
    Persist BOOLEAN NOT NULL,
    Running BOOLEAN NOT NULL
);
INSERT INTO Listeners (Uuid, Name, Host, Port, Persist, Running)
VALUES ('legacy-id', 'legacy', '127.0.0.1', '8443', 1, 1);`); err != nil {
			t.Fatal(err)
		}
	}
	if err := DBMS.dbCreateDs(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyListenerConfigurationMigrationIsIdempotent(t *testing.T) {
	listenerConfigurationTestDatabase(t, true)
	if err := DBMS.ensureGenericListenerSchema(); err != nil {
		t.Fatal(err)
	}
	configuration, err := DBListenerConfigGet("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Driver != "http" || configuration.DesiredState != "running" ||
		configuration.ConfigVersion != 1 || configuration.CreatedAt.IsZero() || configuration.UpdatedAt.IsZero() {
		t.Fatalf("migrated configuration = %#v", configuration)
	}
	if !strings.Contains(configuration.OptionsJSON, `"host":"127.0.0.1"`) ||
		!strings.Contains(configuration.OptionsJSON, `"port":"8443"`) {
		t.Fatalf("migrated options = %s", configuration.OptionsJSON)
	}
}

func TestListenerConfigurationLifecycleAndOptimisticUpdate(t *testing.T) {
	listenerConfigurationTestDatabase(t, false)
	created, err := DBListenerConfigInsert(ListenerConfiguration{
		Name: "alpha", UUID: "alpha-id", Driver: "http", Persistent: true,
		OptionsJSON: `{"bind":{"host":"127.0.0.1","port":8080}}`,
		RoutesJSON:  `[{"id":"callback"}]`, DesiredState: "stopped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ConfigVersion != 1 {
		t.Fatalf("created version = %d", created.ConfigVersion)
	}
	created.OptionsJSON = `{"bind":{"host":"0.0.0.0","port":"4444"}}`
	updated, err := DBListenerConfigUpdate(created)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ConfigVersion != 2 {
		t.Fatalf("updated version = %d", updated.ConfigVersion)
	}
	if _, err := DBListenerConfigUpdate(created); !errors.Is(err, ErrListenerConfigConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	var host, port string
	if err := DBMS.DBConn.QueryRow(`SELECT Host, Port FROM Listeners WHERE Name = 'alpha'`).Scan(&host, &port); err != nil {
		t.Fatal(err)
	}
	if host != "0.0.0.0" || port != "4444" {
		t.Fatalf("legacy projection = %s:%s", host, port)
	}
	if err := DBListenerConfigDelete("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := DBListenerConfigGet("alpha"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("get deleted error = %v", err)
	}
}

func TestListenerConfigurationRejectsWrongJSONKinds(t *testing.T) {
	listenerConfigurationTestDatabase(t, false)
	if _, err := DBListenerConfigInsert(ListenerConfiguration{
		Name: "bad-options", UUID: "bad-options-id", Driver: "http", OptionsJSON: `[]`, RoutesJSON: `[]`,
	}); err == nil {
		t.Fatal("array listener options were accepted")
	}
	if _, err := DBListenerConfigInsert(ListenerConfiguration{
		Name: "bad-routes", UUID: "bad-routes-id", Driver: "http", OptionsJSON: `{}`, RoutesJSON: `{}`,
	}); err == nil {
		t.Fatal("object listener routes were accepted")
	}
}
