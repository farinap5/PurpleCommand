package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"purpcmd/pkg/teamapi"
)

var ErrSpeakerConfigVersionConflict = errors.New("speaker configuration version conflict")

func DBSpeakerInsert(item teamapi.Speaker) error {
	if DBMS.DBConn == nil || !item.Persistent {
		return nil
	}
	configuration, err := json.Marshal(item.Config)
	if err != nil {
		return err
	}
	now := formatDBTime(time.Now().UTC())
	_, err = DBMS.DBConn.Exec(
		`INSERT INTO Speakers (Name, Uuid, Config, Persistent, DesiredState, ConfigVersion, CreatedAt, UpdatedAt)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?);`,
		item.Name, item.UUID, configuration, item.Persistent, item.DesiredState, item.ConfigVersion, now, now,
	)
	return err
}

func DBSpeakerUpdate(previousName string, expectedVersion uint64, item teamapi.Speaker) error {
	if DBMS.DBConn == nil {
		return nil
	}
	configuration, err := json.Marshal(item.Config)
	if err != nil {
		return err
	}
	result, err := DBMS.DBConn.Exec(
		`UPDATE Speakers SET Name=?, Config=?, Persistent=?, DesiredState=?, ConfigVersion=?, UpdatedAt=?
		 WHERE Name=? AND Uuid=? AND ConfigVersion=?;`,
		item.Name, configuration, item.Persistent, item.DesiredState, item.ConfigVersion, formatDBTime(time.Now().UTC()),
		previousName, item.UUID, expectedVersion,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrSpeakerConfigVersionConflict
	}
	return nil
}

func DBSpeakerDelete(name, id string) error {
	if DBMS.DBConn == nil {
		return nil
	}
	_, err := DBMS.DBConn.Exec(`DELETE FROM Speakers WHERE Name=? AND Uuid=?;`, name, id)
	return err
}

func DBSpeakerList() ([]teamapi.Speaker, error) {
	if DBMS.DBConn == nil {
		return nil, nil
	}
	rows, err := DBMS.DBConn.Query(
		`SELECT Name, Uuid, Config, Persistent, DesiredState, ConfigVersion FROM Speakers ORDER BY Name;`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]teamapi.Speaker, 0)
	for rows.Next() {
		var item teamapi.Speaker
		var configuration []byte
		if err := rows.Scan(&item.Name, &item.UUID, &configuration, &item.Persistent, &item.DesiredState, &item.ConfigVersion); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(configuration, &item.Config); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func DBSpeakerExists(name string) (bool, error) {
	if DBMS.DBConn == nil {
		return false, nil
	}
	var exists int
	err := DBMS.DBConn.QueryRow(`SELECT 1 FROM Speakers WHERE Name=?;`, name).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
