package implantbuilder

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
)

func TestProfileDefinitionOptionsAndOTSLifecycle(t *testing.T) {
	previousDatabasePath := db.DatabasePath
	previousDatabase := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "profiles.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DBMS = previousDatabase
		db.DatabasePath = previousDatabasePath
	})

	previousProfiles := ProfileMap
	previousCurrentName := CurrentName
	ProfileMap = make(map[string]*Profile)
	CurrentName = ""
	t.Cleanup(func() {
		ProfileMap = previousProfiles
		CurrentName = previousCurrentName
	})

	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	created, err := APICreateProfile(teamapi.Profile{
		Name: "http-profile", Protocol: "http",
		Options: json.RawMessage(`{"path":"/api","header":{"X-Test":"one"}}`),
		OTS:     "first-secret", OTSExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.OTS != "" || !created.OTSConfigured || created.OTSExpiresAt == nil || !created.OTSExpiresAt.Equal(expiresAt) {
		t.Fatalf("created profile metadata = %#v", created)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("first-secret")) {
		t.Fatal("profile reply exposed the plaintext OTS")
	}
	definition, err := db.DBImplantDefinitionGet("http-profile")
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256([]byte("first-secret"))
	if !bytes.Equal(definition.OTSHash, wantHash[:]) {
		t.Fatalf("stored OTS hash = %x", definition.OTSHash)
	}

	updated, err := APIUpdateProfile(teamapi.ProfileUpdateRequest{
		Name: "http-profile", Key: "OPTIONS",
		Value: `{"path":"/v2","header":{"X-Test":"two","X-Extra":"yes"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(updated.Options, []byte(`"X-Extra":"yes"`)) {
		t.Fatalf("updated options = %s", updated.Options)
	}
	if _, err := APIUpdateProfile(teamapi.ProfileUpdateRequest{
		Name: "http-profile", Key: "OTS_EXPIRES_AT", Value: "",
	}); err != nil {
		t.Fatal(err)
	}
	cleared, err := APIUpdateProfile(teamapi.ProfileUpdateRequest{
		Name: "http-profile", Key: "OTS_CLEAR", Value: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.OTSConfigured || cleared.OTSExpiresAt != nil {
		t.Fatalf("cleared OTS metadata = %#v", cleared)
	}
	if cleared.ConfigVersion <= created.ConfigVersion {
		t.Fatalf("config version did not advance: %d -> %d", created.ConfigVersion, cleared.ConfigVersion)
	}
}

func TestProfileListenerUUIDRequiresValidatedAttachmentAndSurvivesReload(t *testing.T) {
	previousDatabasePath := db.DatabasePath
	previousDatabase := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "profiles.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DBMS = previousDatabase
		db.DatabasePath = previousDatabasePath
	})

	previousProfiles := ProfileMap
	previousCurrentName := CurrentName
	ProfileMap = make(map[string]*Profile)
	CurrentName = ""
	t.Cleanup(func() {
		ProfileMap = previousProfiles
		CurrentName = previousCurrentName
	})

	created, err := APICreateProfile(teamapi.Profile{
		Name: "attached", LHOST: "manual.example:4444", ListenerUUID: "unvalidated-listener",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ListenerUUID != "" {
		t.Fatalf("profile creation accepted an unvalidated listener UUID: %#v", created)
	}
	rows, err := db.DBImplantProfileGetAll()
	if err != nil || len(rows) != 1 {
		t.Fatalf("stored profiles = %#v, %v", rows, err)
	}
	rows[0].ListenerUUID = "validated-listener"
	if err := db.DBImplantProfileUpdate(rows[0]); err != nil {
		t.Fatal(err)
	}

	ProfileMap = make(map[string]*Profile)
	ProfilesReloadFromDB()
	got, err := APIGetProfile("attached")
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenerUUID != "validated-listener" {
		t.Fatalf("reloaded profile listener UUID = %q", got.ListenerUUID)
	}
	listed := APIListProfiles()
	if len(listed) != 1 || listed[0].ListenerUUID != "validated-listener" {
		t.Fatalf("listed profiles = %#v", listed)
	}
}

func TestAPISetProfileListenerAtomicallyMaterializesAndDetaches(t *testing.T) {
	previousDatabasePath := db.DatabasePath
	previousDatabase := db.DBMS
	db.DatabasePath = filepath.Join(t.TempDir(), "listener-profile.db")
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.DBMS.DBConn.Close()
		db.DBMS = previousDatabase
		db.DatabasePath = previousDatabasePath
	})
	previousProfiles := ProfileMap
	ProfileMap = make(map[string]*Profile)
	t.Cleanup(func() { ProfileMap = previousProfiles })

	if _, err := APICreateProfile(teamapi.Profile{
		Name: "http-profile", LHOST: "manual.example:1111", Protocol: "http", Options: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	attached, changed, err := APISetProfileListener("http-profile", " listener-id ", " callback.example:4444 ")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || attached.ListenerUUID != "listener-id" || attached.LHOST != "callback.example:4444" {
		t.Fatalf("attached profile = %#v, changed=%t", attached, changed)
	}
	ProfileMap = make(map[string]*Profile)
	ProfilesReloadFromDB()
	reloaded, err := APIGetProfile("http-profile")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ListenerUUID != "listener-id" || reloaded.LHOST != "callback.example:4444" {
		t.Fatalf("reloaded attachment = %#v", reloaded)
	}
	unchanged, changed, err := APISetProfileListener("http-profile", "listener-id", "callback.example:4444")
	if err != nil || changed || unchanged.ListenerUUID != attached.ListenerUUID || unchanged.LHOST != attached.LHOST {
		t.Fatalf("idempotent attachment = %#v, changed=%t, err=%v", unchanged, changed, err)
	}
	detached, changed, err := APISetProfileListener("http-profile", "", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || detached.ListenerUUID != "" || detached.LHOST != "callback.example:4444" {
		t.Fatalf("detached profile = %#v, changed=%t", detached, changed)
	}
	rows, err := db.DBImplantProfileGetAll()
	if err != nil || len(rows) != 1 || rows[0].ListenerUUID != "" || rows[0].LHOST != "callback.example:4444" {
		t.Fatalf("persisted detached profile = %#v, err=%v", rows, err)
	}
	if _, _, err := APISetProfileListener("http-profile", "listener-id", " "); err == nil {
		t.Fatal("accepted an empty listener advertisement")
	}
	if _, _, err := APISetProfileListener("missing", "listener-id", "callback.example:4444"); err == nil {
		t.Fatal("attached a missing profile")
	}

	if _, err := APICreateProfile(teamapi.Profile{Name: "generic-profile", LHOST: "manual.example:2222"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := APISetProfileListener("generic-profile", "listener-id", "callback.example:4444"); err == nil || !strings.Contains(err.Error(), "HTTP implant profile") {
		t.Fatalf("generic profile attachment error = %v", err)
	}
	if _, err := APICreateProfile(teamapi.Profile{
		Name: "https-profile", LHOST: "manual.example:3333", Protocol: "https", Options: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := APISetProfileListener("https-profile", "listener-id", "callback.example:4444"); err == nil || !strings.Contains(err.Error(), "HTTP implant profile") {
		t.Fatalf("HTTPS profile attachment error = %v", err)
	}

	if _, _, err := APISetProfileListener("http-profile", "listener-id", "attached.example:4444"); err != nil {
		t.Fatal(err)
	}
	manual, err := APIUpdateProfile(teamapi.ProfileUpdateRequest{Name: "http-profile", Key: "LHOST", Value: "manual-again.example:7777"})
	if err != nil {
		t.Fatal(err)
	}
	if manual.ListenerUUID != "" || manual.LHOST != "manual-again.example:7777" {
		t.Fatalf("manual LHOST update did not detach = %#v", manual)
	}
	if _, _, err := APISetProfileListener("http-profile", "listener-id", "attached.example:4444"); err != nil {
		t.Fatal(err)
	}
	nonHTTP, err := APIUpdateProfile(teamapi.ProfileUpdateRequest{Name: "http-profile", Key: "PROTOCOL", Value: "https"})
	if err != nil {
		t.Fatal(err)
	}
	if nonHTTP.ListenerUUID != "" || nonHTTP.Protocol != "https" || nonHTTP.LHOST != "attached.example:4444" {
		t.Fatalf("protocol update did not detach = %#v", nonHTTP)
	}
	if _, err := APIUpdateProfile(teamapi.ProfileUpdateRequest{Name: "http-profile", Key: "PROTOCOL", Value: "http"}); err != nil {
		t.Fatal(err)
	}
	if _, err := APICreateProfile(teamapi.Profile{
		Name: "second-http", LHOST: "manual.example:5555", Protocol: "http", Options: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"http-profile", "second-http"} {
		if _, _, err := APISetProfileListener(name, "shared-listener", name+":4444"); err != nil {
			t.Fatal(err)
		}
	}
	cleared, err := APIClearProfileListeners(" shared-listener ")
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 2 || cleared[0].Name != "http-profile" || cleared[1].Name != "second-http" ||
		cleared[0].ListenerUUID != "" || cleared[1].ListenerUUID != "" {
		t.Fatalf("cleared listener profiles = %#v", cleared)
	}

	if _, _, err := APISetProfileListener("http-profile", "listener-id", "before-failure.example:4444"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DBMS.DBConn.Exec(`
CREATE TRIGGER reject_profile_listener_update
BEFORE UPDATE ON ImplantProfiles
BEGIN
    SELECT RAISE(FAIL, 'forced profile update failure');
END;`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := APISetProfileListener("http-profile", "listener-id", "after-failure.example:5555"); err == nil {
		t.Fatal("forced database update unexpectedly succeeded")
	}
	inMemory, err := APIGetProfile("http-profile")
	if err != nil {
		t.Fatal(err)
	}
	if inMemory.LHOST != "before-failure.example:4444" || inMemory.ListenerUUID != "listener-id" {
		t.Fatalf("failed update changed memory = %#v", inMemory)
	}
	rows, err = db.DBImplantProfileGetAll()
	if err != nil {
		t.Fatal(err)
	}
	var stored db.ImplantProfile
	for _, row := range rows {
		if row.Name == "http-profile" {
			stored = row
			break
		}
	}
	if stored.LHOST != "before-failure.example:4444" || stored.ListenerUUID != "listener-id" {
		t.Fatalf("failed update changed storage = %#v", stored)
	}
	if _, err := APIUpdateProfile(teamapi.ProfileUpdateRequest{Name: "http-profile", Key: "PROTOCOL", Value: "https"}); err == nil {
		t.Fatal("forced transactional protocol update unexpectedly succeeded")
	}
	definition, err := db.DBImplantDefinitionGet("http-profile")
	if err != nil {
		t.Fatal(err)
	}
	if definition.Protocol != "http" {
		t.Fatalf("failed protocol transaction changed definition = %#v", definition)
	}
	inMemory, err = APIGetProfile("http-profile")
	if err != nil {
		t.Fatal(err)
	}
	if inMemory.ListenerUUID != "listener-id" || inMemory.LHOST != "before-failure.example:4444" {
		t.Fatalf("failed protocol transaction changed profile = %#v", inMemory)
	}
}
