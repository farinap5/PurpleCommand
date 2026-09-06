package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ImplantDefinition stores the protocol-facing portion of an implant profile.
// Build-only fields remain in ImplantProfiles until the public profile API is
// migrated to the new model.
type ImplantDefinition struct {
	Name             string
	Protocol         string
	PayloadType      string
	OperatingSystems []string
	Architectures    []string
	OptionsJSON      string
	OTSHash          []byte
	OTSExpiresAt     *time.Time
	OTSUsedAt        *time.Time
	ConfigVersion    int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (db *DBDef) ensureImplantDefinitionsTable() error {
	_, err := db.DBConn.Exec(`
CREATE TABLE IF NOT EXISTS ImplantDefinitions (
    Name          TEXT PRIMARY KEY,
    Protocol      TEXT NOT NULL,
    PayloadType   TEXT NOT NULL,
    OperatingSystemsJSON TEXT NOT NULL DEFAULT '[]'
                         CHECK (json_valid(OperatingSystemsJSON) AND json_type(OperatingSystemsJSON) = 'array'),
    ArchitecturesJSON    TEXT NOT NULL DEFAULT '[]'
                         CHECK (json_valid(ArchitecturesJSON) AND json_type(ArchitecturesJSON) = 'array'),
    OptionsJSON   TEXT NOT NULL DEFAULT '{}'
                  CHECK (json_valid(OptionsJSON) AND json_type(OptionsJSON) = 'object'),
    OTSHash       BLOB,
    OTSExpiresAt  TEXT,
    OTSUsedAt     TEXT,
    ConfigVersion INTEGER NOT NULL DEFAULT 1 CHECK (ConfigVersion > 0),
    CreatedAt     TEXT NOT NULL,
    UpdatedAt     TEXT NOT NULL,
    FOREIGN KEY (Name) REFERENCES ImplantProfiles(Name) ON UPDATE CASCADE ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_implant_definitions_protocol
    ON ImplantDefinitions(Protocol);
CREATE INDEX IF NOT EXISTS idx_implant_definitions_payload_type
    ON ImplantDefinitions(PayloadType);
`)
	return err
}

func (db *DBDef) ensureImplantDefinitionTargetColumns() error {
	columns, err := db.tableColumns("ImplantDefinitions")
	if err != nil {
		return err
	}
	if !columns["operatingsystemsjson"] {
		if _, err := db.DBConn.Exec(`ALTER TABLE ImplantDefinitions ADD COLUMN OperatingSystemsJSON TEXT NOT NULL DEFAULT '[]';`); err != nil {
			return err
		}
	}
	if !columns["architecturesjson"] {
		if _, err := db.DBConn.Exec(`ALTER TABLE ImplantDefinitions ADD COLUMN ArchitecturesJSON TEXT NOT NULL DEFAULT '[]';`); err != nil {
			return err
		}
	}
	_, err = db.DBConn.Exec(`
UPDATE ImplantDefinitions
SET OperatingSystemsJSON = COALESCE(
    (SELECT OSOptionsJSON FROM ImplantProfiles WHERE ImplantProfiles.Name = ImplantDefinitions.Name),
    '[]'
)
WHERE OperatingSystemsJSON = '[]';
UPDATE ImplantDefinitions
SET ArchitecturesJSON = COALESCE(
    (SELECT ARCHOptionsJSON FROM ImplantProfiles WHERE ImplantProfiles.Name = ImplantDefinitions.Name),
    '[]'
)
WHERE ArchitecturesJSON = '[]';
`)
	return err
}

func DBImplantDefinitionUpsert(definition ImplantDefinition) error {
	if DBMS.DBConn == nil {
		return errors.New("database is not initialized")
	}
	definition, operatingSystemsJSON, architecturesJSON, err := normalizeImplantDefinition(definition)
	if err != nil {
		return err
	}
	return execImplantDefinitionUpsert(DBMS.DBConn, definition, operatingSystemsJSON, architecturesJSON)
}

// DBImplantDefinitionUpsertAndClearProfileListener changes a profile protocol
// and clears its HTTP listener association as one database transaction.
func DBImplantDefinitionUpsertAndClearProfileListener(definition ImplantDefinition) error {
	if DBMS.DBConn == nil {
		return errors.New("database is not initialized")
	}
	definition, operatingSystemsJSON, architecturesJSON, err := normalizeImplantDefinition(definition)
	if err != nil {
		return err
	}
	tx, err := DBMS.DBConn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := execImplantDefinitionUpsert(tx, definition, operatingSystemsJSON, architecturesJSON); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE ImplantProfiles SET ListenerUUID = '' WHERE Name = ?;`, definition.Name)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("profile not found")
	}
	return tx.Commit()
}

func normalizeImplantDefinition(definition ImplantDefinition) (ImplantDefinition, string, string, error) {
	definition.Name = strings.TrimSpace(definition.Name)
	definition.Protocol = strings.ToLower(strings.TrimSpace(definition.Protocol))
	definition.PayloadType = strings.TrimSpace(definition.PayloadType)
	if definition.Name == "" || definition.Protocol == "" || definition.PayloadType == "" {
		return ImplantDefinition{}, "", "", errors.New("implant definition name, protocol, and payload type are required")
	}
	if err := validateJSONObject(definition.OptionsJSON); err != nil {
		return ImplantDefinition{}, "", "", fmt.Errorf("implant definition options: %w", err)
	}
	now := time.Now().UTC()
	if definition.CreatedAt.IsZero() {
		definition.CreatedAt = now
	}
	if definition.UpdatedAt.IsZero() {
		definition.UpdatedAt = now
	}
	if definition.ConfigVersion < 1 {
		definition.ConfigVersion = 1
	}
	operatingSystemsJSON, err := encodeStringList(definition.OperatingSystems)
	if err != nil {
		return ImplantDefinition{}, "", "", fmt.Errorf("encode implant definition operating systems: %w", err)
	}
	architecturesJSON, err := encodeStringList(definition.Architectures)
	if err != nil {
		return ImplantDefinition{}, "", "", fmt.Errorf("encode implant definition architectures: %w", err)
	}
	return definition, operatingSystemsJSON, architecturesJSON, nil
}

type implantDefinitionExecer interface {
	Exec(string, ...any) (sql.Result, error)
}

func execImplantDefinitionUpsert(execer implantDefinitionExecer, definition ImplantDefinition, operatingSystemsJSON, architecturesJSON string) error {
	_, err := execer.Exec(`
INSERT INTO ImplantDefinitions
    (Name, Protocol, PayloadType, OperatingSystemsJSON, ArchitecturesJSON,
     OptionsJSON, OTSHash, OTSExpiresAt, OTSUsedAt, ConfigVersion, CreatedAt, UpdatedAt)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(Name) DO UPDATE SET
    Protocol=excluded.Protocol,
    PayloadType=excluded.PayloadType,
    OperatingSystemsJSON=excluded.OperatingSystemsJSON,
    ArchitecturesJSON=excluded.ArchitecturesJSON,
    OptionsJSON=excluded.OptionsJSON,
    OTSHash=excluded.OTSHash,
    OTSExpiresAt=excluded.OTSExpiresAt,
    OTSUsedAt=excluded.OTSUsedAt,
    ConfigVersion=ImplantDefinitions.ConfigVersion + 1,
    UpdatedAt=excluded.UpdatedAt;
`, definition.Name, definition.Protocol, definition.PayloadType, operatingSystemsJSON, architecturesJSON, definition.OptionsJSON,
		nullBytes(definition.OTSHash), nullableTime(definition.OTSExpiresAt), nullableTime(definition.OTSUsedAt),
		definition.ConfigVersion, definition.CreatedAt.UTC().Format(time.RFC3339Nano),
		definition.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func DBImplantDefinitionGet(name string) (ImplantDefinition, error) {
	if DBMS.DBConn == nil {
		return ImplantDefinition{}, errors.New("database is not initialized")
	}
	row := DBMS.DBConn.QueryRow(`
SELECT Name, Protocol, PayloadType, OptionsJSON, OTSHash, OTSExpiresAt, OTSUsedAt,
       OperatingSystemsJSON, ArchitecturesJSON, ConfigVersion, CreatedAt, UpdatedAt
FROM ImplantDefinitions WHERE Name = ?;
`, name)
	return scanImplantDefinition(row)
}

func DBImplantDefinitionGetAll() ([]ImplantDefinition, error) {
	if DBMS.DBConn == nil {
		return nil, errors.New("database is not initialized")
	}
	rows, err := DBMS.DBConn.Query(`
SELECT Name, Protocol, PayloadType, OptionsJSON, OTSHash, OTSExpiresAt, OTSUsedAt,
       OperatingSystemsJSON, ArchitecturesJSON, ConfigVersion, CreatedAt, UpdatedAt
FROM ImplantDefinitions ORDER BY Name;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	definitions := make([]ImplantDefinition, 0)
	for rows.Next() {
		definition, scanErr := scanImplantDefinition(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		definitions = append(definitions, definition)
	}
	return definitions, rows.Err()
}

func DBImplantDefinitionDelete(name string) error {
	if DBMS.DBConn == nil {
		return errors.New("database is not initialized")
	}
	result, err := DBMS.DBConn.Exec(`DELETE FROM ImplantDefinitions WHERE Name = ?;`, name)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("implant definition not found")
	}
	return nil
}

type definitionScanner interface {
	Scan(dest ...any) error
}

func scanImplantDefinition(scanner definitionScanner) (ImplantDefinition, error) {
	var definition ImplantDefinition
	var otsHash []byte
	var otsExpiresAt, otsUsedAt sql.NullString
	var operatingSystemsJSON, architecturesJSON string
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&definition.Name, &definition.Protocol, &definition.PayloadType, &definition.OptionsJSON,
		&otsHash, &otsExpiresAt, &otsUsedAt, &operatingSystemsJSON, &architecturesJSON,
		&definition.ConfigVersion, &createdAt, &updatedAt,
	); err != nil {
		return ImplantDefinition{}, err
	}
	var err error
	if definition.OperatingSystems, err = decodeStringList(operatingSystemsJSON); err != nil {
		return ImplantDefinition{}, fmt.Errorf("decode implant definition operating systems: %w", err)
	}
	if definition.Architectures, err = decodeStringList(architecturesJSON); err != nil {
		return ImplantDefinition{}, fmt.Errorf("decode implant definition architectures: %w", err)
	}
	definition.OTSHash = append([]byte(nil), otsHash...)
	if definition.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return ImplantDefinition{}, fmt.Errorf("parse implant definition created time: %w", err)
	}
	if definition.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return ImplantDefinition{}, fmt.Errorf("parse implant definition updated time: %w", err)
	}
	if definition.OTSExpiresAt, err = parseNullableTime(otsExpiresAt); err != nil {
		return ImplantDefinition{}, fmt.Errorf("parse implant definition OTS expiry: %w", err)
	}
	if definition.OTSUsedAt, err = parseNullableTime(otsUsedAt); err != nil {
		return ImplantDefinition{}, fmt.Errorf("parse implant definition OTS used time: %w", err)
	}
	return definition, nil
}

func validateJSONObject(value string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("must be a JSON object")
	}
	return nil
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
