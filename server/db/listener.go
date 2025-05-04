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
	_, err := DBMS.DBConn.Exec(insertQuery, UUID, Name, Port, Persist, Running)

	return err
}

func DBListenerGetAll() ([]Listener, error) {
	var listeners []Listener

	selectQuery := `SELECT Uuid, Name, Host, Port, Persist, Running FROM Listeners;`
	query, err := DBMS.DBConn.Query(selectQuery)
	if err == nil {
		for query.Next() {
			listenerRow := Listener{}
			err = query.Scan(&listenerRow.Name, &listenerRow.Host, &listenerRow.Port, &listenerRow.Persistent, &listenerRow.Running)
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


func DBListenerUpdateOption(Name string, ) error {
	ok = dbms.DbListenerExist(listenerName)
	if !ok {
		return fmt.Errorf("listener %s not exists", listenerName)
	}

	updateQuery := `UPDATE Listeners SET ListenerConfig = ?, CustomData = ? WHERE ListenerName = ?;`
	_, err := dbms.database.Exec(updateQuery, listenerConfig, customData, listenerName)

	return err
}