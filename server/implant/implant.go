package implant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"purpcmd/implant"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/pkg/teamapi"
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
	implantMapMu          sync.RWMutex
)

func (i *Implant) ImplantAddImplant() {
	implantMapMu.Lock()
	ImplantMAP[i.Name] = i
	implantMapMu.Unlock()
	markSessionRegistered(i)
}

func ImplantNew(name string) *Implant {
	n := time.Now()
	return &Implant{
		Name:             name,
		UUID:             uuid.NewString(),
		Transport:        teamapi.SessionTransportListener,
		Alive:            true,
		LastSeen:         n,
		FirstSeen:        n,
		TaskMap:          make(map[[8]byte]*Task),
		taskMu:           &sync.Mutex{},
		taskReady:        make(chan struct{}, 1),
		HealthMonitoring: true,
	}
}

// ImplantSetSpeaker routes this session's queued tasks through the named
// speaker. The session remains in the normal implant registry so command and
// Lua task handling do not need a speaker-specific path.
func (i *Implant) ImplantSetSpeaker(name string, ids ...string) {
	id := ""
	if len(ids) > 0 {
		id = ids[0]
	}
	mu := i.taskMutex()
	mu.Lock()
	i.Transport = teamapi.SessionTransportSpeaker
	i.Speaker = name
	i.SpeakerUUID = id
	i.Listener = ""
	i.ListenerUUID = ""
	pending := false
	for _, task := range i.Task {
		if !task.Done && !task.Processing {
			pending = true
			break
		}
	}
	mu.Unlock()
	persistSession(i)
	if pending {
		i.signalTaskReady()
	}
}

// ImplantRenameSpeaker refreshes the display name for sessions owned by a
// stable speaker UUID. Routing never relies on the mutable name.
func ImplantRenameSpeaker(id, previousName, newName string) {
	implantMapMu.RLock()
	items := make([]*Implant, 0, len(ImplantMAP))
	for _, item := range ImplantMAP {
		items = append(items, item)
	}
	implantMapMu.RUnlock()
	for _, item := range items {
		mu := item.taskMutex()
		mu.Lock()
		owned := item.Transport == teamapi.SessionTransportSpeaker &&
			((id != "" && item.SpeakerUUID == id) || (item.SpeakerUUID == "" && item.Speaker == previousName))
		if owned {
			item.Speaker = newName
		}
		mu.Unlock()
		if owned {
			persistSession(item)
		}
	}
}

func (i *Implant) ImplantSetHealthMonitoring(enabled bool) {
	mu := i.taskMutex()
	mu.Lock()
	i.HealthMonitoring = enabled
	mu.Unlock()
	persistSession(i)
}

func (i *Implant) ImplantSetUnavailable() {
	mu := i.taskMutex()
	mu.Lock()
	i.Alive = false
	mu.Unlock()
	persistSession(i)
}

// ImplantSetListener records the listener instance that accepted this
// session. The listener UUID remains stable if the display name later changes.
func (i *Implant) ImplantSetListener(name, id string) {
	mu := i.taskMutex()
	mu.Lock()
	i.Transport = teamapi.SessionTransportListener
	i.Speaker = ""
	i.SpeakerUUID = ""
	i.HealthMonitoring = true
	i.Listener = name
	i.ListenerUUID = id
	mu.Unlock()
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
	return APIDeleteSession(CurrentImplant)
}

// ImplantRequestTermination queues a single KILL task for the selected live
// implant. The session remains present until the task is dispatched, ensuring
// deletion cannot discard the termination request before the implant sees it.
func ImplantRequestTermination() ([8]byte, error) {
	task, err := APIRequestTermination(CurrentImplant)
	var id [8]byte
	copy(id[:], task.ID)
	return id, err
}

func ImplantInteract(name string) error {
	implantMapMu.RLock()
	implant := ImplantMAP[name]
	implantMapMu.RUnlock()
	if implant == nil {
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
	implantMapMu.RLock()
	defer implantMapMu.RUnlock()
	return ImplantMAP[name]
}

func (i *Implant) ImplantUpdateLastseen() {
	mu := i.taskMutex()
	mu.Lock()
	i.LastSeen = time.Now()
	if !i.Terminating {
		i.Alive = true
	}
	mu.Unlock()
	markSessionCheckin(i)
}

func (i *Implant) refreshAliveLocked(now time.Time) {
	// Reverse sessions advertise their callback cadence in Metadata.Sleep.
	// Speaker sessions instead use the worker's independently configured
	// health interval and failure threshold; the worker marks them unavailable
	// explicitly, so applying the reverse timeout here would create false
	// failures whenever that interval exceeds the implant sleep value.
	if !i.Alive || i.Metadata.Sleep == 0 || i.Transport == teamapi.SessionTransportSpeaker {
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
	implantMapMu.RLock()
	defer implantMapMu.RUnlock()
	return len(ImplantMAP)
}

func ImplantAddTask() {
	if CurrentImplant == "none" {
		return
	}
	t := TaskNew(0x01, []byte("ping"))
	if implant := ImplantPtrByName(CurrentImplant); implant != nil {
		implant.ImplantAddTask(t)
	}
}

func ImplantAddGenericTask(code int, payload string) (string, int) {
	return ImplantAddGenericTaskFor(CurrentImplant, code, payload)
}

func ImplantAddGenericTaskFor(session string, code int, payload string) (string, int) {
	if session == "" || session == "none" {
		return "", 1
	}
	t := TaskNew(uint16(code), []byte(payload))
	target := ImplantPtrByName(session)
	if target == nil {
		return "", 1
	}
	target.ImplantAddTask(t)
	return string(t.ID[:]), 0
}

func ImplantAddUploadTask(code int, name string, data []byte) int {
	_, result := ImplantAddUploadTaskFor(CurrentImplant, code, name, data)
	return result
}

func ImplantAddUploadTaskFor(session string, code int, name string, data []byte) (string, int) {
	if session == "" || session == "none" {
		return "", 1
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
	target := ImplantPtrByName(session)
	if target == nil {
		return "", 1
	}
	target.ImplantAddTask(t)
	return string(t.ID[:]), 0
}

func (i *Implant) ImplantAddTask(task *Task) {
	mu := i.taskMutex()
	mu.Lock()
	i.pruneCompletedTasksLocked(time.Now())
	i.Task = append(i.Task, task)
	i.TaskMap[task.ID] = task
	if task.Code == internal.KILL {
		i.Terminating = true
	}
	mu.Unlock()
	markTaskCreated(i, task)
	persistSession(i)
	i.signalTaskReady()
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
	implantMapMu.RLock()
	defer implantMapMu.RUnlock()
	var suggestions [][]string
	for k, v := range ImplantMAP {
		description := v.Metadata.Type + " " + v.Metadata.Hostname + "@" + v.Metadata.User
		suggestions = append(suggestions, []string{k, description})
	}
	return suggestions
}

func CurrentPayloadType() string {
	imp := ImplantPtrByName(CurrentImplant)
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
