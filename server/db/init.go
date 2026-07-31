package db

import (
	"database/sql"
	"os"
	"strings"

	"purpcmd/internal"
	"purpcmd/server/log"

	_ "github.com/mattn/go-sqlite3"
)

var DBMS DBDef

func CheckDB() error {
	dbms, err := DBInit()
	if err != nil {
		return err
	}

	DBMS = *dbms
	return DBMS.dbCreateDs()
}

func DBInit() (*DBDef, error) {
	fname := "database.db"
	_, err := os.Open(fname)
	if err != nil {
		//utils.LogMsg(homeDir+"/.venera/message.log", 0, "core", "Creating database")
		log.PrintInfo("Creating database")
		_, err := os.Create(fname)
		if err != nil {
			return nil, err
		}
	}

	// Create db definition
	db := new(DBDef)
	//utils.LogMsg(homeDir+"/.venera/message.log", 0, "core", "Open database.")
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
		URI			TEXT NOT NULL,
		UA			TEXT NOT NULL,
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

	return db.ensureImplantProfileTypeColumn()
}

// ensureImplantProfileTypeColumn migrates databases created before payload
// types were persisted on implant build profiles.
func (db *DBDef) ensureImplantProfileTypeColumn() error {
	rows, err := db.DBConn.Query(`PRAGMA table_info(ImplantProfiles);`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if strings.EqualFold(name, "Type") {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.DBConn.Exec(
		`ALTER TABLE ImplantProfiles ADD COLUMN Type TEXT NOT NULL DEFAULT '` + internal.DefaultPayloadType + `'`,
	)
	return err
}
