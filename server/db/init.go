package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"purpcmd/internal"
	"purpcmd/server/log"

	_ "github.com/mattn/go-sqlite3"
)

var DBMS DBDef

// DatabasePath may be set before CheckDB by the teamserver entrypoint.
var DatabasePath = "database.db"

func CheckDB() error {
	dbms, err := DBInit()
	if err != nil {
		return err
	}

	DBMS = *dbms
	return DBMS.dbCreateDs()
}

func DBInit() (*DBDef, error) {
	fname := DatabasePath
	if _, err := os.Stat(fname); os.IsNotExist(err) {
		log.PrintInfo("Creating database")
	}
	file, err := os.OpenFile(fname, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}

	db := new(DBDef)
	db.DBConn, err = sql.Open("sqlite3", fname)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DBDef) dbCreateDs() error {
	sttm, err := db.DBConn.Prepare(`
	CREATE TABLE IF NOT EXISTS Listeners (
		Lid		INTEGER PRIMARY KEY AUTOINCREMENT,
		Uuid	TEXT NOT NULL UNIQUE,
		Name	TEXT NOT NULL UNIQUE,

		Host 	TEXT NOT NULL,
		Port 	TEXT NOT NULL,

		Persist	BOOLEAN NOT NULL,
		Running	BOOLEAN NOT NULL,
		Driver TEXT NOT NULL DEFAULT 'http',
		OptionsJSON TEXT NOT NULL DEFAULT '{}',
		RoutesJSON TEXT NOT NULL DEFAULT '[]',
		DesiredState TEXT NOT NULL DEFAULT 'stopped',
		ConfigVersion INTEGER NOT NULL DEFAULT 1,
		CreatedAt TEXT NOT NULL DEFAULT '',
		UpdatedAt TEXT NOT NULL DEFAULT ''
	);
	`)
	if err != nil {
		return err
	} else {
		sttm.Exec()
	}

	sttm, err = db.DBConn.Prepare(`
	CREATE TABLE IF NOT EXISTS Scripts (
		Sid		INTEGER PRIMARY KEY AUTOINCREMENT,
		Path	TEXT NOT NULL UNIQUE
	);
	`)
	if err != nil {
		return err
	} else {
		sttm.Exec()
	}

	sttm, err = db.DBConn.Prepare(`
	CREATE TABLE IF NOT EXISTS Loot (
		lid		INTEGER PRIMARY KEY AUTOINCREMENT,
		Uuid	TEXT NOT NULL UNIQUE,
		Session TEXT NOT NULL,
		FileName TEXT NOT NULL
	);
	`)
	if err != nil {
		return err
	} else {
		sttm.Exec()
	}

	sttm, err = db.DBConn.Prepare(`
	CREATE TABLE IF NOT EXISTS ImplantProfiles (
		Pid			INTEGER PRIMARY KEY AUTOINCREMENT,
		Name		TEXT NOT NULL UNIQUE,
		Type		TEXT NOT NULL DEFAULT 'impl',
		Mode        TEXT NOT NULL DEFAULT 'reverse',
		LHOST		TEXT NOT NULL,
		OS			TEXT NOT NULL,
		ARCH		TEXT NOT NULL,
		OSOptionsJSON	TEXT NOT NULL DEFAULT '["linux"]',
		ARCHOptionsJSON	TEXT NOT NULL DEFAULT '["amd64"]',
		Output		TEXT NOT NULL,
		Template	TEXT NOT NULL,
		PublicKey	TEXT NOT NULL,
		Builder		TEXT NOT NULL DEFAULT '',
		ListenerUUID	TEXT NOT NULL DEFAULT ''
	);
	`)
	if err != nil {
		return err
	} else {
		sttm.Exec()
	}

	if err := db.ensureGenericListenerSchema(); err != nil {
		return err
	}
	return db.ensureGenericImplantProfileSchema()
}

func (db *DBDef) ensureGenericListenerSchema() error {
	columns, err := db.tableColumns("Listeners")
	if err != nil {
		return err
	}
	additions := []struct {
		name string
		sql  string
	}{
		{name: "driver", sql: `ALTER TABLE Listeners ADD COLUMN Driver TEXT NOT NULL DEFAULT 'http';`},
		{name: "optionsjson", sql: `ALTER TABLE Listeners ADD COLUMN OptionsJSON TEXT NOT NULL DEFAULT '{}';`},
		{name: "routesjson", sql: `ALTER TABLE Listeners ADD COLUMN RoutesJSON TEXT NOT NULL DEFAULT '[]';`},
		{name: "desiredstate", sql: `ALTER TABLE Listeners ADD COLUMN DesiredState TEXT NOT NULL DEFAULT 'stopped';`},
		{name: "configversion", sql: `ALTER TABLE Listeners ADD COLUMN ConfigVersion INTEGER NOT NULL DEFAULT 1;`},
		{name: "createdat", sql: `ALTER TABLE Listeners ADD COLUMN CreatedAt TEXT NOT NULL DEFAULT '';`},
		{name: "updatedat", sql: `ALTER TABLE Listeners ADD COLUMN UpdatedAt TEXT NOT NULL DEFAULT '';`},
	}
	for _, addition := range additions {
		if columns[addition.name] {
			continue
		}
		if _, err := db.DBConn.Exec(addition.sql); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.DBConn.Exec(`
UPDATE Listeners
SET Driver = 'http'
WHERE trim(Driver) = '';
UPDATE Listeners
SET OptionsJSON = json_object(
    'bind', json_object('host', Host, 'port', Port),
    'advertise', json_object('host', Host, 'port', Port)
)
WHERE OptionsJSON = '{}' OR NOT json_valid(OptionsJSON) OR json_type(OptionsJSON) != 'object';
UPDATE Listeners
SET RoutesJSON = '[]'
WHERE NOT json_valid(RoutesJSON) OR json_type(RoutesJSON) != 'array';
UPDATE Listeners
SET DesiredState = CASE WHEN Running THEN 'running' ELSE 'stopped' END
WHERE DesiredState NOT IN ('running', 'stopped')
   OR (DesiredState = 'stopped' AND Running = 1);
UPDATE Listeners
SET ConfigVersion = 1
WHERE ConfigVersion < 1;
UPDATE Listeners
SET CreatedAt = ?
WHERE CreatedAt = '';
UPDATE Listeners
SET UpdatedAt = CreatedAt
WHERE UpdatedAt = '';
`, now); err != nil {
		return err
	}
	_, err = db.DBConn.Exec(`CREATE INDEX IF NOT EXISTS idx_listeners_driver ON Listeners(Driver);`)
	return err
}

func (db *DBDef) ensureGenericImplantProfileSchema() error {
	if err := db.ensureImplantProfileTypeColumn(); err != nil {
		return err
	}
	if err := db.ensureImplantProfileModeColumn(); err != nil {
		return err
	}
	if err := db.ensureImplantProfileTargetOptionColumns(); err != nil {
		return err
	}
	if err := db.ensureImplantProfileBuilderColumn(); err != nil {
		return err
	}
	if err := db.ensureImplantProfileListenerColumn(); err != nil {
		return err
	}
	if err := db.ensureImplantDefinitionsTable(); err != nil {
		return err
	}
	if err := db.ensureImplantDefinitionTargetColumns(); err != nil {
		return err
	}
	return db.migrateImplantProfileProtocolColumns()
}

func (db *DBDef) ensureImplantProfileModeColumn() error {
	columns, err := db.tableColumns("ImplantProfiles")
	if err != nil {
		return err
	}
	if columns["mode"] {
		return nil
	}
	_, err = db.DBConn.Exec(`ALTER TABLE ImplantProfiles ADD COLUMN Mode TEXT NOT NULL DEFAULT 'reverse';`)
	return err
}

func (db *DBDef) ensureImplantProfileListenerColumn() error {
	columns, err := db.tableColumns("ImplantProfiles")
	if err != nil {
		return err
	}
	if columns["listeneruuid"] {
		return nil
	}
	_, err = db.DBConn.Exec(`ALTER TABLE ImplantProfiles ADD COLUMN ListenerUUID TEXT NOT NULL DEFAULT '';`)
	return err
}

func (db *DBDef) ensureImplantProfileBuilderColumn() error {
	columns, err := db.tableColumns("ImplantProfiles")
	if err != nil {
		return err
	}
	if columns["builder"] {
		return nil
	}
	_, err = db.DBConn.Exec(`ALTER TABLE ImplantProfiles ADD COLUMN Builder TEXT NOT NULL DEFAULT '';`)
	return err
}

// ensureImplantProfileTypeColumn migrates databases created before payload
// types were persisted on implant build profiles.
func (db *DBDef) ensureImplantProfileTypeColumn() error {
	columns, err := db.tableColumns("ImplantProfiles")
	if err != nil {
		return err
	}
	if columns["type"] {
		return nil
	}
	_, err = db.DBConn.Exec(
		`ALTER TABLE ImplantProfiles ADD COLUMN Type TEXT NOT NULL DEFAULT '` + internal.DefaultPayloadType + `'`,
	)
	return err
}

func (db *DBDef) ensureImplantProfileTargetOptionColumns() error {
	columns, err := db.tableColumns("ImplantProfiles")
	if err != nil {
		return err
	}
	if !columns["osoptionsjson"] {
		if _, err := db.DBConn.Exec(`ALTER TABLE ImplantProfiles ADD COLUMN OSOptionsJSON TEXT NOT NULL DEFAULT '[]';`); err != nil {
			return err
		}
	}
	if !columns["archoptionsjson"] {
		if _, err := db.DBConn.Exec(`ALTER TABLE ImplantProfiles ADD COLUMN ARCHOptionsJSON TEXT NOT NULL DEFAULT '[]';`); err != nil {
			return err
		}
	}
	_, err = db.DBConn.Exec(`
UPDATE ImplantProfiles
SET OSOptionsJSON = json_array(OS)
WHERE OSOptionsJSON = '[]';
UPDATE ImplantProfiles
SET ARCHOptionsJSON = json_array(ARCH)
WHERE ARCHOptionsJSON = '[]';
`)
	return err
}

func (db *DBDef) migrateImplantProfileProtocolColumns() error {
	columns, err := db.tableColumns("ImplantProfiles")
	if err != nil {
		return err
	}
	hasURI := columns["uri"]
	hasUA := columns["ua"]
	if !hasURI && !hasUA {
		return nil
	}
	pathExpression := "'/'"
	if hasURI {
		pathExpression = "URI"
	}
	userAgentExpression := "''"
	if hasUA {
		userAgentExpression = "UA"
	}
	nowExpression := "strftime('%Y-%m-%dT%H:%M:%fZ', 'now')"
	query := fmt.Sprintf(`
INSERT INTO ImplantDefinitions
    (Name, Protocol, PayloadType, OperatingSystemsJSON, ArchitecturesJSON,
     OptionsJSON, ConfigVersion, CreatedAt, UpdatedAt)
SELECT Name, 'http', Type, OSOptionsJSON, ARCHOptionsJSON,
       json_object('path', %s, 'header', json_object('User-Agent', %s)),
       1, %s, %s
FROM ImplantProfiles
WHERE 1
ON CONFLICT(Name) DO NOTHING;
`, pathExpression, userAgentExpression, nowExpression, nowExpression)
	if _, err := db.DBConn.Exec(query); err != nil {
		return err
	}
	for _, column := range []string{"URI", "UA"} {
		if !columns[strings.ToLower(column)] {
			continue
		}
		if _, err := db.DBConn.Exec(`ALTER TABLE ImplantProfiles DROP COLUMN ` + column + `;`); err != nil {
			return fmt.Errorf("drop protocol-specific ImplantProfiles.%s: %w", column, err)
		}
	}
	return nil
}

func (db *DBDef) tableColumns(table string) (map[string]bool, error) {
	rows, err := db.DBConn.Query(`PRAGMA table_info(` + table + `);`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}
