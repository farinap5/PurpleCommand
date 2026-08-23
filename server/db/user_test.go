package db

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestUserLifecycle(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}

	created, err := UserCreate("alice", "alice-token")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "alice" || created.UUID == "" || created.Connected {
		t.Fatalf("created user = %#v", created)
	}
	if _, err := UserCreate("alice", "other-token"); err == nil {
		t.Fatal("duplicate user name was accepted")
	}

	found, ok, err := UserGetByToken("alice-token")
	if err != nil || !ok || found.UUID != created.UUID {
		t.Fatalf("lookup = %#v, %t, %v", found, ok, err)
	}
	lastSeen := time.Now().UTC().Add(time.Second)
	if err := UserSetConnected(created.UUID, true, lastSeen); err != nil {
		t.Fatal(err)
	}
	updated, err := UserUpdateToken("alice", "refreshed-token")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Connected || !updated.LastSeen.Equal(lastSeen) {
		t.Fatalf("updated user = %#v", updated)
	}
	if _, ok, err := UserGetByToken("alice-token"); err != nil || ok {
		t.Fatalf("old token lookup: found=%t err=%v", ok, err)
	}
	if _, ok, err := UserGetByToken("refreshed-token"); err != nil || !ok {
		t.Fatalf("new token lookup: found=%t err=%v", ok, err)
	}

	if err := UserDelete("alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := UserGet("alice"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("deleted user lookup error = %v", err)
	}
}

func TestUserCreateReservesAdminName(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })
	if err := EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := UserCreate("AdMiN", "token"); err == nil {
		t.Fatal("reserved admin name was accepted")
	}
}
