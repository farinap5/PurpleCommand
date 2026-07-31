package implant

import (
	"errors"
	"testing"
	"time"

	"purpcmd/internal"
)

func isolateImplants(t *testing.T) {
	t.Helper()
	previousMap := ImplantMAP
	previousCurrent := CurrentImplant
	ImplantMAP = make(map[string]*Implant)
	CurrentImplant = "none"
	t.Cleanup(func() {
		ImplantMAP = previousMap
		CurrentImplant = previousCurrent
	})
}

func TestLiveSessionRequiresTerminationBeforeDeletion(t *testing.T) {
	isolateImplants(t)
	imp := ImplantNew("live")
	imp.Metadata.Sleep = 60
	imp.ImplantAddImplant()
	CurrentImplant = imp.Name

	if err := ImplantDelete(); !errors.Is(err, ErrImplantAlive) {
		t.Fatalf("live deletion returned %v", err)
	}
	if _, exists := ImplantMAP[imp.Name]; !exists {
		t.Fatal("live session was deleted")
	}
}

func TestTerminationDispatchMakesSessionDeletable(t *testing.T) {
	isolateImplants(t)
	imp := ImplantNew("terminating")
	imp.Metadata.Sleep = 60
	imp.ImplantAddImplant()
	CurrentImplant = imp.Name

	taskID, err := ImplantRequestTermination()
	if err != nil {
		t.Fatalf("request termination: %v", err)
	}
	if len(imp.Task) != 1 || imp.Task[0].Code != internal.KILL || imp.Task[0].ID != taskID {
		t.Fatalf("unexpected termination queue: %#v", imp.Task)
	}
	if _, err := ImplantRequestTermination(); !errors.Is(err, ErrTerminationPending) {
		t.Fatalf("duplicate termination returned %v", err)
	}
	if len(imp.Task) != 1 {
		t.Fatalf("duplicate termination queued %d tasks", len(imp.Task))
	}
	if err := ImplantDelete(); !errors.Is(err, ErrTerminationPending) {
		t.Fatalf("deletion before termination dispatch returned %v", err)
	}

	claimed, err := imp.taskClaimAt(time.Now())
	if err != nil || claimed.Code != internal.KILL {
		t.Fatalf("claim termination: task=%#v err=%v", claimed, err)
	}
	if err := ImplantDelete(); err != nil {
		t.Fatalf("delete after termination dispatch: %v", err)
	}
	if _, exists := ImplantMAP[imp.Name]; exists || CurrentImplant != "none" {
		t.Fatalf("deleted session remains: exists=%t current=%q", exists, CurrentImplant)
	}
}

func TestStaleSessionCanBeDeleted(t *testing.T) {
	isolateImplants(t)
	imp := ImplantNew("stale")
	imp.Metadata.Sleep = 1
	imp.LastSeen = time.Now().Add(-2 * time.Second)
	imp.ImplantAddImplant()
	CurrentImplant = imp.Name

	if err := ImplantDelete(); err != nil {
		t.Fatalf("delete stale session: %v", err)
	}
}

func TestSessionTypeIsAvailableToRoutingAndSuggestions(t *testing.T) {
	isolateImplants(t)
	imp := ImplantNew("typed")
	imp.Metadata.Type = "iot.v1"
	imp.Metadata.Hostname = "sensor"
	imp.Metadata.User = "root"
	imp.ImplantAddImplant()
	CurrentImplant = imp.Name

	if got := CurrentPayloadType(); got != "iot.v1" {
		t.Fatalf("current payload type = %q", got)
	}
	suggestions := ImplantListForSuggestions()
	if len(suggestions) != 1 || suggestions[0][0] != "typed" || suggestions[0][1] != "iot.v1 sensor@root" {
		t.Fatalf("session suggestions = %#v", suggestions)
	}
}
