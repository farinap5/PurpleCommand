package implant

import (
	"errors"
	"fmt"
	"purpcmd/server/log"
	"time"

	"github.com/cheynewallace/tabby"
	"github.com/google/uuid"
)

var (
	SEP = []byte{0x00} // Separator pattern for data like implant registering metadata
)

var (
	ImplantMAP = make(map[string]*Implant)
	CurrentImplant string = "none"
)

func (i *Implant)ImplantAddImplant() {
	log.AsyncWriteStdout(fmt.Sprintf("New implant %s - %s %s\n", i.Name, i.Metadata.Hostname, i.Metadata.User))
	//fmt.Println(i)
	ImplantMAP[i.Name] = i
}

func ImplantNew(name, key string) *Implant {
	n := time.Now()
	return &Implant{
		Name: name,
		UUID: uuid.NewString(),
		key: key,
		Alive: true,
		LastSeen: n,
		FirstSeen: n,
	}
}

func (i *Implant)ImplantSetMetadata(m *ImplantMetadata) {
	i.Metadata = *m
}

func ImplantList() {
	if len(ImplantMAP) == 0 {
		println("no session")
	}

	t := tabby.New()
	c := 1
	t.AddHeader("N", "NAME", "UUID", "PID", "LAST SEEN", "STATUS")
	for k,v := range ImplantMAP {

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
		status := "healthy"
		if time.Since(v.LastSeen).Seconds() > float64(v.Sleep) {
			status = "dead"
		}

		t.AddLine(c ,k, v.UUID[24:], v.Metadata.PID, fmt.Sprintf("%d%s ago", lastS, aux), status)
		c+=1
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
		println("Session " + CurrentImplant + " deleted")
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

func (i *Implant)ImplantSetAlive() {
	if !i.Alive {
		i.Alive = true
	}
}

