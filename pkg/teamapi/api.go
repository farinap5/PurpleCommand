package teamapi

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	Version           = 1
	Subprotocol       = "purpcmd.v1"
	BrowserAuthPrefix = "purpcmd.auth."
	MaxControlMessage = 1 << 20
)

const (
	AskSystemHello                 = "ask.system.hello"
	AskSystemSnapshot              = "ask.system.snapshot"
	AskSystemHealth                = "ask.system.health"
	AskListenerList                = "ask.listener.list"
	AskListenerGet                 = "ask.listener.get"
	AskListenerCreate              = "ask.listener.create"
	AskListenerUpdate              = "ask.listener.update"
	AskListenerStart               = "ask.listener.start"
	AskListenerStop                = "ask.listener.stop"
	AskListenerRestart             = "ask.listener.restart"
	AskListenerDelete              = "ask.listener.delete"
	AskListenerHosted              = "ask.listener.hosted"
	AskListenerHostedSet           = "ask.listener.hosted.set"
	AskListenerHostedAdd           = "ask.listener.hosted.add"
	AskListenerHostedRemove        = "ask.listener.hosted.remove"
	AskListenerHostedNotFoundSet   = "ask.listener.hosted.not-found.set"
	AskListenerHostedNotFoundClear = "ask.listener.hosted.not-found.clear"
	AskListenerTypeList            = "ask.listener-type.list"
	AskListenerTypeGet             = "ask.listener-type.get"
	AskCarrierTypeList             = "ask.listener-carrier.list"
	AskSessionList                 = "ask.session.list"
	AskSessionGet                  = "ask.session.get"
	AskSessionTerminate            = "ask.session.terminate"
	AskSessionDelete               = "ask.session.delete"
	AskCommandList                 = "ask.command.list"
	AskCommandExecute              = "ask.command.execute"
	AskTaskCreate                  = "ask.task.create"
	AskTaskList                    = "ask.task.list"
	AskTaskGet                     = "ask.task.get"
	AskLootList                    = "ask.loot.list"
	AskLootGet                     = "ask.loot.get"
	AskLootDelete                  = "ask.loot.delete"
	AskScriptList                  = "ask.script.list"
	AskScriptLoad                  = "ask.script.load"
	AskScriptUnload                = "ask.script.unload"
	AskProfileList                 = "ask.profile.list"
	AskProfileGet                  = "ask.profile.get"
	AskProfileCreate               = "ask.profile.create"
	AskProfileUpdate               = "ask.profile.update"
	AskProfileDelete               = "ask.profile.delete"
	AskProfileListenerSet          = "ask.profile.listener.set"
	AskBuildCreate                 = "ask.build.create"
	AskBuildGet                    = "ask.build.get"
	AskBuildList                   = "ask.build.list"
	AskBuildDelete                 = "ask.build.delete"
	AskInteractiveOpen             = "ask.interactive.open"
	AskInteractiveClose            = "ask.interactive.close"
	AskEventReplay                 = "ask.event.replay"
	AskEventAck                    = "ask.event.ack"
	AskUserCreate                  = "ask.user.create"
	AskUserUpdate                  = "ask.user.update"
	AskUserDelete                  = "ask.user.delete"
	AskUserList                    = "ask.user.list"
	AskUserMessage                 = "ask.user.message"

	AskSpeakerList    = "ask.speaker.list"
	AskSpeakerGet     = "ask.speaker.get"
	AskSpeakerCreate  = "ask.speaker.create"
	AskSpeakerUpdate  = "ask.speaker.update"
	AskSpeakerStart   = "ask.speaker.start"
	AskSpeakerStop    = "ask.speaker.stop"
	AskSpeakerRestart = "ask.speaker.restart"
	AskSpeakerDelete  = "ask.speaker.delete"
)

const (
	AskPayloadBuilderList   = "ask.payload-builder.list"
	ReplyPayloadBuilderList = "rpl.payload-builder.list"
)

const (
	EventListenerCreated       = "evt.listener.created"
	EventListenerStarted       = "evt.listener.started"
	EventListenerStopped       = "evt.listener.stopped"
	EventListenerDeleted       = "evt.listener.deleted"
	EventListenerUpdated       = "evt.listener.updated"
	EventListenerStarting      = "evt.listener.starting"
	EventListenerStopping      = "evt.listener.stopping"
	EventListenerFailed        = "evt.listener.failed"
	EventListenerHostedUpdated = "evt.listener.hosted.updated"
	EventSessionRegistered     = "evt.session.registered"
	EventSessionCheckin        = "evt.session.checkin"
	EventSessionDeleted        = "evt.session.deleted"
	EventSessionOutput         = "evt.session.output"
	EventTaskCreated           = "evt.task.created"
	EventTaskDispatched        = "evt.task.dispatched"
	EventTaskCompleted         = "evt.task.completed"
	EventLootCreated           = "evt.loot.created"
	EventLootDeleted           = "evt.loot.deleted"
	EventScriptLoaded          = "evt.script.loaded"
	EventScriptUnloaded        = "evt.script.unloaded"
	EventScriptOutput          = "evt.script.output"
	EventProfileCreated        = "evt.profile.created"
	EventProfileUpdated        = "evt.profile.updated"
	EventProfileDeleted        = "evt.profile.deleted"
	EventBuildQueued           = "evt.build.queued"
	EventBuildStarted          = "evt.build.started"
	EventBuildOutput           = "evt.build.output"
	EventBuildCompleted        = "evt.build.completed"
	EventBuildFailed           = "evt.build.failed"
	EventBuildDeleted          = "evt.build.deleted"
	EventUserLogin             = "evt.user.login"
	EventUserLogout            = "evt.user.logout"
	EventUserCreated           = "evt.user.created"
	EventUserUpdated           = "evt.user.updated"
	EventUserDeleted           = "evt.user.deleted"
	EventUserMessage           = "evt.user.message"

	EventSpeakerCreated      = "evt.speaker.created"
	EventSpeakerConnecting   = "evt.speaker.connecting"
	EventSpeakerConnected    = "evt.speaker.connected"
	EventSpeakerDisconnected = "evt.speaker.disconnected"
	EventSpeakerFailed       = "evt.speaker.failed"
	EventSpeakerStopped      = "evt.speaker.stopped"
	EventSpeakerUpdated      = "evt.speaker.updated"
	EventSpeakerDeleted      = "evt.speaker.deleted"
)

const (
	EventPayloadBuilderRegistered   = "evt.payload-builder.registered"
	EventPayloadBuilderUnregistered = "evt.payload-builder.unregistered"
)

type Envelope struct {
	Version  int             `json:"version"`
	Type     string          `json:"type"`
	ID       string          `json:"id,omitempty"`
	ClientID string          `json:"client_id,omitempty"`
	Sequence uint64          `json:"sequence,omitempty"`
	Time     time.Time       `json:"time,omitempty"`
	OK       bool            `json:"ok,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    *APIError       `json:"error,omitempty"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func ReplyType(requestType string) (string, error) {
	if !strings.HasPrefix(requestType, "ask.") {
		return "", errors.New("request type must begin with ask.")
	}
	if requestType == AskPayloadBuilderList {
		return ReplyPayloadBuilderList, nil
	}
	return "rpy." + strings.TrimPrefix(requestType, "ask."), nil
}

func MarshalData(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage(`{}`), nil
	}
	return json.Marshal(value)
}

func DecodeData(envelope Envelope, destination any) error {
	if len(envelope.Data) == 0 {
		envelope.Data = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(strings.NewReader(string(envelope.Data)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

type HelloRequest struct {
	LastEventSequence uint64 `json:"last_event_sequence,omitempty"`
}

type HelloReply struct {
	ServerID         string `json:"server_id"`
	ServerVersion    string `json:"server_version"`
	Protocol         int    `json:"protocol"`
	EventSequence    uint64 `json:"event_sequence"`
	ResyncRequired   bool   `json:"resync_required,omitempty"`
	HistoryTruncated bool   `json:"-"`
}

type NameRequest struct {
	Name string `json:"name"`
}

type Session struct {
	Name             string    `json:"name"`
	UUID             string    `json:"uuid"`
	PayloadType      string    `json:"payload_type"`
	Transport        string    `json:"transport"`
	Speaker          string    `json:"speaker,omitempty"`
	SpeakerUUID      string    `json:"speaker_uuid,omitempty"`
	Listener         string    `json:"listener,omitempty"`
	ListenerUUID     string    `json:"listener_uuid,omitempty"`
	User             string    `json:"user"`
	Hostname         string    `json:"hostname"`
	Process          string    `json:"process"`
	Socket           string    `json:"socket"`
	PID              uint32    `json:"pid"`
	Sleep            uint32    `json:"sleep"`
	Alive            bool      `json:"alive"`
	Liveness         string    `json:"liveness"`
	HealthMonitoring bool      `json:"health_monitoring"`
	Terminating      bool      `json:"terminating"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
}

type Snapshot struct {
	Listeners     []Listener `json:"listeners"`
	Speakers      []Speaker  `json:"speakers"`
	Sessions      []Session  `json:"sessions"`
	Scripts       []Script   `json:"scripts"`
	Profiles      []Profile  `json:"profiles"`
	Builds        []Build    `json:"builds"`
	Commands      []Command  `json:"commands"`
	Users         []User     `json:"users"`
	EventSequence uint64     `json:"event_sequence"`
}

type EventRecord struct {
	Sequence uint64          `json:"sequence"`
	Type     string          `json:"type"`
	Time     time.Time       `json:"time"`
	Data     json.RawMessage `json:"data"`
}

type EventReplayRequest struct {
	After uint64 `json:"after"`
	Limit int    `json:"limit,omitempty"`
}

type EventAckRequest struct {
	Sequence uint64 `json:"sequence"`
}

type InteractiveOpenRequest struct {
	Session string `json:"session"`
}

type InteractiveCloseRequest struct {
	StreamID string `json:"stream_id"`
}

type InteractiveOpenReply struct {
	StreamID string    `json:"stream_id"`
	URL      string    `json:"url"`
	Expires  time.Time `json:"expires"`
}
