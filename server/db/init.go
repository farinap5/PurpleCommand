package db

import (
	"database/sql"
	"os"
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
	DBMS.dbCreateDs()
	return nil
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

	return nil
}