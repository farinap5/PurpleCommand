package loot

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"purpcmd/server/db"
	"purpcmd/server/log"

	"github.com/cheynewallace/tabby"
	"github.com/google/uuid"
)

// StorageDir is created automatically on the first downloaded file. It is a
// variable so deployments and tests can select a different storage location.
var StorageDir = "loot"

func New(session, name string, content []byte) *Loot {
	return &Loot{
		FileName: name,
		Content:  content,
		Session:  session,
		UUID:     uuid.NewString(),
	}
}

func storagePath(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("invalid loot UUID %q: %w", id, err)
	}
	return filepath.Join(StorageDir, id), nil
}

func (l *Loot) SaveData() error {
	path, err := storagePath(l.UUID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(StorageDir, 0700); err != nil {
		return fmt.Errorf("create loot directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(l.Content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}

	if err := db.DBLootInsert(l.UUID, l.Session, l.FileName); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func List() {
	t := tabby.New()
	c := 1
	t.AddHeader("N", "UUID", "SESSION", "FILENAME")

	entries, err := db.DBLootList()
	if err != nil {
		log.PrintErr(err.Error())
		return
	}
	for _, entry := range entries {
		shortID := entry[0]
		if len(shortID) > 12 {
			shortID = shortID[len(shortID)-12:]
		}
		t.AddLine(c, shortID, entry[1], entry[2])
		c++
	}
	print("\n")
	t.Print()
	print("\n")
}

func Export(id, destination string) error {
	name, fullID, err := db.DBLootGetByUUID(id)
	if err != nil {
		return err
	}
	sourcePath, err := storagePath(fullID)
	if err != nil {
		return err
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destinationFile, source); err != nil {
		_ = destinationFile.Close()
		return err
	}
	if err := destinationFile.Close(); err != nil {
		return err
	}

	log.PrintSuccs("file ", fullID, " ", name, " saved to ", destination)
	return nil
}

func Delete(id string) error {
	name, fullID, err := db.DBLootGetByUUID(id)
	if err != nil {
		return err
	}
	path, err := storagePath(fullID)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return err
	}
	if err := db.DBLootDelete(fullID); err != nil {
		return err
	}

	log.PrintSuccs("deleted loot file ", fullID, " (", name, ")")
	return nil
}

func View(id string) error {
	name, fullID, err := db.DBLootGetByUUID(id)
	if err != nil {
		return err
	}
	path, err := storagePath(fullID)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	log.PrintSuccs("Viewing loot file: ", name, " (UUID: ", fullID, ")")
	println("\n" + string(content) + "\n")
	return nil
}
