package listener

import (
	"encoding/json"
	"fmt"

	"purpcmd/server/db"
)

type ListenerStore interface {
	List() ([]ManagedListenerConfig, error)
	Insert(ManagedListenerConfig) (ManagedListenerConfig, error)
	Update(ManagedListenerConfig) (ManagedListenerConfig, error)
	Delete(string) error
}

// DBStore persists listener control-plane configuration in SQLite.
type DBStore struct{}

func (DBStore) List() ([]ManagedListenerConfig, error) {
	rows, err := db.DBListenerConfigList()
	if err != nil {
		return nil, err
	}
	result := make([]ManagedListenerConfig, 0, len(rows))
	for _, row := range rows {
		if !row.Persistent {
			// Older listener implementations could leave non-persistent rows in
			// SQLite. They are process-local by definition and must not be
			// resurrected by the managed listener restore path.
			if err := db.DBListenerConfigDelete(row.Name); err != nil {
				return nil, fmt.Errorf("delete stale non-persistent listener %q: %w", row.Name, err)
			}
			continue
		}
		configuration, err := managedConfigFromDB(row)
		if err != nil {
			return nil, err
		}
		result = append(result, configuration)
	}
	return result, nil
}

func (DBStore) Insert(configuration ManagedListenerConfig) (ManagedListenerConfig, error) {
	row, err := db.DBListenerConfigInsert(managedConfigToDB(configuration))
	if err != nil {
		return ManagedListenerConfig{}, err
	}
	return managedConfigFromDB(row)
}

func (DBStore) Update(configuration ManagedListenerConfig) (ManagedListenerConfig, error) {
	row, err := db.DBListenerConfigUpdate(managedConfigToDB(configuration))
	if err != nil {
		return ManagedListenerConfig{}, err
	}
	return managedConfigFromDB(row)
}

func (DBStore) Delete(name string) error { return db.DBListenerConfigDelete(name) }

func managedConfigToDB(configuration ManagedListenerConfig) db.ListenerConfiguration {
	routesToPersist := configuration.Routes
	if routesToPersist == nil {
		routesToPersist = []Route{}
	}
	routes, _ := json.Marshal(routesToPersist)
	return db.ListenerConfiguration{
		Name: configuration.Name, UUID: configuration.UUID, Driver: configuration.Driver,
		OptionsJSON: string(configuration.Options), RoutesJSON: string(routes),
		Persistent: configuration.Persistent, DesiredState: string(configuration.DesiredState),
		ConfigVersion: configuration.ConfigVersion,
	}
}

func managedConfigFromDB(row db.ListenerConfiguration) (ManagedListenerConfig, error) {
	var routes []Route
	if err := json.Unmarshal([]byte(row.RoutesJSON), &routes); err != nil {
		return ManagedListenerConfig{}, fmt.Errorf("decode routes for listener %q: %w", row.Name, err)
	}
	return ManagedListenerConfig{
		Name: row.Name, UUID: row.UUID, Driver: row.Driver,
		Options: json.RawMessage(row.OptionsJSON), Routes: routes,
		Persistent: row.Persistent, DesiredState: State(row.DesiredState), ConfigVersion: row.ConfigVersion,
	}, nil
}
