package loot

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/runtimeevents"
)

func APIList() ([]teamapi.Loot, error) {
	rows, err := db.DBLootList()
	if err != nil {
		return nil, err
	}
	result := make([]teamapi.Loot, 0, len(rows))
	for _, row := range rows {
		item, err := lootDTO(row[0], row[1], row[2])
		if err == nil {
			result = append(result, item)
		}
	}
	return result, nil
}

func APIGet(id string) (teamapi.Loot, error) {
	name, fullID, err := db.DBLootGetByUUID(id)
	if err != nil {
		return teamapi.Loot{}, err
	}
	_, session, _, err := db.DBLoot(fullID)
	if err != nil {
		return teamapi.Loot{}, err
	}
	return lootDTO(fullID, session, name)
}

func APIContent(id string) (teamapi.Loot, []byte, error) {
	item, err := APIGet(id)
	if err != nil {
		return teamapi.Loot{}, nil, err
	}
	path, err := storagePath(item.UUID)
	if err != nil {
		return teamapi.Loot{}, nil, err
	}
	content, err := os.ReadFile(path)
	return item, content, err
}

func APIDelete(id string) (teamapi.Loot, error) {
	item, err := APIGet(id)
	if err != nil {
		return teamapi.Loot{}, err
	}
	path, err := storagePath(item.UUID)
	if err != nil {
		return teamapi.Loot{}, err
	}
	if err := os.Remove(path); err != nil {
		return teamapi.Loot{}, err
	}
	if err := db.DBLootDelete(item.UUID); err != nil {
		return teamapi.Loot{}, err
	}
	runtimeevents.Publish(teamapi.EventLootDeleted, item)
	return item, nil
}

func lootDTO(id, session, name string) (teamapi.Loot, error) {
	path, err := storagePath(id)
	if err != nil {
		return teamapi.Loot{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return teamapi.Loot{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return teamapi.Loot{}, err
	}
	sum := sha256.Sum256(content)
	return teamapi.Loot{
		UUID: id, Session: session, FileName: name, Size: info.Size(),
		SHA256: hex.EncodeToString(sum[:]), CreatedAt: info.ModTime().UTC(),
	}, nil
}
