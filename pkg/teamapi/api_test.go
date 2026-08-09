package teamapi

import (
	"encoding/json"
	"testing"
)

func TestReplyType(t *testing.T) {
	reply, err := ReplyType(AskListenerList)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "rpy.listener.list" {
		t.Fatalf("unexpected reply type %q", reply)
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
