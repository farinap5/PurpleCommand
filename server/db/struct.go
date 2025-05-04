package db

import "database/sql"

type DBDef struct {
	DBConn *sql.DB
}

type Listener struct {
	Name	string
	UUID	string
	Host 	string
	Port 	string

	Proto 		string
	Persistent 	bool
	Running 	bool
}