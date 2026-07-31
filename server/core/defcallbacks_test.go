package core

import (
	"testing"

	"purpcmd/internal"
	"purpcmd/server/implant"
	"purpcmd/server/types"
)

func TestRunDeleteHandlesSessionLifecycle(t *testing.T) {
	previousMap := implant.ImplantMAP
	previousCurrent := implant.CurrentImplant
	previousPrefix := LivePrefixState
	implant.ImplantMAP = make(map[string]*implant.Implant)
	implant.CurrentImplant = "none"
	t.Cleanup(func() {
		implant.ImplantMAP = previousMap
		implant.CurrentImplant = previousCurrent
		LivePrefixState = previousPrefix
	})

	imp := implant.ImplantNew("12345")
	imp.Metadata.Sleep = 60
	imp.ImplantAddImplant()
	implant.CurrentImplant = imp.Name
	profile := &types.Profile{STATE: types.SESSION, Prompt: "(session - 12345)>> "}

	if result := runDelete([]string{"delete"}, profile); result != 1 {
		t.Fatalf("live delete result = %d, want 1", result)
	}
	if result := runDelete([]string{"delete", "terminate"}, profile); result != 0 {
		t.Fatalf("terminate result = %d, want 0", result)
	}
	if len(imp.Task) != 1 || imp.Task[0].Code != internal.KILL {
		t.Fatalf("termination did not queue one KILL task: %#v", imp.Task)
	}
}
