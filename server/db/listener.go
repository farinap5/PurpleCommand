package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func DBListenerExist(name string) bool {
	var rowName string
	return DBMS.DBConn.QueryRow("SELECT Name FROM Listeners WHERE Name = ?;", name).Scan(&rowName) == nil
}

func DBListenerInsert(name, id, host, port string, persist, running bool) error {
	if DBListenerExist(name) {
		return errors.New("listener exists")
	}

	_, err := DBMS.DBConn.Exec(
		`INSERT INTO Listeners (Uuid, Name, Host, Port, Persist, Running) VALUES (?,?,?,?,?,?);`,
		id, name, host, port, persist, running,
	)
	return err
}

func DBListenerGetAll() ([]Listener, error) {
	rows, err := DBMS.DBConn.Query(`SELECT Uuid, Name, Host, Port, Persist, Running FROM Listeners;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var listeners []Listener
	for rows.Next() {
		var listenerRow Listener
		if err := rows.Scan(&listenerRow.UUID, &listenerRow.Name, &listenerRow.Host, &listenerRow.Port, &listenerRow.Persistent, &listenerRow.Running); err != nil {
			return nil, err
		}
		listeners = append(listeners, listenerRow)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return listeners, nil
}

func DBListenerUpdateOption(name, key, value string) error {
	var (
		query string
		args  []any
	)

	switch strings.ToLower(key) {
	case "uuid":
		query = `UPDATE Listeners SET Uuid = ? WHERE Name = ?;`
		args = []any{value, name}
	case "host":
		query = `UPDATE Listeners SET Host = ? WHERE Name = ?;`
		args = []any{value, name}
	case "port":
		query = `UPDATE Listeners SET Port = ? WHERE Name = ?;`
		args = []any{value, name}
	case "persist":
		persistent, err := parseDBBool(value)
		if err != nil {
			return err
		}
		query = `UPDATE Listeners SET Persist = ? WHERE Name = ?;`
		args = []any{persistent, name}
	case "running":
		running, err := parseDBBool(value)
		if err != nil {
			return err
		}
		query = `UPDATE Listeners SET Running = ? WHERE Name = ?;`
		args = []any{running, name}
	default:
		return fmt.Errorf("unknown listener option %q", key)
	}

	result, err := DBMS.DBConn.Exec(query, args...)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return errors.New("listener does not exist")
	}
	return nil
}

func parseDBBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "t", "true", "on":
		return true, nil
	case "f", "false", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}

func DBListenerDelete(name string) error {
	result, err := DBMS.DBConn.Exec(`DELETE FROM Listeners WHERE Name = ?;`, name)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return sql.ErrNoRows
	}
	return nil
}
