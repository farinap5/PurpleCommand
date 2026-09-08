package implant

import (
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"purpcmd/internal"
	"purpcmd/internal/protocol"
	"purpcmd/pkg/teamapi"
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

func (i *Implant) taskReadyChannel() chan struct{} {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()
	if i.taskReady == nil {
		i.taskReady = make(chan struct{}, 1)
	}
	return i.taskReady
}

// TaskReady reports that this session has work available for delivery. The
// signal is edge-triggered and coalesced; consumers must inspect the task queue
// and own any retry timing. Listener-backed sessions may safely ignore it.
func (i *Implant) TaskReady() <-chan struct{} {
	return i.taskReadyChannel()
}

func (i *Implant) signalTaskReady() {
	ready := i.taskReadyChannel()
	select {
	case ready <- struct{}{}:
	default:
	}
}

func (i *Implant) taskRetryInterval() time.Duration {
	if i.taskRetry > 0 {
		return i.taskRetry
	}
	retryAfter := 2 * time.Duration(i.Metadata.Sleep) * time.Second
	if retryAfter < minimumTaskRetryInterval {
		return minimumTaskRetryInterval
	}
	if retryAfter > maximumTaskRetryInterval {
		return maximumTaskRetryInterval
	}
	return retryAfter
}

func (i *Implant) ImplantSetTaskRetryInterval(interval time.Duration) {
	mu := i.taskMutex()
	mu.Lock()
	if interval > 0 {
		i.taskRetry = interval
	}
	mu.Unlock()
}

// NextTaskDeliveryDelay reports when the next unfinished task may be claimed.
// It lets speaker workers drain coalesced task notifications without changing
// the listener lease and retry semantics.
func (i *Implant) NextTaskDeliveryDelay(now time.Time) (time.Duration, bool) {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()

	retryAfter := i.taskRetryInterval()
	var earliest time.Duration
	found := false
	for _, task := range i.Task {
		if task.Done || task.Processing || (task.Code == internal.KILL && task.Sent) {
			continue
		}
		delay := time.Duration(0)
		if task.Sent {
			delay = time.Until(task.LastSent.Add(retryAfter))
			if !now.IsZero() {
				delay = task.LastSent.Add(retryAfter).Sub(now)
			}
			if delay < 0 {
				delay = 0
			}
		}
		if !found || delay < earliest {
			earliest = delay
			found = true
		}
	}
	return earliest, found
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
			saveTask(i, t)
			emit(teamapi.EventTaskDispatched, taskDTO(i.Name, t))
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
	markTaskCompleted(i, task)
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
	return protocol.EncodeTask(t.Code, t.ID, t.Payload)
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
