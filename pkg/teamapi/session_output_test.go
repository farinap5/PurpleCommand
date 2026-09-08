package teamapi

import (
	"bytes"
	"testing"
)

func TestSessionOutputRoundTrip(t *testing.T) {
	want := SessionOutput{
		Session: "677222",
		TaskID:  "12345678",
		Message: "Current directory: /tmp",
		Source:  "lua",
	}
	data, err := MarshalData(want)
	if err != nil {
		t.Fatal(err)
	}
	var got SessionOutput
	if err := DecodeData(Envelope{Data: data}, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("session output = %#v, want %#v", got, want)
	}
}

func TestSessionOutputWithoutTaskOmitsTaskID(t *testing.T) {
	want := SessionOutput{
		Session: "677222",
		Message: "Starting command",
		Source:  "lua",
	}
	data, err := MarshalData(want)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"task_id"`)) {
		t.Fatalf("taskless output contains task_id: %s", data)
	}
	var got SessionOutput
	if err := DecodeData(Envelope{Data: data}, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("session output = %#v, want %#v", got, want)
	}
}
