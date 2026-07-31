package db

import (
	"database/sql"
	"testing"

	"purpcmd/internal"

	_ "github.com/mattn/go-sqlite3"
)

func TestEnsureImplantProfileTypeColumnMigratesExistingDatabase(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	definition := &DBDef{DBConn: connection}
	if _, err := connection.Exec(`
CREATE TABLE ImplantProfiles (
    Pid INTEGER PRIMARY KEY AUTOINCREMENT,
    Name TEXT NOT NULL UNIQUE,
    LHOST TEXT NOT NULL,
    OS TEXT NOT NULL,
    ARCH TEXT NOT NULL,
    URI TEXT NOT NULL,
    UA TEXT NOT NULL,
    Output TEXT NOT NULL,
    Template TEXT NOT NULL,
    PublicKey TEXT NOT NULL
);
INSERT INTO ImplantProfiles (Name, LHOST, OS, ARCH, URI, UA, Output, Template, PublicKey)
VALUES ('old', '127.0.0.1:1', 'linux', 'amd64', '/', 'ua', 'out', './template', 'key');
`); err != nil {
		t.Fatal(err)
	}

	if err := definition.ensureImplantProfileTypeColumn(); err != nil {
		t.Fatalf("migrate type column: %v", err)
	}
	if err := definition.ensureImplantProfileTypeColumn(); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var payloadType string
	if err := connection.QueryRow(`SELECT Type FROM ImplantProfiles WHERE Name = 'old'`).Scan(&payloadType); err != nil {
		t.Fatal(err)
	}
	if payloadType != internal.DefaultPayloadType {
		t.Fatalf("migrated payload type = %q", payloadType)
	}
}
