package implantbuilder

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"path/filepath"
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
