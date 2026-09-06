package listener

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerUpdatesHTTPHostedFilesWithoutRestarting(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"old.txt": "old content", "new.txt": "new content", "404.html": "new missing",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	events := make([]ListenerEvent, 0)
	manager, err := NewHTTPManagerWithConfig(nil, func(event ListenerEvent) {
		events = append(events, event)
	}, HTTPDriverConfig{HostedRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	created, err := manager.Create(ManagedListenerConfig{
		Name: "live-hosting", UUID: "live-hosting-id", Driver: "http", Persistent: false,
		Options: json.RawMessage(`{
			"bind":{"host":"127.0.0.1","port":"0"},
			"response_headers":{"X-Listener":"unchanged"},
			"hosted_files":{"/asset":{"source_path":"old.txt"}}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := manager.Start(created.Config.Name)
	if err != nil {
		t.Fatal(err)
	}
	address := started.Status.Address
	assertHTTPBody(t, "http://"+address+"/asset", http.StatusOK, "old content")

	instance := manager.lookup(created.Config.Name)
	instance.mu.RLock()
	runtime := instance.runtime.(*httpRuntime)
	instance.mu.RUnlock()
	handler := runtime.server.Handler.(*httpListenerHandler)
	oldFiles := handler.hostedFiles

	notFound := HTTPHostedFileConfig{SourcePath: "404.html", Headers: map[string]string{"X-Page": "missing"}}
	updated, err := manager.UpdateHTTPHostedFiles(created.Config.Name, HTTPHostedFilesConfig{
		HostedFiles: map[string]HTTPHostedFileConfig{
			"/asset": {SourcePath: "new.txt", Status: http.StatusAccepted, Headers: map[string]string{"X-Page": "asset"}},
		},
		NotFoundPage: &notFound,
	}, started.Config.ConfigVersion)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status.State != StateRunning || updated.Status.Address != address {
		t.Fatalf("live update changed runtime = %#v", updated.Status)
	}
	if updated.Config.ConfigVersion <= started.Config.ConfigVersion || !strings.Contains(string(updated.Config.Options), "X-Listener") {
		t.Fatalf("live update did not preserve options/version = %#v", updated.Config)
	}
	if _, err := oldFiles.files["/asset"].file.Stat(); err == nil {
		t.Fatal("retired hosted-file descriptor remained open")
	}
	assertHTTPBody(t, "http://"+address+"/asset", http.StatusAccepted, "new content")
	assertHTTPBody(t, "http://"+address+"/missing", http.StatusNotFound, "new missing")

	if _, err := manager.UpdateHTTPHostedFiles(created.Config.Name, HTTPHostedFilesConfig{}, started.Config.ConfigVersion); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("stale hosted update error = %v", err)
	}
	beforeFailure := updated.Config.ConfigVersion
	if _, err := manager.UpdateHTTPHostedFiles(created.Config.Name, HTTPHostedFilesConfig{
		HostedFiles: map[string]HTTPHostedFileConfig{"/asset": {SourcePath: "missing.txt"}},
	}, beforeFailure); err == nil {
		t.Fatal("running update accepted a missing source")
	}
	afterFailure, err := manager.Get(created.Config.Name)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.Config.ConfigVersion != beforeFailure {
		t.Fatalf("failed update changed version: before=%d after=%d", beforeFailure, afterFailure.Config.ConfigVersion)
	}
	assertHTTPBody(t, "http://"+address+"/asset", http.StatusAccepted, "new content")

	if len(events) == 0 || events[len(events)-1].Type != "hosted" {
		t.Fatalf("hosted event was not emitted: %#v", events)
	}
}

func TestManagerRejectsHostedFilesForAnotherDriver(t *testing.T) {
	manager, _, _ := newManagerTest(t)
	created, err := manager.Create(ManagedListenerConfig{
		Name: "other", UUID: "other-id", Driver: "test", Options: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.HTTPHostedFiles(created.Config.Name); err == nil {
		t.Fatal("non-HTTP listener returned a hosted-file configuration")
	}
	if _, err := manager.UpdateHTTPHostedFiles(created.Config.Name, HTTPHostedFilesConfig{}, created.Config.ConfigVersion); err == nil {
		t.Fatal("non-HTTP listener accepted a hosted-file update")
	}
}

func assertHTTPBody(t *testing.T, url string, status int, body string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != status || string(content) != body {
		t.Fatalf("GET %s = %d %q, want %d %q", url, response.StatusCode, content, status, body)
	}
}

func TestCompiledHTTPHostedFilesRetireAfterActiveReferences(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, driver := newHTTPDriverTestWithRoot(t, root)
	options, err := resolveHTTPOptions(json.RawMessage(`{"hosted_files":{"/":{"source_path":"file.txt"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	files, err := driver.openHostedFiles(options)
	if err != nil {
		t.Fatal(err)
	}
	if !files.acquire() {
		t.Fatal("could not acquire active hosted-file set")
	}
	files.Close()
	if _, err := files.files["/"].file.Stat(); err != nil {
		t.Fatalf("active descriptor closed before release: %v", err)
	}
	files.release()
	if _, err := files.files["/"].file.Stat(); err == nil {
		t.Fatal("retired descriptor remained open after release")
	}
}
