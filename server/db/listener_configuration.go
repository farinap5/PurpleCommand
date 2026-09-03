package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrListenerConfigConflict = errors.New("listener configuration changed")

type ListenerConfiguration struct {
	Name          string
	UUID          string
	Driver        string
	OptionsJSON   string
	RoutesJSON    string
	Persistent    bool
	DesiredState  string
	ConfigVersion int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func DBListenerConfigInsert(configuration ListenerConfiguration) (ListenerConfiguration, error) {
	configuration, err := normalizeListenerConfiguration(configuration)
	if err != nil {
		return ListenerConfiguration{}, err
	}
	_, err = DBMS.DBConn.Exec(`
INSERT INTO Listeners
    (Uuid, Name, Host, Port, Persist, Running, Driver, OptionsJSON, RoutesJSON,
     DesiredState, ConfigVersion, CreatedAt, UpdatedAt)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
		configuration.UUID, configuration.Name, legacyListenerHost(configuration.OptionsJSON),
		legacyListenerPort(configuration.OptionsJSON), configuration.Persistent,
		configuration.DesiredState == "running", configuration.Driver, configuration.OptionsJSON,
		configuration.RoutesJSON, configuration.DesiredState, configuration.ConfigVersion,
		configuration.CreatedAt.Format(time.RFC3339Nano), configuration.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return ListenerConfiguration{}, err
	}
	return configuration, nil
}

func DBListenerConfigGet(name string) (ListenerConfiguration, error) {
	row := DBMS.DBConn.QueryRow(`
SELECT Name, Uuid, Driver, OptionsJSON, RoutesJSON, Persist, DesiredState,
       ConfigVersion, CreatedAt, UpdatedAt
FROM Listeners WHERE Name = ?;`, name)
	return scanListenerConfiguration(row)
}

func DBListenerConfigList() ([]ListenerConfiguration, error) {
	rows, err := DBMS.DBConn.Query(`
SELECT Name, Uuid, Driver, OptionsJSON, RoutesJSON, Persist, DesiredState,
       ConfigVersion, CreatedAt, UpdatedAt
FROM Listeners ORDER BY Name;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ListenerConfiguration, 0)
	for rows.Next() {
		configuration, err := scanListenerConfiguration(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, configuration)
	}
	return result, rows.Err()
}

func DBListenerConfigUpdate(configuration ListenerConfiguration) (ListenerConfiguration, error) {
	configuration, err := normalizeListenerConfiguration(configuration)
	if err != nil {
		return ListenerConfiguration{}, err
	}
	expectedVersion := configuration.ConfigVersion
	configuration.ConfigVersion++
	configuration.UpdatedAt = time.Now().UTC()
	result, err := DBMS.DBConn.Exec(`
UPDATE Listeners
SET Driver = ?, OptionsJSON = ?, RoutesJSON = ?, Persist = ?, Running = ?,
    DesiredState = ?, ConfigVersion = ?, UpdatedAt = ?, Host = ?, Port = ?
WHERE Name = ? AND ConfigVersion = ?;`,
		configuration.Driver, configuration.OptionsJSON, configuration.RoutesJSON,
		configuration.Persistent, configuration.DesiredState == "running", configuration.DesiredState,
		configuration.ConfigVersion, configuration.UpdatedAt.Format(time.RFC3339Nano),
		legacyListenerHost(configuration.OptionsJSON), legacyListenerPort(configuration.OptionsJSON),
		configuration.Name, expectedVersion,
	)
	if err != nil {
		return ListenerConfiguration{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return ListenerConfiguration{}, err
	}
	if count != 1 {
		if !DBListenerExist(configuration.Name) {
			return ListenerConfiguration{}, sql.ErrNoRows
		}
		return ListenerConfiguration{}, ErrListenerConfigConflict
	}
	return configuration, nil
}

func DBListenerConfigDelete(name string) error {
	return DBListenerDelete(name)
}

func normalizeListenerConfiguration(configuration ListenerConfiguration) (ListenerConfiguration, error) {
	configuration.Name = strings.TrimSpace(configuration.Name)
	configuration.UUID = strings.TrimSpace(configuration.UUID)
	configuration.Driver = strings.ToLower(strings.TrimSpace(configuration.Driver))
	if configuration.Name == "" || configuration.UUID == "" || configuration.Driver == "" {
		return ListenerConfiguration{}, errors.New("listener name, UUID, and driver are required")
	}
	if configuration.OptionsJSON == "" {
		configuration.OptionsJSON = `{}`
	}
	if configuration.RoutesJSON == "" {
		configuration.RoutesJSON = `[]`
	}
	if err := validateJSONKind(configuration.OptionsJSON, "object"); err != nil {
		return ListenerConfiguration{}, fmt.Errorf("listener options: %w", err)
	}
	if err := validateJSONKind(configuration.RoutesJSON, "array"); err != nil {
		return ListenerConfiguration{}, fmt.Errorf("listener routes: %w", err)
	}
	if configuration.DesiredState == "" {
		configuration.DesiredState = "stopped"
	}
	if configuration.DesiredState != "stopped" && configuration.DesiredState != "running" {
		return ListenerConfiguration{}, errors.New("listener desired state must be stopped or running")
	}
	if configuration.ConfigVersion < 1 {
		configuration.ConfigVersion = 1
	}
	now := time.Now().UTC()
	if configuration.CreatedAt.IsZero() {
		configuration.CreatedAt = now
	}
	if configuration.UpdatedAt.IsZero() {
		configuration.UpdatedAt = now
	}
	return configuration, nil
}

func validateJSONKind(value, expected string) error {
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return err
	}
	valid := false
	switch expected {
	case "object":
		_, valid = decoded.(map[string]any)
	case "array":
		_, valid = decoded.([]any)
	}
	if !valid {
		return fmt.Errorf("must be a JSON %s", expected)
	}
	return nil
}

type listenerConfigurationScanner interface {
	Scan(...any) error
}

func scanListenerConfiguration(scanner listenerConfigurationScanner) (ListenerConfiguration, error) {
	var configuration ListenerConfiguration
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&configuration.Name, &configuration.UUID, &configuration.Driver,
		&configuration.OptionsJSON, &configuration.RoutesJSON, &configuration.Persistent,
		&configuration.DesiredState, &configuration.ConfigVersion, &createdAt, &updatedAt,
	); err != nil {
		return ListenerConfiguration{}, err
	}
	var err error
	configuration.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ListenerConfiguration{}, fmt.Errorf("parse listener created time: %w", err)
	}
	configuration.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return ListenerConfiguration{}, fmt.Errorf("parse listener updated time: %w", err)
	}
	return configuration, nil
}

func legacyListenerHost(optionsJSON string) string {
	var options struct {
		Bind struct {
			Host string `json:"host"`
		} `json:"bind"`
	}
	_ = json.Unmarshal([]byte(optionsJSON), &options)
	if options.Bind.Host == "" {
		return "0.0.0.0"
	}
	return options.Bind.Host
}

func legacyListenerPort(optionsJSON string) string {
	var object map[string]any
	if json.Unmarshal([]byte(optionsJSON), &object) == nil {
		if bind, ok := object["bind"].(map[string]any); ok {
			switch value := bind["port"].(type) {
			case string:
				if value != "" {
					return value
				}
			case float64:
				return fmt.Sprintf("%.0f", value)
			}
		}
	}
	return "4444"
}
