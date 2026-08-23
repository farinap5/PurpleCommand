package implant

import (
	"errors"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
)

func APIListSessions() []teamapi.Session {
	implantMapMu.RLock()
	implants := make([]*Implant, 0, len(ImplantMAP))
	for _, implant := range ImplantMAP {
		implants = append(implants, implant)
	}
	implantMapMu.RUnlock()

	result := make([]teamapi.Session, 0, len(implants))
	for _, implant := range implants {
		result = append(result, implant.apiSession())
	}
	return result
}

func APIGetSession(name string) (teamapi.Session, error) {
	implantMapMu.RLock()
	implant := ImplantMAP[name]
	implantMapMu.RUnlock()
	if implant == nil {
		return teamapi.Session{}, ErrNoCurrentImplant
	}
	return implant.apiSession(), nil
}

func (implant *Implant) apiSession() teamapi.Session {
	alive, terminating, lastSeen := implant.implantLifecycleAt(time.Now())
	transport, speaker := implant.sessionRoute()
	return teamapi.Session{
		Name:        implant.Name,
		UUID:        implant.UUID,
		PayloadType: implant.Metadata.Type,
		Transport:   transport,
		Speaker:     speaker,
		User:        implant.Metadata.User,
		Hostname:    implant.Metadata.Hostname,
		Process:     implant.Metadata.Proc,
		Socket:      implant.Metadata.Socket,
		PID:         implant.Metadata.PID,
		Sleep:       implant.Metadata.Sleep,
		Alive:       alive,
		Terminating: terminating,
		FirstSeen:   implant.FirstSeen,
		LastSeen:    lastSeen,
	}
}

func APICreateTask(request teamapi.TaskCreateRequest) (teamapi.Task, error) {
	implantMapMu.RLock()
	implant := ImplantMAP[request.Session]
	implantMapMu.RUnlock()
	if implant == nil {
		return teamapi.Task{}, ErrNoCurrentImplant
	}
	task := TaskNew(request.Code, append([]byte(nil), request.Payload...))
	implant.ImplantAddTask(task)
	return taskDTO(request.Session, task), nil
}

func APIListTasks(session string) ([]teamapi.Task, error) {
	implantMapMu.RLock()
	implant := ImplantMAP[session]
	implantMapMu.RUnlock()
	if implant == nil {
		return nil, ErrNoCurrentImplant
	}
	mutex := implant.taskMutex()
	mutex.Lock()
	defer mutex.Unlock()
	result := make([]teamapi.Task, 0, len(implant.Task))
	for _, task := range implant.Task {
		result = append(result, taskDTO(session, task))
	}
	return result, nil
}

func APIGetTask(session, taskID string) (teamapi.Task, error) {
	if len(taskID) != 8 {
		return teamapi.Task{}, errors.New("task ID must be 8 bytes")
	}
	var id [8]byte
	copy(id[:], taskID)
	implantMapMu.RLock()
	implant := ImplantMAP[session]
	implantMapMu.RUnlock()
	if implant == nil {
		return teamapi.Task{}, ErrNoCurrentImplant
	}
	mutex := implant.taskMutex()
	mutex.Lock()
	defer mutex.Unlock()
	task := implant.TaskMap[id]
	if task == nil {
		return teamapi.Task{}, errors.New("task not found")
	}
	return taskDTO(session, task), nil
}

func taskDTO(session string, task *Task) teamapi.Task {
	status := "queued"
	if task.Done {
		status = "completed"
	} else if task.Processing {
		status = "processing"
	} else if task.Sent {
		status = "dispatched"
	}
	return teamapi.Task{
		ID:           string(task.ID[:]),
		Session:      session,
		Code:         task.Code,
		Status:       status,
		Attempts:     task.Attempts,
		Registered:   task.Registered,
		LastSent:     task.LastSent,
		ResponseTime: task.ResponseTime,
		Response:     append([]byte(nil), task.Response...),
	}
}

func APIRequestTermination(name string) (teamapi.Task, error) {
	implantMapMu.RLock()
	implant := ImplantMAP[name]
	implantMapMu.RUnlock()
	if implant == nil {
		return teamapi.Task{}, ErrNoCurrentImplant
	}
	mutex := implant.taskMutex()
	mutex.Lock()
	implant.refreshAliveLocked(time.Now())
	if !implant.Alive {
		mutex.Unlock()
		return teamapi.Task{}, ErrImplantNotAlive
	}
	for _, task := range implant.Task {
		if task.Code == 5 && !task.Done {
			implant.Terminating = true
			result := taskDTO(name, task)
			mutex.Unlock()
			persistSession(implant)
			return result, ErrTerminationPending
		}
	}
	task := TaskNew(5, nil)
	implant.Task = append(implant.Task, task)
	implant.TaskMap[task.ID] = task
	implant.Terminating = true
	result := taskDTO(name, task)
	mutex.Unlock()
	markTaskCreated(implant, task)
	persistSession(implant)
	implant.signalTaskReady()
	return result, nil
}

func APIDeleteSession(name string) error {
	implantMapMu.Lock()
	implant := ImplantMAP[name]
	if implant == nil {
		implantMapMu.Unlock()
		return ErrNoCurrentImplant
	}
	mutex := implant.taskMutex()
	mutex.Lock()
	implant.refreshAliveLocked(time.Now())
	if implant.Alive {
		terminating := implant.Terminating
		mutex.Unlock()
		implantMapMu.Unlock()
		if terminating {
			return ErrTerminationPending
		}
		return ErrImplantAlive
	}
	mutex.Unlock()
	if err := db.DBSessionDelete(name); err != nil {
		implantMapMu.Unlock()
		return err
	}
	delete(ImplantMAP, name)
	if CurrentImplant == name {
		CurrentImplant = "none"
	}
	implantMapMu.Unlock()
	return nil
}
