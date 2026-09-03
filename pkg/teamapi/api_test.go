package teamapi

import (
	"encoding/json"
	"testing"
)

func TestReplyType(t *testing.T) {
	cases := map[string]string{
		AskListenerList:       "rpy.listener.list",
		AskListenerGet:        "rpy.listener.get",
		AskListenerCreate:     "rpy.listener.create",
		AskListenerUpdate:     "rpy.listener.update",
		AskListenerStart:      "rpy.listener.start",
		AskListenerStop:       "rpy.listener.stop",
		AskListenerRestart:    "rpy.listener.restart",
		AskListenerDelete:     "rpy.listener.delete",
		AskListenerTypeList:   "rpy.listener-type.list",
		AskListenerTypeGet:    "rpy.listener-type.get",
		AskCarrierTypeList:    "rpy.listener-carrier.list",
		AskBuildCreate:        "rpy.build.create",
		AskBuildGet:           "rpy.build.get",
		AskBuildList:          "rpy.build.list",
		AskBuildDelete:        "rpy.build.delete",
		AskPayloadBuilderList: ReplyPayloadBuilderList,
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
