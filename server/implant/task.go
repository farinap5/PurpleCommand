package implant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"purpcmd/internal"
	"purpcmd/server"
)

const (
	minimumTaskRetryInterval = 15 * time.Second
	maximumTaskRetryInterval = 5 * time.Minute
	completedTaskRetention   = 24 * time.Hour
)

func (i *Implant) taskMutex() *sync.Mutex {
	if i.taskMu == nil {
		i.taskMu = &sync.Mutex{}
	}
	return i.taskMu
}

func (i *Implant) taskRetryInterval() time.Duration {
	retryAfter := 2 * time.Duration(i.Metadata.Sleep) * time.Second
	if retryAfter < minimumTaskRetryInterval {
		return minimumTaskRetryInterval
	}
	if retryAfter > maximumTaskRetryInterval {
		return maximumTaskRetryInterval
	}
	return retryAfter
}

// taskClaimAt leases the next unfinished task for delivery. If a previous
// delivery was not completed before the lease expires, the same task ID is
// returned again so a dropped HTTP response does not strand the task forever.
func (i *Implant) taskClaimAt(now time.Time) (*Task, error) {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()

	i.pruneCompletedTasksLocked(now)
	retryAfter := i.taskRetryInterval()
	for _, t := range i.Task {
		if t.Done || t.Processing {
			continue
		}
		if !t.Sent || !now.Before(t.LastSent.Add(retryAfter)) {
			t.Sent = true
			t.LastSent = now
			t.Attempts++
			if t.Code == internal.KILL {
				i.Terminating = true
				i.Alive = false
			}

			// Return a snapshot so callers can marshal without holding the lock.
			claimed := *t
			claimed.Payload = append([]byte(nil), t.Payload...)
			return &claimed, nil
		}
	}
	return nil, errors.New("no pending task")
}

// TaskBeginResponse atomically reserves an unfinished task while its response
// is persisted. It returns false for a duplicate response that was already
// completed or is currently being processed.
func (i *Implant) TaskBeginResponse(taskID [8]byte) (bool, error) {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()

	task := i.TaskMap[taskID]
	if task == nil {
		return false, errors.New("no task with given id")
	}
	if task.Done || task.Processing {
		return false, nil
	}
	task.Processing = true
	return true, nil
}

func (i *Implant) TaskCompleteResponse(taskID [8]byte, payload []byte) error {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()

	task := i.TaskMap[taskID]
	if task == nil {
		return errors.New("no task with given id")
	}
	task.ResponseTime = time.Now()
	task.Done = true
	task.Processing = false
	task.Response = append([]byte(nil), payload...)
	return nil
}

func (i *Implant) TaskAbortResponse(taskID [8]byte) {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()

	if task := i.TaskMap[taskID]; task != nil && !task.Done {
		task.Processing = false
	}
}

func (i *Implant) pruneCompletedTasksLocked(now time.Time) {
	kept := i.Task[:0]
	for _, task := range i.Task {
		if task.Done && !task.ResponseTime.IsZero() && now.Sub(task.ResponseTime) >= completedTaskRetention {
			delete(i.TaskMap, task.ID)
			continue
		}
		kept = append(kept, task)
	}
	i.Task = kept
}

func (t Task) TaskMarshal() []byte {
	b := new(bytes.Buffer)

	binary.Write(b, binary.BigEndian, t.Code)
	binary.Write(b, binary.BigEndian, t.ID)
	binary.Write(b, binary.BigEndian, uint32(len(t.Payload)))
	binary.Write(b, binary.BigEndian, t.Payload)

	return b.Bytes()
}

func TaskEncode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func TaskNew(code uint16, payload []byte) *Task {
	return &Task{
		ID:         server.RandomAlphanumericID8(),
		Code:       code,
		Registered: time.Now(),
		Payload:    payload,
	}
}
