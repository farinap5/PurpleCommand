package listener

import (
	"encoding/json"
	"errors"
)

// HTTPHostedFiles returns the independently managed hosted-file configuration
// attached to an HTTP listener.
func (manager *Manager) HTTPHostedFiles(name string) (ManagedListenerSnapshot, HTTPHostedFilesConfig, error) {
	snapshot, err := manager.Get(name)
	if err != nil {
		return ManagedListenerSnapshot{}, HTTPHostedFilesConfig{}, err
	}
	if normalizeRegistryID(snapshot.Config.Driver) != "http" {
		return ManagedListenerSnapshot{}, HTTPHostedFilesConfig{}, errors.New("listener does not support HTTP hosted files")
	}
	hosted, err := httpHostedFilesFromOptions(snapshot.Config.Options)
	if err != nil {
		return ManagedListenerSnapshot{}, HTTPHostedFilesConfig{}, err
	}
	return snapshot, hosted, nil
}

// UpdateHTTPHostedFiles replaces only the hosted-file portion of an HTTP
// listener. A running HTTP runtime receives the new, pre-opened file set
// without stopping or rebinding its network listener.
func (manager *Manager) UpdateHTTPHostedFiles(name string, hosted HTTPHostedFilesConfig, expectedVersion int) (ManagedListenerSnapshot, error) {
	instance := manager.lookup(name)
	if instance == nil {
		return ManagedListenerSnapshot{}, ErrManagedListenerNotFound
	}
	instance.operationMu.Lock()
	defer instance.operationMu.Unlock()

	instance.mu.RLock()
	current := cloneManagedListenerConfig(instance.config)
	runtime := instance.runtime
	deleted := instance.deleted
	instance.mu.RUnlock()
	if deleted {
		return ManagedListenerSnapshot{}, ErrManagedListenerDeleted
	}
	if expectedVersion > 0 && expectedVersion != current.ConfigVersion {
		return ManagedListenerSnapshot{}, errors.New("listener configuration changed")
	}

	driverValue, found := manager.registry.Driver(current.Driver)
	if !found {
		return ManagedListenerSnapshot{}, errors.New("listener driver is not registered")
	}
	httpDriver, supported := driverValue.(*HTTPDriver)
	if !supported {
		return ManagedListenerSnapshot{}, errors.New("listener does not support HTTP hosted files")
	}

	candidate := cloneManagedListenerConfig(current)
	var err error
	candidate.Options, err = replaceHTTPHostedOptions(candidate.Options, hosted)
	if err != nil {
		return ManagedListenerSnapshot{}, err
	}
	candidate, validatedDriver, err := manager.validateConfiguration(candidate)
	if err != nil {
		return ManagedListenerSnapshot{}, err
	}
	if err := validatedDriver.Validate(candidate.Options, candidate.Routes); err != nil {
		return ManagedListenerSnapshot{}, err
	}

	var prepared *compiledHTTPHostedFiles
	var live *httpRuntime
	if runtime != nil {
		var ok bool
		live, ok = runtime.(*httpRuntime)
		if !ok || live.replaceHosted == nil {
			return ManagedListenerSnapshot{}, errors.New("running listener does not support live hosted-file updates")
		}
		options, resolveErr := resolveHTTPOptions(candidate.Options)
		if resolveErr != nil {
			return ManagedListenerSnapshot{}, resolveErr
		}
		prepared, err = httpDriver.openHostedFiles(options)
		if err != nil {
			return ManagedListenerSnapshot{}, err
		}
	}

	saved, err := manager.persistReplacement(current, candidate)
	if err != nil {
		prepared.Close()
		return ManagedListenerSnapshot{}, err
	}
	if live != nil {
		live.replaceHosted(prepared)
	}
	instance.mu.Lock()
	instance.config = saved
	snapshot := ManagedListenerSnapshot{Config: cloneManagedListenerConfig(instance.config), Status: instance.status}
	instance.mu.Unlock()
	manager.emit("hosted", snapshot)
	return snapshot, nil
}

func httpHostedFilesFromOptions(raw json.RawMessage) (HTTPHostedFilesConfig, error) {
	options, err := resolveHTTPOptions(raw)
	if err != nil {
		return HTTPHostedFilesConfig{}, err
	}
	return cloneHTTPHostedFilesConfig(HTTPHostedFilesConfig{
		HostedFiles: options.HostedFiles, NotFoundPage: options.NotFoundPage,
	}), nil
}

func replaceHTTPHostedOptions(raw json.RawMessage, hosted HTTPHostedFilesConfig) (json.RawMessage, error) {
	options := make(map[string]json.RawMessage)
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &options); err != nil || options == nil {
			return nil, errors.New("listener options must be a JSON object")
		}
	}
	hosted = cloneHTTPHostedFilesConfig(hosted)
	if len(hosted.HostedFiles) == 0 {
		delete(options, "hosted_files")
	} else {
		encoded, err := json.Marshal(hosted.HostedFiles)
		if err != nil {
			return nil, err
		}
		options["hosted_files"] = encoded
	}
	if hosted.NotFoundPage == nil {
		delete(options, "not_found_page")
	} else {
		encoded, err := json.Marshal(hosted.NotFoundPage)
		if err != nil {
			return nil, err
		}
		options["not_found_page"] = encoded
	}
	return json.Marshal(options)
}

func cloneHTTPHostedFilesConfig(source HTTPHostedFilesConfig) HTTPHostedFilesConfig {
	result := HTTPHostedFilesConfig{HostedFiles: make(map[string]HTTPHostedFileConfig, len(source.HostedFiles))}
	for urlPath, file := range source.HostedFiles {
		result.HostedFiles[urlPath] = cloneHTTPHostedFileConfig(file)
	}
	if source.NotFoundPage != nil {
		file := cloneHTTPHostedFileConfig(*source.NotFoundPage)
		result.NotFoundPage = &file
	}
	return result
}

func cloneHTTPHostedFileConfig(source HTTPHostedFileConfig) HTTPHostedFileConfig {
	result := source
	result.Headers = cloneHeaderMap(source.Headers)
	return result
}
