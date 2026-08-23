package implant

import (
	"encoding/json"
	"fmt"

	implantwire "purpcmd/implant"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/runtimeevents"
)

func persistSession(item *Implant) {
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return
	}
	_ = db.DBSessionSave(item.apiSession(), metadata)
}

func saveTask(item *Implant, task *Task) {
	_ = db.DBTaskSave(taskDTO(item.Name, task), append([]byte(nil), task.Payload...))
}

func emit(eventType string, value any) {
	runtimeevents.Publish(eventType, value)
}

func RestoreFromDB() error {
	sessions, err := db.DBSessionList()
	if err != nil {
		return err
	}
	for _, stored := range sessions {
		var metadata implantwire.ImplantMetadata
		if err := json.Unmarshal(stored.Metadata, &metadata); err != nil {
			return fmt.Errorf("restore session %s: %w", stored.Session.Name, err)
		}
		item := &Implant{
			Name:        stored.Session.Name,
			UUID:        stored.Session.UUID,
			Metadata:    metadata,
			Transport:   stored.Session.Transport,
			Speaker:     stored.Session.Speaker,
			Alive:       false,
			Terminating: stored.Session.Terminating,
			FirstSeen:   stored.Session.FirstSeen,
			LastSeen:    stored.Session.LastSeen,
			TaskMap:     make(map[[8]byte]*Task),
			taskReady:   make(chan struct{}, 1),
		}
		if item.Transport == "" {
			item.Transport = teamapi.SessionTransportListener
		}
		tasks, err := db.DBTaskList(item.Name)
		if err != nil {
			return err
		}
		for _, storedTask := range tasks {
			var id [8]byte
			copy(id[:], []byte(storedTask.Task.ID))
			task := &Task{
				ID:           id,
				Code:         storedTask.Task.Code,
				Payload:      append([]byte(nil), storedTask.Payload...),
				Attempts:     storedTask.Task.Attempts,
				Registered:   storedTask.Task.Registered,
				LastSent:     storedTask.Task.LastSent,
				ResponseTime: storedTask.Task.ResponseTime,
				Response:     append([]byte(nil), storedTask.Task.Response...),
			}
			switch storedTask.Task.Status {
			case "completed":
				task.Sent = true
				task.Done = true
			case "processing":
				task.Sent = true
			case "dispatched":
				task.Sent = true
			}
			item.Task = append(item.Task, task)
			item.TaskMap[id] = task
		}
		item.taskMutex()
		if item.Transport == teamapi.SessionTransportSpeaker {
			for _, task := range item.Task {
				if !task.Done {
					item.signalTaskReady()
					break
				}
			}
		}
		implantMapMu.Lock()
		if ImplantMAP[item.Name] == nil {
			ImplantMAP[item.Name] = item
		}
		implantMapMu.Unlock()
		persistSession(item)
	}
	return nil
}

func markSessionRegistered(item *Implant) {
	persistSession(item)
	emit(teamapi.EventSessionRegistered, item.apiSession())
}

func markSessionCheckin(item *Implant) {
	persistSession(item)
	emit(teamapi.EventSessionCheckin, item.apiSession())
}

func markTaskCreated(item *Implant, task *Task) {
	saveTask(item, task)
	emit(teamapi.EventTaskCreated, taskDTO(item.Name, task))
}

func markTaskDispatched(item *Implant, task *Task) {
	saveTask(item, task)
	emit(teamapi.EventTaskDispatched, taskDTO(item.Name, task))
}

func markTaskCompleted(item *Implant, task *Task) {
	saveTask(item, task)
	emit(teamapi.EventTaskCompleted, taskDTO(item.Name, task))
}
