package implant

import (
	"testing"

	"purpcmd/server/db"
)

func TestSessionAndTaskHistoryRestoresAsInactive(t *testing.T) {
	db.DatabasePath = t.TempDir() + "/sessions.db"
	if err := db.CheckDB(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DBMS.DBConn.Close() })
	if err := db.EnsureTeamserverSchema(); err != nil {
		t.Fatal(err)
	}

	implantMapMu.Lock()
	previous := ImplantMAP
	ImplantMAP = make(map[string]*Implant)
	implantMapMu.Unlock()
	t.Cleanup(func() {
		implantMapMu.Lock()
		ImplantMAP = previous
		implantMapMu.Unlock()
	})

	item := ImplantNew("persisted")
	item.Metadata.Type = "impl"
	item.Metadata.Hostname = "host"
	item.Metadata.User = "user"
	item.Metadata.Sleep = 60
	item.ImplantSetSpeaker("bind-http")
	item.ImplantAddImplant()
	task := TaskNew(1, []byte("ping"))
	item.ImplantAddTask(task)
	if err := item.TaskCompleteResponse(task.ID, []byte("pong")); err != nil {
		t.Fatal(err)
	}

	implantMapMu.Lock()
	ImplantMAP = make(map[string]*Implant)
	implantMapMu.Unlock()
	if err := RestoreFromDB(); err != nil {
		t.Fatal(err)
	}
	session, err := APIGetSession("persisted")
	if err != nil {
		t.Fatal(err)
	}
	if session.Alive {
		t.Fatal("restored session was marked alive without restored transport keys")
	}
	if session.Transport != "speaker" || session.Speaker != "bind-http" {
		t.Fatalf("restored session route = %q/%q", session.Transport, session.Speaker)
	}
	restoredTask, err := APIGetTask("persisted", string(task.ID[:]))
	if err != nil {
		t.Fatal(err)
	}
	if restoredTask.Status != "completed" || string(restoredTask.Response) != "pong" {
		t.Fatalf("restored task = %#v", restoredTask)
	}
	if err := APIDeleteSession("persisted"); err != nil {
		t.Fatal(err)
	}
}
