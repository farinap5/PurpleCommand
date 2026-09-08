package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
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
		`CREATE TABLE IF NOT EXISTS EventRetentionConfiguration (
			EventType TEXT PRIMARY KEY,
			RetentionTier TEXT NOT NULL,
			RetentionSeconds INTEGER NOT NULL CHECK (RetentionSeconds >= 0),
			UpdatedAt TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS Sessions (
			Name TEXT PRIMARY KEY,
			Uuid TEXT NOT NULL,
			PayloadType TEXT NOT NULL,
			Transport TEXT NOT NULL DEFAULT 'listener',
			Speaker TEXT NOT NULL DEFAULT '',
			SpeakerUuid TEXT NOT NULL DEFAULT '',
			HealthMonitoring BOOLEAN NOT NULL DEFAULT TRUE,
			Listener TEXT NOT NULL DEFAULT '',
			ListenerUuid TEXT NOT NULL DEFAULT '',
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
			Builder TEXT NOT NULL DEFAULT '',
			Status TEXT NOT NULL,
			ArtifactName TEXT,
			Error TEXT,
			CreatedAt TEXT NOT NULL,
			CompletedAt TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS Speakers (
			Name TEXT PRIMARY KEY,
			Uuid TEXT NOT NULL UNIQUE,
			Config BLOB NOT NULL,
			Persistent BOOLEAN NOT NULL,
			DesiredState TEXT NOT NULL,
			ConfigVersion INTEGER NOT NULL,
			CreatedAt TEXT NOT NULL,
			UpdatedAt TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS Users (
			Name TEXT PRIMARY KEY,
			Uuid TEXT NOT NULL UNIQUE,
			Token TEXT NOT NULL UNIQUE,
			Connected BOOLEAN NOT NULL,
			Created TEXT NOT NULL,
			LastSeen TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS UsersUuid ON Users (Uuid);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS UsersToken ON Users (Token);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS SpeakersUuid ON Speakers (Uuid);`,
		`CREATE INDEX IF NOT EXISTS EventsTypeCreatedAt ON Events (Type, CreatedAt);`,
	}
	for _, statement := range statements {
		if _, err := DBMS.DBConn.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureDefaultEventRetentionConfigurations(); err != nil {
		return err
	}
	if err := ensureSessionRoutingColumns(); err != nil {
		return err
	}
	if err := ensureBuildJobBuilderColumn(); err != nil {
		return err
	}
	_, err := DBMS.DBConn.Exec(`PRAGMA journal_mode=WAL;`)
	return err
}

func ensureBuildJobBuilderColumn() error {
	columns, err := DBMS.tableColumns("BuildJobs")
	if err != nil {
		return err
	}
	if columns["builder"] {
		return nil
	}
	_, err = DBMS.DBConn.Exec(`ALTER TABLE BuildJobs ADD COLUMN Builder TEXT NOT NULL DEFAULT '';`)
	return err
}

// ensureSessionRoutingColumns migrates databases created before sessions
// could be delivered through a speaker.
func ensureSessionRoutingColumns() error {
	rows, err := DBMS.DBConn.Query(`PRAGMA table_info(Sessions);`)
	if err != nil {
		return err
	}
	found := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		found[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	statements := []struct {
		name string
		sql  string
	}{
		{name: "transport", sql: `ALTER TABLE Sessions ADD COLUMN Transport TEXT NOT NULL DEFAULT 'listener';`},
		{name: "speaker", sql: `ALTER TABLE Sessions ADD COLUMN Speaker TEXT NOT NULL DEFAULT '';`},
		{name: "speakeruuid", sql: `ALTER TABLE Sessions ADD COLUMN SpeakerUuid TEXT NOT NULL DEFAULT '';`},
		{name: "healthmonitoring", sql: `ALTER TABLE Sessions ADD COLUMN HealthMonitoring BOOLEAN NOT NULL DEFAULT TRUE;`},
		{name: "listener", sql: `ALTER TABLE Sessions ADD COLUMN Listener TEXT NOT NULL DEFAULT '';`},
		{name: "listeneruuid", sql: `ALTER TABLE Sessions ADD COLUMN ListenerUuid TEXT NOT NULL DEFAULT '';`},
	}
	for _, statement := range statements {
		if found[statement.name] {
			continue
		}
		if _, err := DBMS.DBConn.Exec(statement.sql); err != nil {
			return err
		}
	}
	return nil
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
	var sequence int64
	if err := DBMS.DBConn.QueryRow(`
		SELECT COALESCE(
			(SELECT seq FROM sqlite_sequence WHERE name = 'Events'),
			(SELECT MAX(Sequence) FROM Events),
			0
		);
	`).Scan(&sequence); err != nil {
		return 0, err
	}
	return uint64(sequence), nil
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
