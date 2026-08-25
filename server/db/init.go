package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

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
		Running	BOOLEAN NOT NULL
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
		LHOST		TEXT NOT NULL,
		OS			TEXT NOT NULL,
		ARCH		TEXT NOT NULL,
		OSOptionsJSON	TEXT NOT NULL DEFAULT '["linux"]',
		ARCHOptionsJSON	TEXT NOT NULL DEFAULT '["amd64"]',
		Output		TEXT NOT NULL,
		Template	TEXT NOT NULL,
		PublicKey	TEXT NOT NULL
	);
	`)
	if err != nil {
		return err
	} else {
		sttm.Exec()
	}

	return db.ensureGenericImplantProfileSchema()
}

func (db *DBDef) ensureGenericImplantProfileSchema() error {
	if err := db.ensureImplantProfileTypeColumn(); err != nil {
		return err
	}
	if err := db.ensureImplantProfileTargetOptionColumns(); err != nil {
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
