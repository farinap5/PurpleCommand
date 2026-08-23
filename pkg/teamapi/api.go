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
	AskSystemHello      = "ask.system.hello"
	AskSystemSnapshot   = "ask.system.snapshot"
	AskSystemHealth     = "ask.system.health"
	AskListenerList     = "ask.listener.list"
	AskListenerGet      = "ask.listener.get"
	AskListenerCreate   = "ask.listener.create"
	AskListenerUpdate   = "ask.listener.update"
	AskListenerStart    = "ask.listener.start"
	AskListenerStop     = "ask.listener.stop"
	AskListenerRestart  = "ask.listener.restart"
	AskListenerDelete   = "ask.listener.delete"
	AskSessionList      = "ask.session.list"
	AskSessionGet       = "ask.session.get"
	AskSessionTerminate = "ask.session.terminate"
	AskSessionDelete    = "ask.session.delete"
	AskCommandList      = "ask.command.list"
	AskCommandExecute   = "ask.command.execute"
	AskTaskCreate       = "ask.task.create"
	AskTaskList         = "ask.task.list"
	AskTaskGet          = "ask.task.get"
	AskLootList         = "ask.loot.list"
	AskLootGet          = "ask.loot.get"
	AskLootDelete       = "ask.loot.delete"
	AskScriptList       = "ask.script.list"
	AskScriptLoad       = "ask.script.load"
	AskScriptUnload     = "ask.script.unload"
	AskProfileList      = "ask.profile.list"
	AskProfileGet       = "ask.profile.get"
	AskProfileCreate    = "ask.profile.create"
	AskProfileUpdate    = "ask.profile.update"
	AskProfileDelete    = "ask.profile.delete"
	AskBuildCreate      = "ask.build.create"
	AskBuildGet         = "ask.build.get"
	AskBuildList        = "ask.build.list"
	AskInteractiveOpen  = "ask.interactive.open"
	AskInteractiveClose = "ask.interactive.close"
	AskEventReplay      = "ask.event.replay"
	AskEventAck         = "ask.event.ack"
	AskUserCreate       = "ask.user.create"
	AskUserUpdate = "ask.user.update"
	AskUserDelete = "ask.user.delete"
	AskUserList   = "ask.user.list"
	AskUserMessage = "ask.user.message"
)

const (
	EventListenerCreated   = "evt.listener.created"
	EventListenerStarted   = "evt.listener.started"
	EventListenerStopped   = "evt.listener.stopped"
	EventListenerFailed    = "evt.listener.failed"
	EventSessionRegistered = "evt.session.registered"
	EventSessionCheckin    = "evt.session.checkin"
	EventSessionDeleted    = "evt.session.deleted"
	EventTaskCreated       = "evt.task.created"
	EventTaskDispatched    = "evt.task.dispatched"
	EventTaskCompleted     = "evt.task.completed"
	EventLootCreated       = "evt.loot.created"
	EventLootDeleted       = "evt.loot.deleted"
	EventScriptLoaded      = "evt.script.loaded"
	EventScriptUnloaded    = "evt.script.unloaded"
	EventScriptOutput      = "evt.script.output"
	EventBuildStarted      = "evt.build.started"
	EventBuildOutput       = "evt.build.output"
	EventBuildCompleted    = "evt.build.completed"
	EventBuildFailed       = "evt.build.failed"
	EventUserLogin         = "evt.user.login"
	EventUserLogout        = "evt.user.logout"
	EventUserCreated       = "evt.user.created"
	EventUserUpdated       = "evt.user.updated"
	EventUserDeleted       = "evt.user.deleted"
	EventUserMessage	   = "evt.user.message"
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
	ServerID       string `json:"server_id"`
	ServerVersion  string `json:"server_version"`
	Protocol       int    `json:"protocol"`
	EventSequence  uint64 `json:"event_sequence"`
	ResyncRequired bool   `json:"resync_required,omitempty"`
}

type NameRequest struct {
	Name string `json:"name"`
}

type Listener struct {
	Name         string `json:"name"`
	UUID         string `json:"uuid"`
	Host         string `json:"host"`
	Port         string `json:"port"`
	Running      bool   `json:"running"`
	Persistent   bool   `json:"persistent"`
	Associations int    `json:"associations"`
}

type ListenerCreateRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host,omitempty"`
	Port       string `json:"port,omitempty"`
	Persistent *bool  `json:"persistent,omitempty"`
}

type ListenerUpdateRequest struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Session struct {
	Name        string    `json:"name"`
	UUID        string    `json:"uuid"`
	PayloadType string    `json:"payload_type"`
	User        string    `json:"user"`
	Hostname    string    `json:"hostname"`
	Process     string    `json:"process"`
	Socket      string    `json:"socket"`
	PID         uint32    `json:"pid"`
	Sleep       uint32    `json:"sleep"`
	Alive       bool      `json:"alive"`
	Terminating bool      `json:"terminating"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

type Command struct {
	PayloadType string `json:"payload_type"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CommandListRequest struct {
	PayloadType string `json:"payload_type"`
}

type CommandExecuteRequest struct {
	Session     string            `json:"session"`
	Name        string            `json:"name"`
	Arguments   string            `json:"arguments,omitempty"`
	Attachments map[string]string `json:"attachments,omitempty"`
}

type CommandExecuteReply struct {
	TaskIDs []string `json:"task_ids,omitempty"`
	Message string   `json:"message,omitempty"`
}

type TaskCreateRequest struct {
	Session string `json:"session"`
	Code    uint16 `json:"code"`
	Payload []byte `json:"payload,omitempty"`
}

type Task struct {
	ID           string    `json:"id"`
	Session      string    `json:"session"`
	Code         uint16    `json:"code"`
	Status       string    `json:"status"`
	Attempts     uint32    `json:"attempts"`
	Registered   time.Time `json:"registered"`
	LastSent     time.Time `json:"last_sent,omitempty"`
	ResponseTime time.Time `json:"response_time,omitempty"`
	Response     []byte    `json:"response,omitempty"`
}

type TaskListRequest struct {
	Session string `json:"session,omitempty"`
}

type TaskGetRequest struct {
	Session string `json:"session"`
	TaskID  string `json:"task_id"`
}

type LootRequest struct {
	UUID string `json:"uuid"`
}

type LootGetReply struct {
	Loot        Loot   `json:"loot"`
	DownloadURL string `json:"download_url"`
}

type Loot struct {
	UUID      string    `json:"uuid"`
	Session   string    `json:"session"`
	FileName  string    `json:"file_name"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type Script struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Loaded bool   `json:"loaded"`
	SHA256 string `json:"sha256,omitempty"`
}

type ScriptLoadRequest struct {
	Name     string `json:"name,omitempty"`
	Path     string `json:"path,omitempty"`
	UploadID string `json:"upload_id,omitempty"`
}

type Profile struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	LHOST     string `json:"lhost"`
	OS        string `json:"os"`
	ARCH      string `json:"arch"`
	URI       string `json:"uri"`
	UA        string `json:"ua"`
	Output    string `json:"output"`
	Template  string `json:"template"`
	PublicKey string `json:"public_key"`
}

type ProfileUpdateRequest struct {
	Name  string `json:"name"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type BuildRequest struct {
	Profile string `json:"profile"`
}

type Build struct {
	ID           string    `json:"id"`
	Profile      string    `json:"profile"`
	Status       string    `json:"status"`
	ArtifactName string    `json:"artifact_name,omitempty"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	DownloadURL  string    `json:"download_url,omitempty"`
}

type User struct {
	Name      string    `json:"name"`
	UUID      string    `json:"uuid"`
	Admin     bool      `json:"admin"`
	Connected bool      `json:"connected"`
	Created   time.Time `json:"created"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

type UserCreateRequest struct {
	Name string `json:"name"`
}

// UserUpdateRequest deliberately contains only the user name. Updating a user
// rotates the token; no other user properties can be changed through this API.
type UserUpdateRequest struct {
	Name string `json:"name"`
}

type UserCredentials struct {
	User  User   `json:"user"`
	Token string `json:"token"`
}

type Snapshot struct {
	Listeners     []Listener `json:"listeners"`
	Sessions      []Session  `json:"sessions"`
	Scripts       []Script   `json:"scripts"`
	Profiles      []Profile  `json:"profiles"`
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
