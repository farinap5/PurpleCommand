package uploads

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

const MaxSize = 64 << 20

type entry struct {
	path    string
	expires time.Time
}

type Store struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
}

func New(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Store{entries: make(map[string]entry), ttl: ttl}
}

func (store *Store) Put(reader io.Reader) (string, error) {
	file, err := os.CreateTemp("", "purpcmd-upload-")
	if err != nil {
		return "", err
	}
	_ = file.Chmod(0600)
	limited := io.LimitReader(reader, MaxSize+1)
	written, copyErr := io.Copy(file, limited)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(file.Name())
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(file.Name())
		return "", closeErr
	}
	if written > MaxSize {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("upload exceeds %d bytes", MaxSize)
	}
	id := uuid.NewString()
	store.mu.Lock()
	store.pruneLocked()
	store.entries[id] = entry{path: file.Name(), expires: time.Now().Add(store.ttl)}
	store.mu.Unlock()
	return id, nil
}

func (store *Store) Take(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("invalid upload ID")
	}
	store.mu.Lock()
	store.pruneLocked()
	item, ok := store.entries[id]
	if ok {
		delete(store.entries, id)
	}
	store.mu.Unlock()
	if !ok {
		return "", errors.New("upload not found or expired")
	}
	absolute, err := filepath.Abs(item.path)
	if err != nil {
		_ = os.Remove(item.path)
		return "", err
	}
	return absolute, nil
}

func (store *Store) pruneLocked() {
	now := time.Now()
	for id, item := range store.entries {
		if now.After(item.expires) {
			delete(store.entries, id)
			_ = os.Remove(item.path)
		}
	}
}

var Default = New(10 * time.Minute)
