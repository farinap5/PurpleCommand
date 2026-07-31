package implant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"purpcmd/implant"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/server/log"
	"sync"
	"time"

	"github.com/cheynewallace/tabby"
	"github.com/google/uuid"
)

var (
	ImplantMAP            = make(map[string]*Implant)
	CurrentImplant string = "none"

	ErrNoCurrentImplant   = errors.New("no session is selected")
	ErrImplantAlive       = errors.New("session is alive; run `delete terminate` to request implant termination")
	ErrTerminationPending = errors.New("implant termination is pending; wait for its next check-in before deleting")
	ErrImplantNotAlive    = errors.New("session is not alive; run `delete` to remove it")
)

func (i *Implant) ImplantAddImplant() {
	/*if CurrentImplant == "none" {
		CurrentImplant = i.Name
	}*/
	ImplantMAP[i.Name] = i
}

func ImplantNew(name string) *Implant {
	n := time.Now()
	return &Implant{
		Name:      name,
		UUID:      uuid.NewString(),
		Alive:     true,
		LastSeen:  n,
		FirstSeen: n,
		TaskMap:   make(map[[8]byte]*Task),
		taskMu:    &sync.Mutex{},
	}
}

func (i *Implant) ImplantSetEncryption(enc encrypt.Encrypt) {
	i.Enc = enc
}

func (i *Implant) ImplantSetMetadata(m *implant.ImplantMetadata) {
	i.Metadata = *m
}

func ImplantList() {
	if len(ImplantMAP) == 0 {
		log.PrintAlert("no session")
	}

	t := tabby.New()
	c := 1
	t.AddHeader("N", "NAME", "TYPE", "USERNAME", "MACHINE", "UUID", "SOCKET", "PID", "SLEEP", "LAST SEEN", "STATUS")
	now := time.Now()
	for k, v := range ImplantMAP {
		alive, terminating, lastSeen := v.implantLifecycleAt(now)

		lastS := int(now.Sub(lastSeen).Seconds())
		if lastS < 0 {
			lastS = 0
		}
		aux := "s"
		if lastS > 360 {
			lastS = int(now.Sub(lastSeen).Minutes())
			aux = "m"
			if lastS > 360 {
				lastS = int(now.Sub(lastSeen).Hours())
				aux = "h"
			}
		}
		status := "\u001B[1;32mhealthy\u001B[0;0m"
		if terminating && alive {
			status = "\u001B[1;33mterminating\u001B[0;0m"
		} else if !alive {
			status = "\u001B[1;31mdead\u001B[0;0m"
		}

		t.AddLine(c, k, v.Metadata.Type, v.Metadata.User, v.Metadata.Hostname, v.UUID[24:], v.Metadata.Socket, v.Metadata.PID, v.Metadata.Sleep, fmt.Sprintf("%d%s ago", lastS, aux), status)
		c += 1
	}
	print("\n")
	t.Print()
	print("\n")
}

func ImplantDelete() error {
	name := CurrentImplant
	if name == "none" {
		return ErrNoCurrentImplant
	}
	imp := ImplantMAP[name]
	if imp == nil {
		return ErrNoCurrentImplant
	}

	mu := imp.taskMutex()
	mu.Lock()
	imp.refreshAliveLocked(time.Now())
	if imp.Alive {
		terminating := imp.Terminating
		mu.Unlock()
		if terminating {
			return ErrTerminationPending
		}
		return ErrImplantAlive
	}
	mu.Unlock()

	delete(ImplantMAP, name)
	log.PrintSuccs("Session " + name + " deleted")
	CurrentImplant = "none"
	return nil
}

// ImplantRequestTermination queues a single KILL task for the selected live
// implant. The session remains present until the task is dispatched, ensuring
// deletion cannot discard the termination request before the implant sees it.
func ImplantRequestTermination() ([8]byte, error) {
	name := CurrentImplant
	if name == "none" || ImplantMAP[name] == nil {
		return [8]byte{}, ErrNoCurrentImplant
	}

	imp := ImplantMAP[name]
	mu := imp.taskMutex()
	mu.Lock()
	defer mu.Unlock()

	imp.refreshAliveLocked(time.Now())
	if !imp.Alive {
		return [8]byte{}, ErrImplantNotAlive
	}
	for _, task := range imp.Task {
		if task.Code == internal.KILL && !task.Done {
			imp.Terminating = true
			return task.ID, ErrTerminationPending
		}
	}

	task := TaskNew(internal.KILL, nil)
	imp.Task = append(imp.Task, task)
	imp.TaskMap[task.ID] = task
	imp.Terminating = true
	log.PrintInfo("implant termination task added: ", string(task.ID[:]))
	return task.ID, nil
}

func ImplantInteract(name string) error {
	if ImplantMAP[name] == nil {
		return errors.New("no implant")
	}
	CurrentImplant = name
	return nil
}

func (i *Implant) ImplantSetAlive() {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()
	if !i.Terminating {
		i.Alive = true
	}
}

func (i *Implant) ImplantSetRemoteSocket(socket string) {
	i.Metadata.Socket = socket
}

func ImplantPtrByName(name string) *Implant {
	return ImplantMAP[name]
}

func (i *Implant) ImplantUpdateLastseen() {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()
	i.LastSeen = time.Now()
	if !i.Terminating {
		i.Alive = true
	}
}

func (i *Implant) refreshAliveLocked(now time.Time) {
	if !i.Alive || i.Metadata.Sleep == 0 {
		return
	}
	if now.Sub(i.LastSeen) > time.Duration(i.Metadata.Sleep)*time.Second {
		i.Alive = false
	}
}

func (i *Implant) implantLifecycleAt(now time.Time) (alive, terminating bool, lastSeen time.Time) {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()
	i.refreshAliveLocked(now)
	return i.Alive, i.Terminating, i.LastSeen
}

func ImplantCount() int {
	return len(ImplantMAP)
}

func ImplantAddTask() {
	if CurrentImplant == "none" {
		return
	}
	t := TaskNew(0x01, []byte("ping"))
	ImplantMAP[CurrentImplant].ImplantAddTask(t)
}

func ImplantAddGenericTask(code int, payload string) (string, int) {
	if CurrentImplant == "none" {
		return "", 1
	}
	t := TaskNew(uint16(code), []byte(payload))
	ImplantMAP[CurrentImplant].ImplantAddTask(t)
	return string(t.ID[:]), 0
}

func ImplantAddUploadTask(code int, name string, data []byte) int {
	if CurrentImplant == "none" {
		return 1
	}

	var Buff []byte
	nameLen := uint16(len(name))
	nameLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(nameLenBytes, nameLen)
	Buff = append(Buff, nameLenBytes...)

	Buff = append(Buff, []byte(name)...)

	dataLen := uint32(len(data))
	dataLenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(dataLenBytes, dataLen)
	Buff = append(Buff, dataLenBytes...)

	// Write data
	Buff = append(Buff, data...)

	t := TaskNew(uint16(code), Buff)
	ImplantMAP[CurrentImplant].ImplantAddTask(t)
	return 0
}

func (i *Implant) ImplantAddTask(task *Task) {
	mu := i.taskMutex()
	mu.Lock()
	defer mu.Unlock()

	i.pruneCompletedTasksLocked(time.Now())
	i.Task = append(i.Task, task)
	i.TaskMap[task.ID] = task
	if task.Code == internal.KILL {
		i.Terminating = true
	}
	log.PrintInfo("new task added: ", string(task.ID[:]))
}

func (i *Implant) ImplantGetTaskStr() (string, [8]byte, error) {
	t, err := i.taskClaimAt(time.Now())
	if err != nil {
		return "", [8]byte{}, err
	}

	tb := t.TaskMarshal()
	tbe := i.Enc.AESCbcEncrypt(tb)
	i.Enc.HMACPackAddHmac(&tbe)
	return TaskEncode(tbe), t.ID, nil
}

func ImplantListForSuggestions() [][]string {
	var suggestions [][]string
	for k, v := range ImplantMAP {
		description := v.Metadata.Type + " " + v.Metadata.Hostname + "@" + v.Metadata.User
		suggestions = append(suggestions, []string{k, description})
	}
	return suggestions
}

func CurrentPayloadType() string {
	imp := ImplantMAP[CurrentImplant]
	if imp == nil {
		return ""
	}
	return imp.Metadata.Type
}

// ImplantGetType is retained for compatibility. New code should use
// CurrentPayloadType to make clear that this is a command-routing identifier.
func ImplantGetType() string {
	return CurrentPayloadType()
}
