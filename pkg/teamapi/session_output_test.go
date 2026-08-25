package teamapi

import "testing"

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
