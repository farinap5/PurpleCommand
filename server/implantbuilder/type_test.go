package implantbuilder

import (
	"testing"

	"purpcmd/internal"
)

func TestProfilePayloadTypeDefaultsAndPersistenceMapping(t *testing.T) {
	profile := defaultProfile()
	if profile.Type != internal.DefaultPayloadType {
		t.Fatalf("default profile type = %q", profile.Type)
	}
	profile.Type = "linux.impl"
	profile.Builder = "linux-builder"
	row := profileToDBRow("linux", profile)
	if row.Type != profile.Type {
		t.Fatalf("database row type = %q", row.Type)
	}
	if row.Builder != profile.Builder {
		t.Fatalf("database row builder = %q", row.Builder)
	}
}
