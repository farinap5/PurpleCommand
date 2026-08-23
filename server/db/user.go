package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"purpcmd/pkg/teamapi"

	"github.com/google/uuid"
)

var ErrUserNotFound = errors.New("user not found")

func UserCreate(name, token string) (teamapi.User, error) {
	if DBMS.DBConn == nil {
		return teamapi.User{}, errors.New("database is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return teamapi.User{}, errors.New("user name is required")
	}
	if len(name) > 64 {
		return teamapi.User{}, errors.New("user name must not exceed 64 characters")
	}
	if strings.EqualFold(name, "admin") {
		return teamapi.User{}, errors.New("the admin user name is reserved")
	}
	if token == "" {
		return teamapi.User{}, errors.New("user token is required")
	}

	now := time.Now().UTC()
	user := teamapi.User{
		Name: name, UUID: uuid.NewString(), Created: now, LastSeen: now,
	}
	_, err := DBMS.DBConn.Exec(
		`INSERT INTO Users (Name, Uuid, Token, Connected, Created, LastSeen) VALUES (?, ?, ?, ?, ?, ?);`,
		user.Name, user.UUID, token, false, formatDBTime(user.Created), formatDBTime(user.LastSeen),
	)
	if err != nil {
		return teamapi.User{}, fmt.Errorf("create user %q: %w", user.Name, err)
	}
	return user, nil
}

func UserUpdateToken(name, token string) (teamapi.User, error) {
	if DBMS.DBConn == nil {
		return teamapi.User{}, errors.New("database is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return teamapi.User{}, errors.New("user name is required")
	}
	if token == "" {
		return teamapi.User{}, errors.New("user token is required")
	}
	result, err := DBMS.DBConn.Exec(`UPDATE Users SET Token = ? WHERE Name = ?;`, token, name)
	if err != nil {
		return teamapi.User{}, fmt.Errorf("update user %q: %w", name, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return teamapi.User{}, err
	}
	if changed == 0 {
		return teamapi.User{}, fmt.Errorf("%w: %s", ErrUserNotFound, name)
	}
	return UserGet(name)
}

func UserDelete(name string) error {
	if DBMS.DBConn == nil {
		return errors.New("database is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("user name is required")
	}
	result, err := DBMS.DBConn.Exec(`DELETE FROM Users WHERE Name = ?;`, name)
	if err != nil {
		return fmt.Errorf("delete user %q: %w", name, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("%w: %s", ErrUserNotFound, name)
	}
	return nil
}

func UserGet(name string) (teamapi.User, error) {
	if DBMS.DBConn == nil {
		return teamapi.User{}, errors.New("database is not initialized")
	}
	return scanUser(DBMS.DBConn.QueryRow(
		`SELECT Name, Uuid, Connected, Created, LastSeen FROM Users WHERE Name = ?;`,
		strings.TrimSpace(name),
	))
}

func UserGetByToken(token string) (teamapi.User, bool, error) {
	if DBMS.DBConn == nil || token == "" {
		return teamapi.User{}, false, nil
	}
	user, err := scanUser(DBMS.DBConn.QueryRow(
		`SELECT Name, Uuid, Connected, Created, LastSeen FROM Users WHERE Token = ?;`, token,
	))
	if errors.Is(err, ErrUserNotFound) {
		return teamapi.User{}, false, nil
	}
	return user, err == nil, err
}

func UserList() ([]teamapi.User, error) {
	if DBMS.DBConn == nil {
		return nil, errors.New("database is not initialized")
	}
	rows, err := DBMS.DBConn.Query(
		`SELECT Name, Uuid, Connected, Created, LastSeen FROM Users ORDER BY Name;`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]teamapi.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func UserSetConnected(uuid string, connected bool, lastSeen time.Time) error {
	if DBMS.DBConn == nil {
		return nil
	}
	_, err := DBMS.DBConn.Exec(
		`UPDATE Users SET Connected = ?, LastSeen = ? WHERE Uuid = ?;`,
		connected, formatDBTime(lastSeen), uuid,
	)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (teamapi.User, error) {
	var user teamapi.User
	var created, lastSeen string
	if err := row.Scan(&user.Name, &user.UUID, &user.Connected, &created, &lastSeen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return teamapi.User{}, ErrUserNotFound
		}
		return teamapi.User{}, err
	}
	var err error
	user.Created, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return teamapi.User{}, err
	}
	user.LastSeen, err = time.Parse(time.RFC3339Nano, lastSeen)
	if err != nil {
		return teamapi.User{}, err
	}
	return user, nil
}
