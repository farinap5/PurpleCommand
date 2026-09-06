package server

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"purpcmd/internal"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implant"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/interactive"
	"purpcmd/server/listener"
	"purpcmd/server/loot"
	"purpcmd/server/lua"
)

func (server *Server) listenerList() []teamapi.Listener {
	snapshots := server.listeners.List()
	result := make([]teamapi.Listener, len(snapshots))
	for index, snapshot := range snapshots {
		result[index] = listener.ListenerSnapshotToAPI(snapshot)
	}
	return result
}

func (server *Server) dispatch(envelope teamapi.Envelope, actor principal) (any, *teamapi.APIError) {
	fail := func(err error) (any, *teamapi.APIError) {
		return nil, &teamapi.APIError{Code: "request_failed", Message: err.Error()}
	}
	publish := func(eventType string, value any) {
		_, _ = server.events.Publish(eventType, value)
	}
	if userMutation(envelope.Type) && !actor.Admin {
		return nil, &teamapi.APIError{Code: "forbidden", Message: "only the admin user can manage users"}
	}
	switch envelope.Type {
	case teamapi.AskSystemHello:
		var request teamapi.HelloRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		latest, err := server.events.LatestSequence()
		if err != nil {
			return fail(err)
		}
		return teamapi.HelloReply{ServerID: server.id, ServerVersion: "dev", Protocol: teamapi.Version, EventSequence: latest, ResyncRequired: request.LastEventSequence < latest}, nil
	case teamapi.AskSystemHealth:
		return map[string]any{"ok": true, "time": time.Now().UTC()}, nil
	case teamapi.AskSystemSnapshot:
		latest, err := server.events.LatestSequence()
		if err != nil {
			return fail(err)
		}
		users, err := server.userList()
		if err != nil {
			return fail(err)
		}
		return teamapi.Snapshot{
			Listeners: server.listenerList(), Sessions: implant.APIListSessions(),
			Scripts: lua.APIListScripts(), Profiles: implantbuilder.APIListProfiles(),
			Builds: server.builds.List(), Commands: lua.APIListCommands(""),
			Users: users, EventSequence: latest,
		}, nil
	case teamapi.AskListenerList:
		return server.listenerList(), nil
	case teamapi.AskListenerGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		snapshot, err := server.listeners.Get(request.Name)
		if err != nil {
			return fail(err)
		}
		return listener.ListenerSnapshotToAPI(snapshot), nil
	case teamapi.AskListenerHosted:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		snapshot, _, err := server.listeners.HTTPHostedFiles(request.Name)
		if err != nil {
			return fail(err)
		}
		hosted, err := listener.ListenerHostedConfigurationToAPI(snapshot)
		if err != nil {
			return fail(err)
		}
		return hosted, nil
	case teamapi.AskListenerHostedSet:
		var request teamapi.ListenerHostedSetRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if request.HostedFiles == nil {
			return fail(errors.New("hosted_files is required"))
		}
		hosted, err := server.updateListenerHosted(request.Name, request.ExpectedConfigVersion, func(configuration *listener.HTTPHostedFilesConfig) error {
			*configuration = listener.HTTPHostedFilesConfigFromAPI(request.HostedFiles, request.NotFoundPage)
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return hosted, nil
	case teamapi.AskListenerHostedAdd:
		var request teamapi.ListenerHostedAddRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		hosted, err := server.updateListenerHosted(request.Name, request.ExpectedConfigVersion, func(configuration *listener.HTTPHostedFilesConfig) error {
			file := listener.HTTPHostedFilesConfigFromAPI(map[string]teamapi.HTTPHostedFile{request.URLPath: request.File}, nil)
			configuration.HostedFiles[request.URLPath] = file.HostedFiles[request.URLPath]
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return hosted, nil
	case teamapi.AskListenerHostedRemove:
		var request teamapi.ListenerHostedRemoveRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		hosted, err := server.updateListenerHosted(request.Name, request.ExpectedConfigVersion, func(configuration *listener.HTTPHostedFilesConfig) error {
			if _, found := configuration.HostedFiles[request.URLPath]; !found {
				return errors.New("hosted URL path not found")
			}
			delete(configuration.HostedFiles, request.URLPath)
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return hosted, nil
	case teamapi.AskListenerHostedNotFoundSet:
		var request teamapi.ListenerHostedNotFoundSetRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		hosted, err := server.updateListenerHosted(request.Name, request.ExpectedConfigVersion, func(configuration *listener.HTTPHostedFilesConfig) error {
			file := listener.HTTPHostedFilesConfigFromAPI(nil, &request.File)
			configuration.NotFoundPage = file.NotFoundPage
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return hosted, nil
	case teamapi.AskListenerHostedNotFoundClear:
		var request teamapi.ListenerHostedNotFoundClearRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		hosted, err := server.updateListenerHosted(request.Name, request.ExpectedConfigVersion, func(configuration *listener.HTTPHostedFilesConfig) error {
			configuration.NotFoundPage = nil
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return hosted, nil
	case teamapi.AskListenerCreate:
		var request teamapi.ListenerCreateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		configuration, err := listener.ListenerCreateFromAPI(request)
		if err != nil {
			return fail(err)
		}
		snapshot, err := server.listeners.Create(configuration)
		if err != nil {
			return fail(err)
		}
		if request.Start {
			snapshot, err = server.listeners.Start(configuration.Name)
			if err != nil {
				return fail(err)
			}
		}
		return listener.ListenerSnapshotToAPI(snapshot), nil
	case teamapi.AskListenerUpdate:
		var request teamapi.ListenerUpdateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		server.profileListenerMu.Lock()
		current, err := server.listeners.Get(request.Name)
		if err != nil {
			server.profileListenerMu.Unlock()
			return fail(err)
		}
		configuration, err := listener.ListenerUpdateFromAPI(current, request)
		if err != nil {
			server.profileListenerMu.Unlock()
			return fail(err)
		}
		if request.ExpectedConfigVersion > 0 && request.ExpectedConfigVersion != current.Config.ConfigVersion {
			server.profileListenerMu.Unlock()
			return fail(errors.New("listener configuration changed"))
		}
		if _, compatibilityErr := listener.HTTPProfileLHOST(listener.ManagedListenerSnapshot{Config: configuration}); compatibilityErr != nil {
			profiles := implantbuilder.APIProfileNamesForListener(current.Config.UUID)
			if len(profiles) != 0 {
				server.profileListenerMu.Unlock()
				return fail(fmt.Errorf(
					"detach profiles %s before making listener %q incompatible: %w",
					strings.Join(profiles, ", "), current.Config.Name, compatibilityErr,
				))
			}
		}
		snapshot, err := server.listeners.Update(configuration, request.ExpectedConfigVersion)
		if err != nil {
			server.profileListenerMu.Unlock()
			return fail(err)
		}
		server.profileListenerMu.Unlock()
		return listener.ListenerSnapshotToAPI(snapshot), nil
	case teamapi.AskListenerStart:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		snapshot, err := server.listeners.Start(request.Name)
		if err != nil {
			return fail(err)
		}
		return listener.ListenerSnapshotToAPI(snapshot), nil
	case teamapi.AskListenerStop:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		snapshot, err := server.listeners.Stop(request.Name)
		if err != nil {
			return fail(err)
		}
		return listener.ListenerSnapshotToAPI(snapshot), nil
	case teamapi.AskListenerRestart:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		snapshot, err := server.listeners.Restart(request.Name)
		if err != nil {
			return fail(err)
		}
		return listener.ListenerSnapshotToAPI(snapshot), nil
	case teamapi.AskListenerDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		server.profileListenerMu.Lock()
		snapshot, err := server.listeners.Get(request.Name)
		if err != nil {
			server.profileListenerMu.Unlock()
			return fail(err)
		}
		updatedProfiles, err := implantbuilder.APIClearProfileListeners(snapshot.Config.UUID)
		if err != nil {
			server.profileListenerMu.Unlock()
			return fail(err)
		}
		_, deleteErr := server.listeners.Delete(request.Name)
		server.profileListenerMu.Unlock()
		for _, profile := range updatedProfiles {
			publish(teamapi.EventProfileUpdated, profile)
		}
		if deleteErr != nil {
			return fail(deleteErr)
		}
		deleted := map[string]string{"name": request.Name}
		return deleted, nil
	case teamapi.AskListenerTypeList:
		return listener.ListenerDriverDefinitionsToAPI(server.listeners.DriverDefinitions()), nil
	case teamapi.AskListenerTypeGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		for _, definition := range listener.ListenerDriverDefinitionsToAPI(server.listeners.DriverDefinitions()) {
			if definition.ID == strings.ToLower(strings.TrimSpace(request.Name)) {
				return definition, nil
			}
		}
		return fail(errors.New("listener type not found"))
	case teamapi.AskCarrierTypeList:
		return listener.ListenerCarrierDefinitionsToAPI(server.listeners.CarrierDefinitions()), nil
	case teamapi.AskSessionList:
		return implant.APIListSessions(), nil
	case teamapi.AskSessionGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APIGetSession(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskSessionTerminate:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APIRequestTermination(request.Name)
		if err != nil && !errors.Is(err, implant.ErrTerminationPending) {
			return fail(err)
		}
		return item, nil
	case teamapi.AskSessionDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := implant.APIDeleteSession(request.Name); err != nil {
			return fail(err)
		}
		publish(teamapi.EventSessionDeleted, map[string]string{"name": request.Name})
		return map[string]string{"name": request.Name}, nil
	case teamapi.AskCommandList:
		var request teamapi.CommandListRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		return lua.APIListCommands(request.PayloadType), nil
	case teamapi.AskCommandExecute:
		var request teamapi.CommandExecuteRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		result, err := lua.APIExecuteCommand(request)
		if err != nil {
			return fail(err)
		}
		return result, nil
	case teamapi.AskTaskCreate:
		var request teamapi.TaskCreateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APICreateTask(request)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskTaskList:
		var request teamapi.TaskListRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		items, err := implant.APIListTasks(request.Session)
		if err != nil {
			return fail(err)
		}
		return items, nil
	case teamapi.AskTaskGet:
		var request teamapi.TaskGetRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implant.APIGetTask(request.Session, request.TaskID)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskLootList:
		items, err := loot.APIList()
		if err != nil {
			return fail(err)
		}
		return items, nil
	case teamapi.AskLootGet:
		var request teamapi.LootRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := loot.APIGet(request.UUID)
		if err != nil {
			return fail(err)
		}
		return teamapi.LootGetReply{Loot: item, DownloadURL: "/api/v1/loot/" + item.UUID + "/content"}, nil
	case teamapi.AskLootDelete:
		var request teamapi.LootRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := loot.APIDelete(request.UUID)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskScriptList:
		return lua.APIListScripts(), nil
	case teamapi.AskScriptLoad:
		var request teamapi.ScriptLoadRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := lua.APILoadScript(request.Path)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventScriptLoaded, item)
		return item, nil
	case teamapi.AskScriptUnload:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := lua.APIUnloadScript(request.Name); err != nil {
			return fail(err)
		}
		publish(teamapi.EventScriptUnloaded, map[string]string{"path": request.Name})
		return map[string]string{"path": request.Name}, nil
	case teamapi.AskProfileList:
		return implantbuilder.APIListProfiles(), nil
	case teamapi.AskProfileGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implantbuilder.APIGetProfile(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskProfileCreate:
		var request teamapi.Profile
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implantbuilder.APICreateProfile(request)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventProfileCreated, item)
		return item, nil
	case teamapi.AskProfileUpdate:
		var request teamapi.ProfileUpdateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := implantbuilder.APIUpdateProfile(request)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventProfileUpdated, item)
		return item, nil
	case teamapi.AskProfileDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := implantbuilder.APIDeleteProfile(request.Name); err != nil {
			return fail(err)
		}
		deleted := map[string]string{"name": request.Name}
		publish(teamapi.EventProfileDeleted, deleted)
		return deleted, nil
	case teamapi.AskProfileListenerSet:
		var request teamapi.ProfileListenerSetRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if request.ListenerUUID == nil {
			return fail(errors.New("listener_uuid is required; use an explicit empty string to detach"))
		}
		server.profileListenerMu.Lock()
		defer server.profileListenerMu.Unlock()
		listenerUUID := strings.TrimSpace(*request.ListenerUUID)
		lhost := ""
		if listenerUUID != "" {
			snapshot, err := server.listeners.GetByUUID(listenerUUID)
			if err != nil {
				return fail(err)
			}
			lhost, err = listener.HTTPProfileLHOST(snapshot)
			if err != nil {
				return fail(err)
			}
		}
		item, changed, err := implantbuilder.APISetProfileListener(request.Name, listenerUUID, lhost)
		if err != nil {
			return fail(err)
		}
		if changed {
			publish(teamapi.EventProfileUpdated, item)
		}
		return item, nil
	case teamapi.AskBuildCreate:
		var request teamapi.BuildRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := server.builds.Create(request.Profile, request.Builder)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskBuildGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := server.builds.Get(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskBuildList:
		return server.builds.List(), nil
	case teamapi.AskPayloadBuilderList:
		return implantbuilder.APIListPayloadBuilders(), nil
	case teamapi.AskBuildDelete:
		var request teamapi.BuildDeleteRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := server.builds.Delete(request.ID)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskInteractiveOpen:
		var request teamapi.InteractiveOpenRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if _, err := implant.APIGetSession(request.Session); err != nil {
			return fail(err)
		}
		streamID, expires := interactive.Default.Open(request.Session)
		_, err := implant.APICreateTask(teamapi.TaskCreateRequest{Session: request.Session, Code: uint16(internal.SSH), Payload: []byte(streamID)})
		if err != nil {
			interactive.Default.Close(streamID)
			return fail(err)
		}
		return teamapi.InteractiveOpenReply{StreamID: streamID, URL: "/api/v1/interactive/" + streamID, Expires: expires}, nil
	case teamapi.AskInteractiveClose:
		var request teamapi.InteractiveCloseRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		interactive.Default.Close(request.StreamID)
		return map[string]string{"stream_id": request.StreamID}, nil
	case teamapi.AskEventReplay:
		var request teamapi.EventReplayRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		items, err := server.events.Replay(request.After, request.Limit)
		if err != nil {
			return fail(err)
		}
		return items, nil
	case teamapi.AskEventAck:
		return map[string]bool{"acknowledged": true}, nil
	case teamapi.AskUserList:
		users, err := server.userList()
		if err != nil {
			return fail(err)
		}
		return users, nil
	case teamapi.AskUserMessage:
		var request teamapi.UserMessageRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		message := strings.TrimSpace(request.Message)
		if message == "" {
			return fail(errors.New("message is required"))
		}
		if len(message) > 4096 {
			return fail(errors.New("message must not exceed 4096 bytes"))
		}
		broadcast := teamapi.UserMessage{User: actor.Name, Message: message}
		publish(teamapi.EventUserMessage, broadcast)
		return broadcast, nil
	case teamapi.AskUserCreate:
		var request teamapi.UserCreateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		token, err := newUserToken()
		if err != nil {
			return fail(err)
		}
		user, err := db.UserCreate(request.Name, token)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventUserCreated, user)
		return teamapi.UserCredentials{User: user, Token: token}, nil
	case teamapi.AskUserUpdate:
		var request teamapi.UserUpdateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if strings.EqualFold(strings.TrimSpace(request.Name), "admin") {
			return fail(errors.New("the startup admin token cannot be refreshed through the user API"))
		}
		token, err := newUserToken()
		if err != nil {
			return fail(err)
		}
		user, err := db.UserUpdateToken(request.Name, token)
		if err != nil {
			return fail(err)
		}
		if server.closeUserConnections(user.UUID) {
			user.Connected = false
			user.LastSeen = time.Now().UTC()
			publish(teamapi.EventUserLogout, user)
		}
		publish(teamapi.EventUserUpdated, user)
		return teamapi.UserCredentials{User: user, Token: token}, nil
	case teamapi.AskUserDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if strings.EqualFold(strings.TrimSpace(request.Name), "admin") {
			return fail(errors.New("the startup admin user cannot be deleted"))
		}
		user, err := db.UserGet(request.Name)
		if err != nil {
			return fail(err)
		}
		if err := db.UserDelete(request.Name); err != nil {
			return fail(err)
		}
		if server.closeUserConnections(user.UUID) {
			user.LastSeen = time.Now().UTC()
		}
		user.Connected = false
		publish(teamapi.EventUserDeleted, user)
		return user, nil
	default:
		return fail(fmt.Errorf("unknown operation %q", envelope.Type))
	}
}

func (server *Server) updateListenerHosted(name string, expectedVersion int, mutate func(*listener.HTTPHostedFilesConfig) error) (teamapi.ListenerHostedConfiguration, error) {
	server.profileListenerMu.Lock()
	defer server.profileListenerMu.Unlock()
	current, hosted, err := server.listeners.HTTPHostedFiles(name)
	if err != nil {
		return teamapi.ListenerHostedConfiguration{}, err
	}
	if err := mutate(&hosted); err != nil {
		return teamapi.ListenerHostedConfiguration{}, err
	}
	if expectedVersion == 0 {
		expectedVersion = current.Config.ConfigVersion
	}
	updated, err := server.listeners.UpdateHTTPHostedFiles(name, hosted, expectedVersion)
	if err != nil {
		return teamapi.ListenerHostedConfiguration{}, err
	}
	return listener.ListenerHostedConfigurationToAPI(updated)
}
