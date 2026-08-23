package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"purpcmd/pkg/teamapi"
)

type PersistedSession struct {
	Session  teamapi.Session
	Metadata json.RawMessage
}

type PersistedTask struct {
	Task    teamapi.Task
	Payload []byte
}

func DBSessionSave(session teamapi.Session, metadata json.RawMessage) error {
	if DBMS.DBConn == nil {
		return nil
	}
	transport := session.Transport
	if transport == "" {
		transport = teamapi.SessionTransportListener
	}
	_, err := DBMS.DBConn.Exec(
		`INSERT INTO Sessions (Name, Uuid, PayloadType, Transport, Speaker, Metadata, Alive, Terminating, FirstSeen, LastSeen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(Name) DO UPDATE SET Uuid=excluded.Uuid, PayloadType=excluded.PayloadType,
		 Transport=excluded.Transport, Speaker=excluded.Speaker, Metadata=excluded.Metadata,
		 Alive=excluded.Alive, Terminating=excluded.Terminating,
		 FirstSeen=excluded.FirstSeen, LastSeen=excluded.LastSeen;`,
		session.Name, session.UUID, session.PayloadType, transport, session.Speaker, []byte(metadata), session.Alive,
		session.Terminating, formatDBTime(session.FirstSeen), formatDBTime(session.LastSeen),
	)
	return err
}

func DBSessionDelete(name string) error {
	if DBMS.DBConn == nil {
		return nil
	}
	transaction, err := DBMS.DBConn.Begin()
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(`DELETE FROM Tasks WHERE SessionName = ?;`, name); err != nil {
		_ = transaction.Rollback()
		return err
	}
	if _, err := transaction.Exec(`DELETE FROM Sessions WHERE Name = ?;`, name); err != nil {
		_ = transaction.Rollback()
		return err
	}
	return transaction.Commit()
}

func DBSessionList() ([]PersistedSession, error) {
	if DBMS.DBConn == nil {
		return nil, nil
	}
	rows, err := DBMS.DBConn.Query(
		`SELECT Name, Uuid, PayloadType, Transport, Speaker, Metadata, Alive, Terminating, FirstSeen, LastSeen FROM Sessions;`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PersistedSession
	for rows.Next() {
		var item PersistedSession
		var metadata []byte
		var firstSeen, lastSeen string
		if err := rows.Scan(
			&item.Session.Name, &item.Session.UUID, &item.Session.PayloadType,
			&item.Session.Transport, &item.Session.Speaker, &metadata,
			&item.Session.Alive, &item.Session.Terminating, &firstSeen, &lastSeen,
		); err != nil {
			return nil, err
		}
		item.Session.FirstSeen, err = time.Parse(time.RFC3339Nano, firstSeen)
		if err != nil {
			return nil, err
		}
		item.Session.LastSeen, err = time.Parse(time.RFC3339Nano, lastSeen)
		if err != nil {
			return nil, err
		}
		item.Metadata = append(json.RawMessage(nil), metadata...)
		result = append(result, item)
	}
	return result, rows.Err()
}

func DBTaskSave(task teamapi.Task, payload []byte) error {
	if DBMS.DBConn == nil {
		return nil
	}
	var lastSent any
	if !task.LastSent.IsZero() {
		lastSent = formatDBTime(task.LastSent)
	}
	var responseTime any
	if !task.ResponseTime.IsZero() {
		responseTime = formatDBTime(task.ResponseTime)
	}
	_, err := DBMS.DBConn.Exec(
		`INSERT INTO Tasks (TaskID, SessionName, Code, Payload, Status, Attempts, Registered, LastSent, ResponseTime, Response)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(SessionName, TaskID) DO UPDATE SET Status=excluded.Status, Attempts=excluded.Attempts,
		 LastSent=excluded.LastSent, ResponseTime=excluded.ResponseTime, Response=excluded.Response;`,
		[]byte(task.ID), task.Session, task.Code, payload, task.Status, task.Attempts,
		formatDBTime(task.Registered), lastSent, responseTime, task.Response,
	)
	return err
}

func DBTaskList(session string) ([]PersistedTask, error) {
	if DBMS.DBConn == nil {
		return nil, nil
	}
	rows, err := DBMS.DBConn.Query(
		`SELECT TaskID, Code, Payload, Status, Attempts, Registered, LastSent, ResponseTime, Response
		 FROM Tasks WHERE SessionName = ? ORDER BY Registered;`, session,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PersistedTask
	for rows.Next() {
		var item PersistedTask
		var id []byte
		var registered string
		var lastSent, responseTime sql.NullString
		if err := rows.Scan(&id, &item.Task.Code, &item.Payload, &item.Task.Status,
			&item.Task.Attempts, &registered, &lastSent, &responseTime, &item.Task.Response); err != nil {
			return nil, err
		}
		if len(id) != 8 {
			return nil, errors.New("persisted task ID is not 8 bytes")
		}
		item.Task.ID = string(id)
		item.Task.Session = session
		item.Task.Registered, err = time.Parse(time.RFC3339Nano, registered)
		if err != nil {
			return nil, err
		}
		if lastSent.Valid {
			item.Task.LastSent, err = time.Parse(time.RFC3339Nano, lastSent.String)
			if err != nil {
				return nil, err
			}
		}
		if responseTime.Valid {
			item.Task.ResponseTime, err = time.Parse(time.RFC3339Nano, responseTime.String)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
