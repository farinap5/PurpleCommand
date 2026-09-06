package teamapi

import (
	"encoding/json"
	"testing"
)

func TestReplyType(t *testing.T) {
	cases := map[string]string{
		AskListenerList:                "rpy.listener.list",
		AskListenerGet:                 "rpy.listener.get",
		AskListenerCreate:              "rpy.listener.create",
		AskListenerUpdate:              "rpy.listener.update",
		AskListenerStart:               "rpy.listener.start",
		AskListenerStop:                "rpy.listener.stop",
		AskListenerRestart:             "rpy.listener.restart",
		AskListenerDelete:              "rpy.listener.delete",
		AskListenerHosted:              "rpy.listener.hosted",
		AskListenerHostedSet:           "rpy.listener.hosted.set",
		AskListenerHostedAdd:           "rpy.listener.hosted.add",
		AskListenerHostedRemove:        "rpy.listener.hosted.remove",
		AskListenerHostedNotFoundSet:   "rpy.listener.hosted.not-found.set",
		AskListenerHostedNotFoundClear: "rpy.listener.hosted.not-found.clear",
		AskProfileListenerSet:          "rpy.profile.listener.set",
		AskListenerTypeList:            "rpy.listener-type.list",
		AskListenerTypeGet:             "rpy.listener-type.get",
		AskCarrierTypeList:             "rpy.listener-carrier.list",
		AskBuildCreate:                 "rpy.build.create",
		AskBuildGet:                    "rpy.build.get",
		AskBuildList:                   "rpy.build.list",
		AskBuildDelete:                 "rpy.build.delete",
		AskPayloadBuilderList:          ReplyPayloadBuilderList,
	}
	for request, expected := range cases {
		reply, err := ReplyType(request)
		if err != nil {
			t.Fatalf("reply type for %q: %v", request, err)
		}
		if reply != expected {
			t.Fatalf("reply type for %q = %q, want %q", request, reply, expected)
		}
	}
	if _, err := ReplyType("evt.listener.started"); err == nil {
		t.Fatal("expected non-request type to be rejected")
	}
}

func TestDecodeProfileListenerSetRequest(t *testing.T) {
	envelope := Envelope{Data: json.RawMessage(`{"name":"linux-impl","listener_uuid":"listener-id"}`)}
	var request ProfileListenerSetRequest
	if err := DecodeData(envelope, &request); err != nil {
		t.Fatal(err)
	}
	if request.Name != "linux-impl" || request.ListenerUUID == nil || *request.ListenerUUID != "listener-id" {
		t.Fatalf("decoded listener attachment request = %#v", request)
	}
	envelope.Data = json.RawMessage(`{"name":"linux-impl","listener_uuid":""}`)
	if err := DecodeData(envelope, &request); err != nil {
		t.Fatal(err)
	}
	if request.ListenerUUID == nil || *request.ListenerUUID != "" {
		t.Fatalf("decoded detach request = %#v", request)
	}
	for _, data := range []string{`{"name":"linux-impl"}`, `{"name":"linux-impl","listener_uuid":null}`} {
		request = ProfileListenerSetRequest{}
		envelope.Data = json.RawMessage(data)
		if err := DecodeData(envelope, &request); err != nil {
			t.Fatal(err)
		}
		if request.ListenerUUID != nil {
			t.Fatalf("absent listener UUID decoded as present: %#v", request)
		}
	}
	envelope.Data = json.RawMessage(`{"name":"linux-impl","listener_uuid":"","unexpected":true}`)
	if err := DecodeData(envelope, &request); err == nil {
		t.Fatal("expected unknown attachment field to be rejected")
	}
}

func TestDecodeListenerHostedRequests(t *testing.T) {
	envelope := Envelope{Data: json.RawMessage(`{
		"name":"http","url_path":"/download","file":{"source_path":"payload.bin","status":202,"headers":{"X-Test":"yes"}},
		"expected_config_version":7
	}`)}
	var request ListenerHostedAddRequest
	if err := DecodeData(envelope, &request); err != nil {
		t.Fatal(err)
	}
	if request.Name != "http" || request.URLPath != "/download" || request.File.SourcePath != "payload.bin" ||
		request.File.Status != 202 || request.File.Headers["X-Test"] != "yes" || request.ExpectedConfigVersion != 7 {
		t.Fatalf("decoded hosted add request = %#v", request)
	}
	envelope.Data = json.RawMessage(`{"name":"http","url_path":"/download","file":{"source_path":"payload.bin"},"unexpected":true}`)
	if err := DecodeData(envelope, &request); err == nil {
		t.Fatal("expected unknown hosted-file request field to be rejected")
	}
}

func TestDecodeDataRejectsUnknownFields(t *testing.T) {
	envelope := Envelope{Data: json.RawMessage(`{"name":"main","unexpected":true}`)}
	var request NameRequest
	if err := DecodeData(envelope, &request); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestDecodeBuildRequestAcceptsBuilder(t *testing.T) {
	envelope := Envelope{Data: json.RawMessage(`{"profile":"linux-impl","builder":"implant-builder-linux-amd64"}`)}
	var request BuildRequest
	if err := DecodeData(envelope, &request); err != nil {
		t.Fatal(err)
	}
	if request.Profile != "linux-impl" || request.Builder != "implant-builder-linux-amd64" {
		t.Fatalf("decoded build request = %#v", request)
	}
}
