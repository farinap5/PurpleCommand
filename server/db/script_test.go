package db

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestDBScriptReplacePathMigratesAndDeduplicates(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if _, err := connection.Exec("CREATE TABLE Scripts (Sid INTEGER PRIMARY KEY, Path TEXT NOT NULL UNIQUE);"); err != nil {
		t.Fatal(err)
	}
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })

	if _, err := connection.Exec("INSERT INTO Scripts (Path) VALUES ('/old/script/main.lua');"); err != nil {
		t.Fatal(err)
	}
	if err := DBScriptReplacePath("/old/script/main.lua", "/new/script/main.lua"); err != nil {
		t.Fatalf("migrate path: %v", err)
	}
	if _, err := connection.Exec("INSERT INTO Scripts (Path) VALUES ('/stale/script/main.lua');"); err != nil {
		t.Fatal(err)
	}
	if err := DBScriptReplacePath("/stale/script/main.lua", "/new/script/main.lua"); err != nil {
		t.Fatalf("deduplicate path: %v", err)
	}

	var count int
	if err := connection.QueryRow("SELECT COUNT(*) FROM Scripts WHERE Path = '/new/script/main.lua';").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new path count = %d", count)
	}
	if err := connection.QueryRow("SELECT COUNT(*) FROM Scripts;").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("total script count = %d", count)
	}
}
