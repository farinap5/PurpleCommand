package db

import (
	"database/sql"
	"testing"

	"purpcmd/internal"

	_ "github.com/mattn/go-sqlite3"
)

func TestEnsureGenericImplantProfileSchemaMigratesExistingDatabase(t *testing.T) {
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

	if err := definition.ensureGenericImplantProfileSchema(); err != nil {
		t.Fatalf("migrate generic profile schema: %v", err)
	}
	if err := definition.ensureGenericImplantProfileSchema(); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var payloadType, builder string
	if err := connection.QueryRow(`SELECT Type, Builder FROM ImplantProfiles WHERE Name = 'old'`).Scan(&payloadType, &builder); err != nil {
		t.Fatal(err)
	}
	if payloadType != internal.DefaultPayloadType {
		t.Fatalf("migrated payload type = %q", payloadType)
	}
	if builder != "" {
		t.Fatalf("migrated builder = %q", builder)
	}
	columns, err := definition.tableColumns("ImplantProfiles")
	if err != nil {
		t.Fatal(err)
	}
	if columns["uri"] || columns["ua"] {
		t.Fatalf("protocol-specific columns survived migration: %#v", columns)
	}
	var osOptions, archOptions string
	if err := connection.QueryRow(
		`SELECT OSOptionsJSON, ARCHOptionsJSON FROM ImplantProfiles WHERE Name = 'old'`,
	).Scan(&osOptions, &archOptions); err != nil {
		t.Fatal(err)
	}
	if osOptions != `["linux"]` || archOptions != `["amd64"]` {
		t.Fatalf("target suggestions = %s / %s", osOptions, archOptions)
	}
	var options string
	if err := connection.QueryRow(
		`SELECT OptionsJSON FROM ImplantDefinitions WHERE Name = 'old'`,
	).Scan(&options); err != nil {
		t.Fatal(err)
	}
	if options != `{"path":"/","header":{"User-Agent":"ua"}}` {
		t.Fatalf("migrated protocol options = %s", options)
	}
}
