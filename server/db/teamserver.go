package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"purpcmd/pkg/teamapi"
)

func EnsureTeamserverSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS Events (
			Sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			Type TEXT NOT NULL,
			CreatedAt TEXT NOT NULL,
			Data BLOB NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS RequestDeduplication (
			ClientID TEXT NOT NULL,
			RequestID TEXT NOT NULL,
			Operation TEXT NOT NULL,
			Response BLOB NOT NULL,
			CreatedAt TEXT NOT NULL,
			PRIMARY KEY (ClientID, RequestID)
		);`,
		`CREATE TABLE IF NOT EXISTS Sessions (
			Name TEXT PRIMARY KEY,
			Uuid TEXT NOT NULL,
			PayloadType TEXT NOT NULL,
			Metadata BLOB NOT NULL,
			Alive BOOLEAN NOT NULL,
			Terminating BOOLEAN NOT NULL,
			FirstSeen TEXT NOT NULL,
			LastSeen TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS Tasks (
			TaskID BLOB NOT NULL,
			SessionName TEXT NOT NULL,
			Code INTEGER NOT NULL,
			Payload BLOB NOT NULL,
			Status TEXT NOT NULL,
			Attempts INTEGER NOT NULL,
			Registered TEXT NOT NULL,
			LastSent TEXT,
			ResponseTime TEXT,
			Response BLOB,
			PRIMARY KEY (SessionName, TaskID)
		);`,
		`CREATE TABLE IF NOT EXISTS BuildJobs (
			ID TEXT PRIMARY KEY,
			Profile TEXT NOT NULL,
			Status TEXT NOT NULL,
			ArtifactName TEXT,
			Error TEXT,
			CreatedAt TEXT NOT NULL,
			CompletedAt TEXT
		);`,
	}
	for _, statement := range statements {
		if _, err := DBMS.DBConn.Exec(statement); err != nil {
			return err
		}
	}
	_, err := DBMS.DBConn.Exec(`PRAGMA journal_mode=WAL;`)
	return err
}

func DBEventInsert(eventType string, data json.RawMessage, createdAt time.Time) (teamapi.EventRecord, error) {
	result, err := DBMS.DBConn.Exec(
		`INSERT INTO Events (Type, CreatedAt, Data) VALUES (?, ?, ?);`,
		eventType, createdAt.UTC().Format(time.RFC3339Nano), []byte(data),
	)
	if err != nil {
		return teamapi.EventRecord{}, err
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return teamapi.EventRecord{}, err
	}
	return teamapi.EventRecord{
		Sequence: uint64(sequence),
		Type:     eventType,
		Time:     createdAt.UTC(),
		Data:     append(json.RawMessage(nil), data...),
	}, nil
}

func DBEventList(after uint64, limit int) ([]teamapi.EventRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := DBMS.DBConn.Query(
		`SELECT Sequence, Type, CreatedAt, Data FROM Events WHERE Sequence > ? ORDER BY Sequence LIMIT ?;`,
		after, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]teamapi.EventRecord, 0)
	for rows.Next() {
		var event teamapi.EventRecord
		var createdAt string
		var data []byte
		if err := rows.Scan(&event.Sequence, &event.Type, &createdAt, &data); err != nil {
			return nil, err
		}
		event.Time, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		event.Data = append(json.RawMessage(nil), data...)
		events = append(events, event)
	}
	return events, rows.Err()
}

func DBEventLatestSequence() (uint64, error) {
	var sequence sql.NullInt64
	if err := DBMS.DBConn.QueryRow(`SELECT MAX(Sequence) FROM Events;`).Scan(&sequence); err != nil {
		return 0, err
	}
	if !sequence.Valid {
		return 0, nil
	}
	return uint64(sequence.Int64), nil
}

func DBRequestGet(clientID, requestID string) ([]byte, bool, error) {
	var response []byte
	err := DBMS.DBConn.QueryRow(
		`SELECT Response FROM RequestDeduplication WHERE ClientID = ? AND RequestID = ?;`,
		clientID, requestID,
	).Scan(&response)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return response, err == nil, err
}

func DBRequestSave(clientID, requestID, operation string, response []byte) error {
	_, err := DBMS.DBConn.Exec(
		`INSERT OR IGNORE INTO RequestDeduplication (ClientID, RequestID, Operation, Response, CreatedAt) VALUES (?, ?, ?, ?, ?);`,
		clientID, requestID, operation, response, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}
