package implant

import (
	"time"

	"github.com/google/uuid"
)

var (
	ImplantMAP = make(map[string]*Implant)
	CurrentImplant string = "none"
)

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

func (i *Implant)SetAlive() {
	if !i.Alive {
		i.Alive = true
	}
}