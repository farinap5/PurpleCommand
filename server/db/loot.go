package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrAmbiguousLootUUID = errors.New("loot UUID fragment is ambiguous")

func DBLootInsert(id, session, fileName string) error {
	_, err := DBMS.DBConn.Exec(
		`INSERT INTO Loot (Uuid, Session, FileName) VALUES (?,?,?);`,
		id, session, fileName,
	)
	return err
}

func DBLoot(id string) (string, string, string, error) {
	row := DBMS.DBConn.QueryRow(
		`SELECT Uuid, Session, FileName FROM Loot WHERE Uuid = ?;`,
		id,
	)

	var storedID, session, fileName string
	err := row.Scan(&storedID, &session, &fileName)
	return storedID, session, fileName, err
}

func DBLootList() ([][3]string, error) {
	rows, err := DBMS.DBConn.Query(`SELECT Uuid, Session, FileName FROM Loot;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lootList [][3]string
	for rows.Next() {
		var id, session, fileName string
		if err := rows.Scan(&id, &session, &fileName); err != nil {
			return nil, err
		}
		lootList = append(lootList, [3]string{id, session, fileName})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lootList, nil
}

// DBLootGetByUUID resolves either an exact UUID or a unique UUID fragment.
// Exact matches take precedence; ambiguous fragments are rejected rather than
// selecting an arbitrary loot entry.
func DBLootGetByUUID(fragment string) (name string, fullID string, err error) {
	fragment = strings.TrimSpace(fragment)
	if fragment == "" {
		return "", "", errors.New("loot UUID must not be empty")
	}
	if strings.IndexFunc(fragment, func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == '-')
	}) >= 0 {
		return "", "", fmt.Errorf("invalid loot UUID fragment %q", fragment)
	}

	err = DBMS.DBConn.QueryRow(
		`SELECT FileName, Uuid FROM Loot WHERE Uuid = ?;`,
		fragment,
	).Scan(&name, &fullID)
	if err == nil {
		return name, fullID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}

	rows, err := DBMS.DBConn.Query(
		`SELECT FileName, Uuid FROM Loot WHERE Uuid LIKE ? ORDER BY Uuid LIMIT 2;`,
		"%"+fragment+"%",
	)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()

	matches := 0
	for rows.Next() {
		if err := rows.Scan(&name, &fullID); err != nil {
			return "", "", err
		}
		matches++
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	switch matches {
	case 0:
		return "", "", sql.ErrNoRows
	case 1:
		return name, fullID, nil
	default:
		return "", "", fmt.Errorf("%w: %q matched multiple entries", ErrAmbiguousLootUUID, fragment)
	}
}

func DBLootDelete(id string) error {
	result, err := DBMS.DBConn.Exec(`DELETE FROM Loot WHERE Uuid = ?;`, id)
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
