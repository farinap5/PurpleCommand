package listener

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"purpcmd/server/db"
)

func TestDBStoreRestoresDesiredRuntimeAndPreservesItOnShutdown(t *testing.T) {
	previousPath := db.DatabasePath
	previousDB := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "listeners.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DatabasePath = previousPath
		db.DBMS = previousDB
	})

	registry := NewRegistry()
	driver := &managerTestDriver{}
	if err := registry.RegisterDriver(driver); err != nil {
		t.Fatal(err)
	}
	manager := NewManagerWithStore(registry, nil, nil, DBStore{})
	created, err := manager.Create(ManagedListenerConfig{
		Name: "persistent", UUID: "persistent-id", Driver: "test", Persistent: true,
		Options: json.RawMessage(`{"value":"one","hosted_files":{"/":{"source_path":"site/index.html"}},"not_found_page":{"source_path":"site/404.html"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Config.Options = json.RawMessage(`{"value":"two","hosted_files":{"/":{"source_path":"site/index.html"}},"not_found_page":{"source_path":"site/404.html"}}`)
	updated, err := manager.Update(created.Config, created.Config.ConfigVersion)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config.ConfigVersion <= created.Config.ConfigVersion {
		t.Fatalf("version did not advance: created=%d updated=%d", created.Config.ConfigVersion, updated.Config.ConfigVersion)
	}
	started, err := manager.Start("persistent")
	if err != nil {
		t.Fatal(err)
	}
	if started.Config.DesiredState != StateRunning {
		t.Fatalf("started desired state = %q", started.Config.DesiredState)
	}
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := db.DBListenerConfigGet("persistent")
	if err != nil {
		t.Fatal(err)
	}
	if stored.DesiredState != string(StateRunning) {
		t.Fatalf("shutdown overwrote desired state: %#v", stored)
	}

	restored := NewManagerWithStore(registry, nil, nil, DBStore{})
	if err := restored.Restore(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := restored.Get("persistent")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status.State != StateRunning || snapshot.Config.DesiredState != StateRunning {
		t.Fatalf("restored listener = %#v", snapshot)
	}
	var restoredOptions struct {
		HostedFiles  map[string]json.RawMessage `json:"hosted_files"`
		NotFoundPage json.RawMessage            `json:"not_found_page"`
	}
	if err := json.Unmarshal(snapshot.Config.Options, &restoredOptions); err != nil {
		t.Fatal(err)
	}
	if len(restoredOptions.HostedFiles) != 1 || len(restoredOptions.NotFoundPage) == 0 {
		t.Fatalf("restored hosted-file options = %s", snapshot.Config.Options)
	}
	if _, err := restored.Stop("persistent"); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Delete("persistent"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DBListenerConfigGet("persistent"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted listener lookup error = %v", err)
	}
}

func TestDBStoreRestorePurgesStaleNonPersistentRows(t *testing.T) {
	previousPath := db.DatabasePath
	previousDB := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "stale-listeners.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DatabasePath = previousPath
		db.DBMS = previousDB
	})

	if _, err := db.DBListenerConfigInsert(db.ListenerConfiguration{
		Name: "stale", UUID: "stale-id", Driver: "test", Persistent: false,
		OptionsJSON: `{}`, RoutesJSON: `[]`, DesiredState: string(StateRunning),
	}); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.RegisterDriver(&managerTestDriver{}); err != nil {
		t.Fatal(err)
	}
	manager := NewManagerWithStore(registry, nil, nil, DBStore{})
	if err := manager.Restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Get("stale"); !errors.Is(err, ErrManagedListenerNotFound) {
		t.Fatalf("stale listener restore error = %v", err)
	}
	if _, err := db.DBListenerConfigGet("stale"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale listener database lookup error = %v", err)
	}
}
