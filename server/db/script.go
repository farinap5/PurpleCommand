package db

import (
	"database/sql"
	"errors"
)

func DBScriptExist(Path string) bool {
	var rowName string
	if err := DBMS.DBConn.QueryRow("SELECT Path FROM Scripts WHERE Name = ?;", Path).Scan(&rowName); err != nil {
		if err == sql.ErrNoRows {
			return false
		}
		return false
	} else {
		return true
	}
}

func DBScriptInsert(Path string) error {
	if DBScriptExist(Path) {
		return errors.New("script exists")
	}

	insertQuery := `
	INSERT INTO Scripts (Path) VALUES (?);
	`
	_, err := DBMS.DBConn.Exec(insertQuery, Path)
	return err
}

func DBScriptGetAll() ([]string, error) {
	var ScriptsPath []string

	selectQuery := `SELECT Path FROM Scripts;`
	query, err := DBMS.DBConn.Query(selectQuery)
	if err == nil {
		for query.Next() {
			var scriptPath string
			err = query.Scan(&scriptPath)
			if err != nil {
				continue
			}
			ScriptsPath = append(ScriptsPath, scriptPath)
		}
	} else {
		return ScriptsPath, err
	}
	defer func(query *sql.Rows) {
		_ = query.Close()
	}(query)

	return ScriptsPath, nil
}