package listener

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"

	"github.com/google/uuid"
)

var apiMu sync.Mutex

func APIList() []teamapi.Listener {
	apiMu.Lock()
	defer apiMu.Unlock()
	result := make([]teamapi.Listener, 0, len(ListenerMAP))
	for _, listener := range ListenerMAP {
		persistent, running, associations := listener.snapshotState()
		result = append(result, teamapi.Listener{
			Name:         listener.Name,
			UUID:         listener.UUID,
			Host:         listener.Host,
			Port:         listener.Port,
			Running:      running,
			Persistent:   persistent,
			Associations: associations,
		})
	}
	return result
}

func APIGet(name string) (teamapi.Listener, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	return apiGetLocked(name)
}

func apiGetLocked(name string) (teamapi.Listener, error) {
	listener := ListenerMAP[name]
	if listener == nil {
		return teamapi.Listener{}, errors.New("listener not found")
	}
	persistent, running, associations := listener.snapshotState()
	return teamapi.Listener{
		Name:         listener.Name,
		UUID:         listener.UUID,
		Host:         listener.Host,
		Port:         listener.Port,
		Running:      running,
		Persistent:   persistent,
		Associations: associations,
	}, nil
}

func APICreate(request teamapi.ListenerCreateRequest) (teamapi.Listener, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return teamapi.Listener{}, errors.New("listener name is required")
	}
	if ListenerMAP[name] != nil {
		return teamapi.Listener{}, errors.New("listener exists")
	}
	host := request.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := request.Port
	if port == "" {
		port = "4444"
	}
	persistent := true
	if request.Persistent != nil {
		persistent = *request.Persistent
	}
	listener := newListener(name, uuid.NewString(), host, port, persistent)
	if persistent {
		if err := db.DBListenerInsert(name, listener.UUID, host, port, true, false); err != nil {
			return teamapi.Listener{}, err
		}
	}
	ListenerMAP[name] = listener
	return apiGetLocked(name)
}

func APIUpdate(request teamapi.ListenerUpdateRequest) (teamapi.Listener, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	listener := ListenerMAP[request.Name]
	if listener == nil {
		return teamapi.Listener{}, errors.New("listener not found")
	}
	key := strings.ToLower(strings.TrimSpace(request.Key))
	value := strings.TrimSpace(request.Value)
	switch key {
	case "uuid":
		if listener.Persistent {
			if err := db.DBListenerUpdateOption(listener.Name, key, value); err != nil {
				return teamapi.Listener{}, err
			}
		}
		listener.UUID = value
	case "host":
		if listener.SC.isRunning() {
			return teamapi.Listener{}, errors.New("stop listener before changing host")
		}
		if listener.Persistent {
			if err := db.DBListenerUpdateOption(listener.Name, key, value); err != nil {
				return teamapi.Listener{}, err
			}
		}
		listener.Host = value
	case "port":
		if listener.SC.isRunning() {
			return teamapi.Listener{}, errors.New("stop listener before changing port")
		}
		if listener.Persistent {
			if err := db.DBListenerUpdateOption(listener.Name, key, value); err != nil {
				return teamapi.Listener{}, err
			}
		}
		listener.Port = value
	case "persist":
		persistent, err := parseBoolOption(value)
		if err != nil {
			return teamapi.Listener{}, err
		}
		wasPersistent, running := listener.state()
		if persistent != wasPersistent {
			if persistent {
				if err := db.DBListenerInsert(listener.Name, listener.UUID, listener.Host, listener.Port, true, running); err != nil {
					return teamapi.Listener{}, err
				}
			} else if err := db.DBListenerDelete(listener.Name); err != nil {
				return teamapi.Listener{}, err
			}
			listener.setPersistent(persistent)
		}
	default:
		return teamapi.Listener{}, fmt.Errorf("unknown listener option %q", key)
	}
	return apiGetLocked(request.Name)
}

func APIStart(name string) (teamapi.Listener, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	listener := ListenerMAP[name]
	if listener == nil {
		return teamapi.Listener{}, errors.New("listener not found")
	}
	if err := listener.StartHTTP(); err != nil {
		return teamapi.Listener{}, err
	}
	return apiGetLocked(name)
}

func APIStop(name string) (teamapi.Listener, error) {
	apiMu.Lock()
	defer apiMu.Unlock()
	listener := ListenerMAP[name]
	if listener == nil {
		return teamapi.Listener{}, errors.New("listener not found")
	}
	if err := listener.StopHTTP(); err != nil {
		return teamapi.Listener{}, err
	}
	return apiGetLocked(name)
}

func APIRestart(name string) (teamapi.Listener, error) {
	if _, err := APIStop(name); err != nil {
		return teamapi.Listener{}, err
	}
	return APIStart(name)
}

func APIDelete(name string) error {
	apiMu.Lock()
	defer apiMu.Unlock()
	listener := ListenerMAP[name]
	if listener == nil {
		return errors.New("listener not found")
	}
	if listener.SC.isRunning() {
		if err := listener.StopHTTP(); err != nil {
			return err
		}
	}
	persistent, _ := listener.state()
	if persistent {
		if err := db.DBListenerDelete(listener.Name); err != nil {
			return err
		}
	}
	delete(ListenerMAP, name)
	if CurrentListener == name {
		CurrentListener = "none"
	}
	return nil
}
