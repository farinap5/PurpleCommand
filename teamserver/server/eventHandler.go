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
			Listeners: listener.APIList(), Sessions: implant.APIListSessions(),
			Scripts: lua.APIListScripts(), Profiles: implantbuilder.APIListProfiles(),
			Builds: server.builds.List(), Commands: lua.APIListCommands(""),
			Users: users, EventSequence: latest,
		}, nil
	case teamapi.AskListenerList:
		return listener.APIList(), nil
	case teamapi.AskListenerGet:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIGet(request.Name)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskListenerCreate:
		var request teamapi.ListenerCreateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APICreate(request)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerCreated, item)
		return item, nil
	case teamapi.AskListenerUpdate:
		var request teamapi.ListenerUpdateRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIUpdate(request)
		if err != nil {
			return fail(err)
		}
		return item, nil
	case teamapi.AskListenerStart:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIStart(request.Name)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerStarted, item)
		return item, nil
	case teamapi.AskListenerStop:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIStop(request.Name)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerStopped, item)
		return item, nil
	case teamapi.AskListenerRestart:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := listener.APIRestart(request.Name)
		if err != nil {
			return fail(err)
		}
		publish(teamapi.EventListenerStarted, item)
		return item, nil
	case teamapi.AskListenerDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := listener.APIDelete(request.Name); err != nil {
			return fail(err)
		}
		return map[string]string{"name": request.Name}, nil
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
		return item, nil
	case teamapi.AskProfileDelete:
		var request teamapi.NameRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		if err := implantbuilder.APIDeleteProfile(request.Name); err != nil {
			return fail(err)
		}
		return map[string]string{"name": request.Name}, nil
	case teamapi.AskBuildCreate:
		var request teamapi.BuildRequest
		if err := teamapi.DecodeData(envelope, &request); err != nil {
			return fail(err)
		}
		item, err := server.builds.Create(request.Profile)
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
