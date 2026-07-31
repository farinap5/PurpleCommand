package implant

import (
	"testing"
	"time"
)

func TestTaskDeliveryRetriesUntilCompleted(t *testing.T) {
	imp := ImplantNew("retry-test")
	imp.Metadata.Sleep = 10
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	task := &Task{
		ID:         [8]byte{'t', 'a', 's', 'k', '0', '0', '0', '1'},
		Code:       1,
		Payload:    []byte("ping"),
		Registered: now,
	}
	imp.ImplantAddTask(task)

	first, err := imp.taskClaimAt(now)
	if err != nil {
		t.Fatalf("first task claim failed: %v", err)
	}
	if first.ID != task.ID || first.Attempts != 1 {
		t.Fatalf("unexpected first claim: id=%q attempts=%d", first.ID, first.Attempts)
	}

	if _, err := imp.taskClaimAt(now.Add(imp.taskRetryInterval() - time.Nanosecond)); err == nil {
		t.Fatal("task was redelivered before its lease expired")
	}

	retry, err := imp.taskClaimAt(now.Add(imp.taskRetryInterval()))
	if err != nil {
		t.Fatalf("task was not retried after its lease expired: %v", err)
	}
	if retry.ID != task.ID || retry.Attempts != 2 {
		t.Fatalf("retry did not preserve task identity: id=%q attempts=%d", retry.ID, retry.Attempts)
	}

	accepted, err := imp.TaskBeginResponse(task.ID)
	if err != nil || !accepted {
		t.Fatalf("response was not accepted: accepted=%t err=%v", accepted, err)
	}
	accepted, err = imp.TaskBeginResponse(task.ID)
	if err != nil || accepted {
		t.Fatalf("duplicate in-progress response was accepted: accepted=%t err=%v", accepted, err)
	}
	if err := imp.TaskCompleteResponse(task.ID, []byte("pong")); err != nil {
		t.Fatalf("complete response: %v", err)
	}

	accepted, err = imp.TaskBeginResponse(task.ID)
	if err != nil || accepted {
		t.Fatalf("completed task accepted another response: accepted=%t err=%v", accepted, err)
	}
	if _, err := imp.taskClaimAt(now.Add(2 * imp.taskRetryInterval())); err == nil {
		t.Fatal("completed task was delivered again")
	}
}

func TestTaskAbortAllowsResponseRetry(t *testing.T) {
	imp := ImplantNew("abort-test")
	task := TaskNew(1, nil)
	imp.ImplantAddTask(task)

	accepted, err := imp.TaskBeginResponse(task.ID)
	if err != nil || !accepted {
		t.Fatalf("begin response: accepted=%t err=%v", accepted, err)
	}
	imp.TaskAbortResponse(task.ID)
	accepted, err = imp.TaskBeginResponse(task.ID)
	if err != nil || !accepted {
		t.Fatalf("aborted response could not be retried: accepted=%t err=%v", accepted, err)
	}
}

func TestCompletedTasksArePruned(t *testing.T) {
	imp := ImplantNew("prune-test")
	old := TaskNew(1, nil)
	old.Done = true
	old.ResponseTime = time.Now().Add(-completedTaskRetention - time.Minute)
	imp.ImplantAddTask(old)
	imp.ImplantAddTask(TaskNew(1, nil))

	if _, exists := imp.TaskMap[old.ID]; exists {
		t.Fatal("expired completed task remained in task map")
	}
	for _, task := range imp.Task {
		if task.ID == old.ID {
			t.Fatal("expired completed task remained in task queue")
		}
	}

	if _, err := imp.TaskBeginResponse([8]byte{'m', 'i', 's', 's', 'i', 'n', 'g'}); err == nil || err.Error() != "no task with given id" {
		t.Fatalf("missing task returned unexpected error: %v", err)
	}
}
