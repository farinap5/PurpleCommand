package db

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestImplantDefinitionLifecycle(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })
	if err := DBMS.dbCreateDs(); err != nil {
		t.Fatal(err)
	}

	profile := ImplantProfile{
		Name: "linux-impl", Type: "impl", OS: "linux", ARCH: "amd64",
		OSOptions: []string{"linux"}, ARCHOptions: []string{"amd64", "386"},
		Output: "implant", Template: "./template", PublicKey: "server.pub",
	}
	if err := DBImplantProfileInsert(profile); err != nil {
		t.Fatal(err)
	}
	wantHash := bytes.Repeat([]byte{0x5a}, 32)
	if err := DBImplantDefinitionUpsert(ImplantDefinition{
		Name: "linux-impl", Protocol: "HTTP", PayloadType: "impl",
		OperatingSystems: []string{"linux"}, Architectures: []string{"amd64", "386"},
		OptionsJSON: `{"path":"/","header":{"X-Test":"Test"}}`, OTSHash: wantHash,
	}); err != nil {
		t.Fatal(err)
	}

	definition, err := DBImplantDefinitionGet("linux-impl")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Protocol != "http" || definition.ConfigVersion != 1 {
		t.Fatalf("definition = %#v", definition)
	}
	if len(definition.OperatingSystems) != 1 || len(definition.Architectures) != 2 {
		t.Fatalf("definition target suggestions = %#v / %#v", definition.OperatingSystems, definition.Architectures)
	}
	if !bytes.Equal(definition.OTSHash, wantHash) {
		t.Fatalf("OTS hash = %x", definition.OTSHash)
	}

	if err := DBImplantDefinitionUpsert(ImplantDefinition{
		Name: "linux-impl", Protocol: "https", PayloadType: "impl",
		OperatingSystems: []string{"linux", "windows"}, Architectures: []string{"arm64"},
		OptionsJSON: `{"path":"/v2"}`,
	}); err != nil {
		t.Fatal(err)
	}
	definition, err = DBImplantDefinitionGet("linux-impl")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Protocol != "https" || definition.ConfigVersion != 2 || definition.OptionsJSON != `{"path":"/v2"}` {
		t.Fatalf("updated definition = %#v", definition)
	}
	if len(definition.OTSHash) != 0 {
		t.Fatalf("cleared OTS hash = %x", definition.OTSHash)
	}

	if err := DBImplantDefinitionUpsert(ImplantDefinition{
		Name: "bad", Protocol: "http", PayloadType: "impl", OptionsJSON: `[]`,
	}); err == nil {
		t.Fatal("accepted a non-object OPTIONS value")
	}

	if err := DBImplantProfileDelete("linux-impl"); err != nil {
		t.Fatal(err)
	}
	if _, err := DBImplantDefinitionGet("linux-impl"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("definition survived build-profile deletion: %v", err)
	}
}

func TestImplantDefinitionOneTimeSecretIsAtomic(t *testing.T) {
	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	connection.SetMaxOpenConns(1)
	defer connection.Close()
	previous := DBMS
	DBMS = DBDef{DBConn: connection}
	t.Cleanup(func() { DBMS = previous })
	if err := DBMS.dbCreateDs(); err != nil {
		t.Fatal(err)
	}
	if err := DBImplantProfileInsert(ImplantProfile{
		Name: "ots-profile", Type: "impl", Mode: "bind", OS: "linux", ARCH: "amd64",
		Output: "implant", Template: "./template", PublicKey: "server.pub",
	}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("operator supplied secret"))
	expires := time.Now().UTC().Add(time.Hour)
	if err := DBImplantDefinitionUpsert(ImplantDefinition{
		Name: "ots-profile", Protocol: "http", PayloadType: "impl", OptionsJSON: `{}`,
		OTSHash: digest[:], OTSExpiresAt: &expires,
	}); err != nil {
		t.Fatal(err)
	}
	var token [12]byte
	copy(token[:], digest[:12])
	if err := DBImplantDefinitionValidateAndConsumeOTS("ots-profile", token, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := DBImplantDefinitionValidateAndConsumeOTS("ots-profile", token, time.Now().UTC()); !errors.Is(err, ErrOTSUsed) {
		t.Fatalf("replayed OTS error = %v", err)
	}
	definition, err := DBImplantDefinitionGet("ots-profile")
	if err != nil || definition.OTSUsedAt == nil {
		t.Fatalf("consumed definition = %#v, %v", definition, err)
	}
}
