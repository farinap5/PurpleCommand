package builds

import (
	"errors"
	"path/filepath"
	"sync"
	"time"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implantbuilder"
	"purpcmd/teamserver/events"

	"github.com/google/uuid"
)

type Manager struct {
	mu   sync.RWMutex
	jobs map[string]teamapi.Build
	bus  *events.Bus
}

func New(bus *events.Bus) *Manager {
	manager := &Manager{jobs: make(map[string]teamapi.Build), bus: bus}
	if jobs, err := db.DBBuildList(); err == nil {
		for _, job := range jobs {
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
	err := implantbuilder.APIGenerateProfile(job.Profile)
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
	job := manager.jobs[id]
	update(&job)
	manager.jobs[id] = job
	manager.mu.Unlock()
	_ = db.DBBuildSave(job)
}

func (manager *Manager) Get(id string) (teamapi.Build, error) {
	manager.mu.RLock()
	job, ok := manager.jobs[id]
	manager.mu.RUnlock()
	if !ok {
		return teamapi.Build{}, errors.New("build not found")
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
	return result
}

func (manager *Manager) Artifact(id string) (teamapi.Build, string, error) {
	job, err := manager.Get(id)
	if err != nil {
		return teamapi.Build{}, "", err
	}
	if job.Status != "completed" {
		return teamapi.Build{}, "", errors.New("build is not complete")
	}
	profile, err := implantbuilder.APIGetProfile(job.Profile)
	if err != nil {
		return teamapi.Build{}, "", err
	}
	path, err := filepath.Abs(profile.Output)
	return job, path, err
}
