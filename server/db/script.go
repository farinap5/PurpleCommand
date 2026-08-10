package db

import (
	"database/sql"
	"errors"
)

func DBScriptExist(Path string) bool {
	var rowName string
	if err := DBMS.DBConn.QueryRow("SELECT Path FROM Scripts WHERE Path = ?;", Path).Scan(&rowName); err != nil {
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

func DBScriptDelete(Path string) error {
	if !DBScriptExist(Path) {
		return errors.New("script does not exist")
	}

	delQuery := `
	DELETE FROM Scripts WHERE Path = ?;
	`
	_, err := DBMS.DBConn.Exec(delQuery, Path)
	return err
}

// DBScriptReplacePath atomically migrates a persisted script path. If the new
// path is already present, the obsolete row is removed instead of creating a
// duplicate.
func DBScriptReplacePath(oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}
	tx, err := DBMS.DBConn.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRow("SELECT 1 FROM Scripts WHERE Path = ?;", newPath).Scan(&exists)
	switch {
	case err == nil:
		_, err = tx.Exec("DELETE FROM Scripts WHERE Path = ?;", oldPath)
	case errors.Is(err, sql.ErrNoRows):
		var result sql.Result
		result, err = tx.Exec("UPDATE Scripts SET Path = ? WHERE Path = ?;", newPath, oldPath)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected != 1 {
				err = errors.New("script path does not exist")
			}
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
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
