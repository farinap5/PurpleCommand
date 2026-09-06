package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ImplantProfile mirrors implantbuilder.Profile for DB storage.
type ImplantProfile struct {
	Name         string
	Type         string
	LHOST        string
	OS           string
	ARCH         string
	OSOptions    []string
	ARCHOptions  []string
	Output       string
	Template     string
	PublicKey    string
	Builder      string
	ListenerUUID string
}

func DBImplantProfileExists(name string) bool {
	var n string
	err := DBMS.DBConn.QueryRow("SELECT Name FROM ImplantProfiles WHERE Name = ?;", name).Scan(&n)
	return err == nil
}

func DBImplantProfileInsert(p ImplantProfile) error {
	if DBImplantProfileExists(p.Name) {
		return errors.New("profile already exists")
	}
	osOptionsJSON, err := encodeStringList(p.OSOptions)
	if err != nil {
		return fmt.Errorf("encode OS options: %w", err)
	}
	archOptionsJSON, err := encodeStringList(p.ARCHOptions)
	if err != nil {
		return fmt.Errorf("encode architecture options: %w", err)
	}
	_, err = DBMS.DBConn.Exec(
		`INSERT INTO ImplantProfiles
		 (Name, Type, LHOST, OS, ARCH, OSOptionsJSON, ARCHOptionsJSON, Output, Template, PublicKey, Builder, ListenerUUID)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?);`,
		p.Name, p.Type, p.LHOST, p.OS, p.ARCH, osOptionsJSON, archOptionsJSON, p.Output, p.Template, p.PublicKey, p.Builder, p.ListenerUUID,
	)
	return err
}

func DBImplantProfileUpdate(p ImplantProfile) error {
	if !DBImplantProfileExists(p.Name) {
		return errors.New("profile not found")
	}
	osOptionsJSON, err := encodeStringList(p.OSOptions)
	if err != nil {
		return fmt.Errorf("encode OS options: %w", err)
	}
	archOptionsJSON, err := encodeStringList(p.ARCHOptions)
	if err != nil {
		return fmt.Errorf("encode architecture options: %w", err)
	}
	_, err = DBMS.DBConn.Exec(
		`UPDATE ImplantProfiles
		 SET Type=?, LHOST=?, OS=?, ARCH=?, OSOptionsJSON=?, ARCHOptionsJSON=?, Output=?, Template=?, PublicKey=?, Builder=?, ListenerUUID=?
		 WHERE Name=?;`,
		p.Type, p.LHOST, p.OS, p.ARCH, osOptionsJSON, archOptionsJSON, p.Output, p.Template, p.PublicKey, p.Builder, p.ListenerUUID, p.Name,
	)
	return err
}

func DBImplantProfileDelete(name string) error {
	tx, err := DBMS.DBConn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM ImplantDefinitions WHERE Name = ?;`, name); err != nil {
		return err
	}
	result, err := tx.Exec(`DELETE FROM ImplantProfiles WHERE Name = ?;`, name)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("profile not found")
	}
	return tx.Commit()
}

// DBImplantProfilesClearListener removes a soft listener association without
// changing the materialized callback address used by existing builds.
func DBImplantProfilesClearListener(listenerUUID string) (int64, error) {
	result, err := DBMS.DBConn.Exec(
		`UPDATE ImplantProfiles SET ListenerUUID = '' WHERE ListenerUUID = ?;`,
		listenerUUID,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func DBImplantProfileGetAll() ([]ImplantProfile, error) {
	var profiles []ImplantProfile
	rows, err := DBMS.DBConn.Query(
		`SELECT Name, Type, LHOST, OS, ARCH, OSOptionsJSON, ARCHOptionsJSON, Output, Template, PublicKey, Builder, ListenerUUID
		 FROM ImplantProfiles;`,
	)
	if err != nil {
		return nil, err
	}
	defer func(r *sql.Rows) { _ = r.Close() }(rows)

	for rows.Next() {
		var p ImplantProfile
		var osOptionsJSON, archOptionsJSON string
		if err := rows.Scan(
			&p.Name, &p.Type, &p.LHOST, &p.OS, &p.ARCH, &osOptionsJSON, &archOptionsJSON,
			&p.Output, &p.Template, &p.PublicKey, &p.Builder, &p.ListenerUUID,
		); err != nil {
			return nil, err
		}
		if p.OSOptions, err = decodeStringList(osOptionsJSON); err != nil {
			return nil, fmt.Errorf("decode OS options for profile %q: %w", p.Name, err)
		}
		if p.ARCHOptions, err = decodeStringList(archOptionsJSON); err != nil {
			return nil, fmt.Errorf("decode architecture options for profile %q: %w", p.Name, err)
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

func encodeStringList(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	encoded, err := json.Marshal(values)
	return string(encoded), err
}

func decodeStringList(value string) ([]string, error) {
	var decoded []string
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, err
	}
	if decoded == nil {
		decoded = []string{}
	}
	return decoded, nil
}
