package listener

import (
	"errors"
	"fmt"
	"purpcmd/server/db"
	"purpcmd/server/log"
	"strings"

	"github.com/cheynewallace/tabby"
	"github.com/google/uuid"
)

var (
	ListenerMAP            = make(map[string]*Listener)
	CurrentListener string = "none"
)

func ListenerNew(name string) error {
	if ListenerMAP[name] != nil {
		return errors.New("listener exists")
	}

	u := uuid.New()

	l := &Listener{
		Name: name,
		UUID: u.String(),
		Host: "0.0.0.0",
		Port: "4444",
		Persistent: true,
		SC: &ServerController{
			stopChan: make(chan struct{}),
			running:  false,
		},
	}

	ListenerMAP[name] = l
	CurrentListener = name

	err := db.DBListenerInsert(name, l.UUID, l.Host, l.Port, true, false)
	if err != nil {
		return err
	}

	log.PrintSuccs("New listener " + CurrentListener)

	return nil
}

func ListenerSetOptions(key, value string) error {
	if ListenerMAP[CurrentListener] == nil {
		return errors.New("no listener")
	}

	key = strings.ToLower(key)

	switch key {
	case "uuid":
		ListenerMAP[CurrentListener].UUID = value
	case "host":
		ListenerMAP[CurrentListener].Host = value
	case "port":
		ListenerMAP[CurrentListener].Port = value
	case "persist":
		var v bool
		if value == "t" || value == "true" || value == "on" {
			v = true
			db.DBListenerInsert(
				ListenerMAP[CurrentListener].Name, 
				ListenerMAP[CurrentListener].UUID, 
				ListenerMAP[CurrentListener].Host, 
				ListenerMAP[CurrentListener].Port, 
				true, 
				false,
			)
		} else if value == "f" || value == "false" || value == "off" {
			v = false
			db.DBListenerDelete(ListenerMAP[CurrentListener].Name)
		} else {
			return errors.New("what?")
		}
		ListenerMAP[CurrentListener].Persistent = v
	}

	return db.DBListenerUpdateOption(CurrentListener, key, value)
}

func ListenerShowOptions() error {
	if ListenerMAP[CurrentListener] == nil {
		return errors.New("no listener")
	}

	t := tabby.New()
	print("\n")
	println("Name: ", ListenerMAP[CurrentListener].Name)
	println("UUID: ", ListenerMAP[CurrentListener].UUID)
	t.AddHeader("OPTION", "VALUE", "DESCRIPTION")
	t.AddLine("Host", ListenerMAP[CurrentListener].Host, "Local host")
	t.AddLine("Port", ListenerMAP[CurrentListener].Port, "Local port")
	t.AddLine("Persist", fmt.Sprintf("%t", ListenerMAP[CurrentListener].Persistent), "Persist across startups")
	t.AddLine("Running", fmt.Sprintf("%t", ListenerMAP[CurrentListener].SC.running), "Is up")
	print("\n")
	t.Print()
	print("\n")

	return nil
}

func ListenerList() {
	if len(ListenerMAP) == 0 {
		log.PrintErr("No listener")
	}

	t := tabby.New()
	c := 1
	t.AddHeader("ID", "NAME", "UUID", "SOCKET", "RUNNING", "PERSISTENT", "ASSOCIATION")
	for k, v := range ListenerMAP {
		t.AddLine(c, k, v.UUID[24:], v.Host+":"+v.Port, fmt.Sprintf("%t", ListenerMAP[k].SC.running), fmt.Sprintf("%t", ListenerMAP[k].Persistent), v.Association)
		c += 1
	}
	print("\n")
	t.Print()
	print("\n")
}

func ListenerStart() {
	ListenerMAP[CurrentListener].StartHTTP()
}

func ListenerRestart() {
	ListenerMAP[CurrentListener].StopHTTP()
	ListenerMAP[CurrentListener].StartHTTP()
}

func ListenerStop() {
	ListenerMAP[CurrentListener].StopHTTP()
}

func ListenerInteract(name string) error {
	if ListenerMAP[name] == nil {
		return errors.New("no listener")
	}
	CurrentListener = name
	return nil
}

func ListenerDelete() error {
	if ListenerMAP[CurrentListener] != nil {
		if ListenerMAP[CurrentListener].SC.running {
			return errors.New("listener is running")
		}

		delete(ListenerMAP, CurrentListener)
		log.PrintSuccs("Listener " + CurrentListener + " deleted")
		CurrentListener = "none"
	} else {
		return errors.New("no listener")
	}
	return nil
}

func ListenerGetCurrentListener() string {
	return CurrentListener
}

func ListenerCount() int {
	return len(ListenerMAP)
}

func ListenerInitFromDB() error {
	list, err := db.DBListenerGetAll()
	if err != nil {
		return err
	}

	for i := range(list) {
		log.PrintInfo("Setting up listener ", list[i].Name)
		ListenerNew(list[i].Name)
		ListenerSetOptions("host", list[i].Host)
		ListenerSetOptions("port", list[i].Port)
		ListenerSetOptions("uuid", list[i].UUID)
		ListenerMAP[list[i].Name].Persistent = list[i].Persistent

		if list[i].Running {
			ListenerMAP[list[i].Name].StartHTTP()
		}
	}

	return nil
}
