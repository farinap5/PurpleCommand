package db

import (
	"database/sql"
	"errors"
)

func DBListenerExist(Name string) bool {
	var rowName string
	if err := DBMS.DBConn.QueryRow("SELECT Name FROM Listeners WHERE Name = ?;", Name).Scan(&rowName); err != nil {
		if err == sql.ErrNoRows {
			return false
		}
		return false
	} else {
		return true
	}
}

func DBListenerInsert(Name, UUID, Host, Port string, Persist, Running bool) error {
	if DBListenerExist(Name) {
		return errors.New("listener exists")
	}

	insertQuery := `
	INSERT INTO Listeners (Uuid, Name, Host, Port, Persist, Running) VALUES (?,?,?,?,?,?);
	`
	_, err := DBMS.DBConn.Exec(insertQuery, UUID, Name, Host, Port, Persist, Running)

	return err
}

func DBListenerGetAll() ([]Listener, error) {
	var listeners []Listener

	selectQuery := `SELECT Uuid, Name, Host, Port, Persist, Running FROM Listeners;`
	query, err := DBMS.DBConn.Query(selectQuery)
	if err == nil {
		for query.Next() {
			listenerRow := Listener{}
			err = query.Scan(&listenerRow.UUID ,&listenerRow.Name, &listenerRow.Host, &listenerRow.Port, &listenerRow.Persistent, &listenerRow.Running)
			if err != nil {
				continue
			}
			listeners = append(listeners, listenerRow)
		}
	} else {
		return listeners, err
	}
	defer func(query *sql.Rows) {
		_ = query.Close()
	}(query)

	return listeners, nil
}


func DBListenerUpdateOption(Name, Key, Value string) error {
	var updateQuery string

	switch Key {
	case "uuid":
		updateQuery = `
		UPDATE Listeners SET Uuid = ? WHERE Name = ?;
		`
	case "host":
		updateQuery = `
		UPDATE Listeners SET Host = ? WHERE Name = ?;
		`
	case "port":
		updateQuery = `
		UPDATE Listeners SET Port = ? WHERE Name = ?;
		`
	case "running":
		if Value == "t" || Value == "true" || Value == "on" {
			updateQuery = `
			UPDATE Listeners SET Running = 1 WHERE Name = ?;
			`
		} else if Value == "f" || Value == "false" || Value == "off" {
			updateQuery = `
			UPDATE Listeners SET Running = 0 WHERE Name = ?;
			`
		} else {
			return errors.New("what?")
		}

		_, err := DBMS.DBConn.Exec(updateQuery, Name)
		return err
	}

	_, err := DBMS.DBConn.Exec(updateQuery, Value, Name)
	return err
}