package listener

import (
	"errors"
	"fmt"
	"strings"

	"purpcmd/server/db"
	"purpcmd/server/log"

	"github.com/cheynewallace/tabby"
	"github.com/google/uuid"
)

var (
	ListenerMAP            = make(map[string]*Listener)
	CurrentListener string = "none"
)

func newListener(name, id, host, port string, persistent bool) *Listener {
	return &Listener{
		Name:       name,
		UUID:       id,
		Host:       host,
		Port:       port,
		Persistent: persistent,
		SC:         &ServerController{},
	}
}

func ListenerNew(name string) error {
	if ListenerMAP[name] != nil {
		return errors.New("listener exists")
	}

	l := newListener(name, uuid.NewString(), "0.0.0.0", "4444", true)
	if err := db.DBListenerInsert(name, l.UUID, l.Host, l.Port, true, false); err != nil {
		return err
	}

	ListenerMAP[name] = l
	CurrentListener = name
	log.PrintSuccs("New listener " + CurrentListener)
	return nil
}

func ListenerSetOptions(key, value string) error {
	l := ListenerMAP[CurrentListener]
	if l == nil {
		return errors.New("no listener")
	}

	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "uuid":
		if l.Persistent {
			if err := db.DBListenerUpdateOption(l.Name, key, value); err != nil {
				return err
			}
		}
		l.UUID = value
	case "host":
		if l.Persistent {
			if err := db.DBListenerUpdateOption(l.Name, key, value); err != nil {
				return err
			}
		}
		l.Host = value
	case "port":
		if l.Persistent {
			if err := db.DBListenerUpdateOption(l.Name, key, value); err != nil {
				return err
			}
		}
		l.Port = value
	case "persist":
		persistent, err := parseBoolOption(value)
		if err != nil {
			return err
		}
		wasPersistent, running := l.state()
		if persistent == wasPersistent {
			return nil
		}
		if persistent {
			if err := db.DBListenerInsert(l.Name, l.UUID, l.Host, l.Port, true, running); err != nil {
				return err
			}
		} else if err := db.DBListenerDelete(l.Name); err != nil {
			return err
		}
		l.setPersistent(persistent)
	default:
		return fmt.Errorf("unknown listener option %q", key)
	}

	return nil
}

func parseBoolOption(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "t", "true", "on":
		return true, nil
	case "f", "false", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}

func ListenerShowOptions() error {
	l := ListenerMAP[CurrentListener]
	if l == nil {
		return errors.New("no listener")
	}

	t := tabby.New()
	print("\n")
	println("Name: ", l.Name)
	println("UUID: ", l.UUID)
	t.AddHeader("OPTION", "VALUE", "DESCRIPTION")
	t.AddLine("Host", l.Host, "Local host")
	t.AddLine("Port", l.Port, "Local port")
	persistent, running := l.state()
	t.AddLine("Persist", fmt.Sprintf("%t", persistent), "Persist across startups")
	t.AddLine("Running", fmt.Sprintf("%t", running), "Is up")
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
	for name, l := range ListenerMAP {
		persistent, running := l.state()
		shortID := l.UUID
		if len(shortID) > 12 {
			shortID = shortID[len(shortID)-12:]
		}
		t.AddLine(c, name, shortID, l.Host+":"+l.Port, fmt.Sprintf("%t", running), fmt.Sprintf("%t", persistent), l.Association)
		c++
	}
	print("\n")
	t.Print()
	print("\n")
}

func ListenerStart() {
	l := ListenerMAP[CurrentListener]
	if l == nil {
		log.PrintErr("no listener selected")
		return
	}
	if err := l.StartHTTP(); err != nil {
		log.PrintErr(err.Error())
	}
}

func ListenerRestart() {
	l := ListenerMAP[CurrentListener]
	if l == nil {
		log.PrintErr("no listener selected")
		return
	}
	if err := l.StopHTTP(); err != nil {
		log.PrintErr(err.Error())
		return
	}
	if err := l.StartHTTP(); err != nil {
		log.PrintErr(err.Error())
	}
}

func ListenerStop() {
	l := ListenerMAP[CurrentListener]
	if l == nil {
		log.PrintErr("no listener selected")
		return
	}
	if err := l.StopHTTP(); err != nil {
		log.PrintErr(err.Error())
	}
}

func ListenerInteract(name string) error {
	if ListenerMAP[name] == nil {
		return errors.New("no listener")
	}
	CurrentListener = name
	return nil
}

func ListenerDelete() error {
	l := ListenerMAP[CurrentListener]
	if l == nil {
		return errors.New("no listener")
	}
	if l.SC.isRunning() {
		return errors.New("listener is running")
	}
	persistent, _ := l.state()
	if persistent {
		if err := db.DBListenerDelete(l.Name); err != nil {
			return err
		}
	}

	delete(ListenerMAP, CurrentListener)
	log.PrintSuccs("Listener " + CurrentListener + " deleted")
	CurrentListener = "none"
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

	for _, stored := range list {
		if !stored.Persistent {
			// Old versions could leave non-persistent rows behind. They must not
			// be resurrected during startup.
			if err := db.DBListenerDelete(stored.Name); err != nil {
				return err
			}
			continue
		}
		if ListenerMAP[stored.Name] != nil {
			return fmt.Errorf("listener %q already loaded", stored.Name)
		}

		log.PrintInfo("Setting up listener ", stored.Name)
		l := newListener(stored.Name, stored.UUID, stored.Host, stored.Port, true)
		ListenerMAP[stored.Name] = l
		CurrentListener = stored.Name
		if stored.Running {
			if err := l.StartHTTP(); err != nil {
				return fmt.Errorf("start persisted listener %q: %w", l.Name, err)
			}
		}
	}

	return nil
}
