package builds

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implantbuilder"
	"purpcmd/teamserver/events"

	"github.com/google/uuid"
)

type Manager struct {
	mu          sync.RWMutex
	buildMu     sync.Mutex
	jobs        map[string]teamapi.Build
	sourcePaths map[string]string
	artifactDir string
	bus         *events.Bus
}

var (
	ErrBuildNotFound = errors.New("build not found")
	ErrBuildActive   = errors.New("active build cannot be deleted")
)

func New(bus *events.Bus, artifactDirectories ...string) *Manager {
	artifactDir := "builds"
	if len(artifactDirectories) > 0 && strings.TrimSpace(artifactDirectories[0]) != "" {
		artifactDir = artifactDirectories[0]
	}
	manager := &Manager{
		jobs:        make(map[string]teamapi.Build),
		sourcePaths: make(map[string]string),
		artifactDir: artifactDir,
		bus:         bus,
	}
	if jobs, err := db.DBBuildList(); err == nil {
		for _, job := range jobs {
			if job.Status == "queued" || job.Status == "running" {
				job.Status = "failed"
				job.Error = "build interrupted by teamserver restart"
				job.CompletedAt = time.Now().UTC()
				_ = db.DBBuildSave(job)
			}
			manager.jobs[job.ID] = job
		}
	}
	return manager
}

func (manager *Manager) Create(profileName string) (teamapi.Build, error) {
	profile, err := implantbuilder.APIGetProfile(profileName)
	if err != nil {
		return teamapi.Build{}, err
	}
	job := teamapi.Build{
		ID: uuid.NewString(), Profile: profileName, Status: "queued",
		ArtifactName: filepath.Base(profile.Output), CreatedAt: time.Now().UTC(),
	}
	if err := db.DBBuildSave(job); err != nil {
		return teamapi.Build{}, err
	}
	manager.mu.Lock()
	manager.jobs[job.ID] = job
	manager.sourcePaths[job.ID] = profile.Output
	manager.mu.Unlock()
	go manager.run(job.ID)
	return job, nil
}

func (manager *Manager) run(id string) {
	manager.update(id, func(job *teamapi.Build) { job.Status = "running" })
	job, _ := manager.Get(id)
	if manager.bus != nil {
		_, _ = manager.bus.Publish(teamapi.EventBuildStarted, job)
	}
	manager.buildMu.Lock()
	err := implantbuilder.APIGenerateProfile(job.Profile)
	if err == nil {
		manager.mu.RLock()
		sourcePath := manager.sourcePaths[id]
		manager.mu.RUnlock()
		err = manager.archive(id, sourcePath)
	}
	manager.buildMu.Unlock()
	manager.update(id, func(job *teamapi.Build) {
		job.CompletedAt = time.Now().UTC()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			job.Status = "completed"
			job.DownloadURL = "/api/v1/builds/" + job.ID + "/artifact"
		}
	})
	manager.mu.Lock()
	delete(manager.sourcePaths, id)
	manager.mu.Unlock()
	job, _ = manager.Get(id)
	if manager.bus != nil {
		eventType := teamapi.EventBuildCompleted
		if err != nil {
			eventType = teamapi.EventBuildFailed
		}
		_, _ = manager.bus.Publish(eventType, job)
	}
}

func (manager *Manager) update(id string, update func(*teamapi.Build)) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job := manager.jobs[id]
	update(&job)
	manager.jobs[id] = job
	_ = db.DBBuildSave(job)
}

func (manager *Manager) Get(id string) (teamapi.Build, error) {
	manager.mu.RLock()
	job, ok := manager.jobs[id]
	manager.mu.RUnlock()
	if !ok {
		return teamapi.Build{}, ErrBuildNotFound
	}
	if job.Status == "completed" {
		job.DownloadURL = "/api/v1/builds/" + job.ID + "/artifact"
	}
	return job, nil
}

func (manager *Manager) List() []teamapi.Build {
	manager.mu.RLock()
	result := make([]teamapi.Build, 0, len(manager.jobs))
	for _, job := range manager.jobs {
		if job.Status == "completed" {
			job.DownloadURL = "/api/v1/builds/" + job.ID + "/artifact"
		}
		result = append(result, job)
	}
	manager.mu.RUnlock()
	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.After(result[right].CreatedAt)
	})
	return result
}

// Delete removes a completed or failed build from history and deletes its
// managed artifact. Profile output files from builds created before artifact
// archiving was introduced are not removed because they may be shared.
func (manager *Manager) Delete(id string) (teamapi.Build, error) {
	id = strings.TrimSpace(id)
	manager.mu.Lock()
	job, ok := manager.jobs[id]
	if !ok {
		manager.mu.Unlock()
		return teamapi.Build{}, ErrBuildNotFound
	}
	if job.Status == "queued" || job.Status == "running" {
		manager.mu.Unlock()
		return teamapi.Build{}, ErrBuildActive
	}
	artifactPath, err := manager.artifactPath(id)
	if err != nil {
		manager.mu.Unlock()
		return teamapi.Build{}, err
	}
	if err := os.Remove(artifactPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		manager.mu.Unlock()
		return teamapi.Build{}, fmt.Errorf("delete build artifact: %w", err)
	}
	if err := db.DBBuildDelete(id); err != nil {
		manager.mu.Unlock()
		return teamapi.Build{}, err
	}
	delete(manager.jobs, id)
	delete(manager.sourcePaths, id)
	manager.mu.Unlock()
	if manager.bus != nil {
		_, _ = manager.bus.Publish(teamapi.EventBuildDeleted, job)
	}
	return job, nil
}

func (manager *Manager) Artifact(id string) (teamapi.Build, string, error) {
	job, err := manager.Get(id)
	if err != nil {
		return teamapi.Build{}, "", err
	}
	if job.Status != "completed" {
		return teamapi.Build{}, "", errors.New("build is not complete")
	}
	managedPath, err := manager.artifactPath(id)
	if err != nil {
		return teamapi.Build{}, "", err
	}
	if info, err := os.Stat(managedPath); err == nil && info.Mode().IsRegular() {
		return job, managedPath, nil
	}

	// Compatibility for builds completed before per-build artifact storage.
	profile, err := implantbuilder.APIGetProfile(job.Profile)
	if err != nil {
		return teamapi.Build{}, "", err
	}
	path, err := filepath.Abs(profile.Output)
	return job, path, err
}

func (manager *Manager) artifactPath(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("invalid build ID %q: %w", id, err)
	}
	return filepath.Join(manager.artifactDir, id), nil
}

func (manager *Manager) archive(id, sourcePath string) error {
	if strings.TrimSpace(sourcePath) == "" {
		return errors.New("build output path is empty")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open build artifact: %w", err)
	}
	defer source.Close()
	if err := os.MkdirAll(manager.artifactDir, 0700); err != nil {
		return fmt.Errorf("create build artifact directory: %w", err)
	}
	destinationPath, err := manager.artifactPath(id)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(manager.artifactDir, ".build-*")
	if err != nil {
		return fmt.Errorf("create build artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		return err
	}
	if _, err := io.Copy(temporary, source); err != nil {
		return fmt.Errorf("copy build artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close build artifact: %w", err)
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		return fmt.Errorf("store build artifact: %w", err)
	}
	removeTemporary = false
	return nil
}
