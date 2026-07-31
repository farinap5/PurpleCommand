package core

import (
	"testing"

	"purpcmd/internal"
)

func TestImplantInitUsesConfiguredPayloadType(t *testing.T) {
	if got := ImplantInit().Type; got != internal.DefaultPayloadType {
		t.Fatalf("default payload type = %q", got)
	}
	if got := ImplantInit("linux.impl").Type; got != "linux.impl" {
		t.Fatalf("configured payload type = %q", got)
	}
	if got := ImplantInit("invalid type").Type; got != internal.DefaultPayloadType {
		t.Fatalf("invalid payload type did not fall back: %q", got)
	}
}
