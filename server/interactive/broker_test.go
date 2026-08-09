package interactive

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestBrokerPairsByExplicitStreamID(t *testing.T) {
	broker := New(time.Second)
	firstID, _ := broker.Open("first")
	secondID, _ := broker.Open("second")

	wrongServer, wrongPeer := net.Pipe()
	if err := broker.AttachImplant("not-a-stream", wrongServer); err != ErrStreamNotFound {
		t.Fatalf("wrong stream error = %v", err)
	}
	_ = wrongServer.Close()
	_ = wrongPeer.Close()

	implantServer, implantPeer := net.Pipe()
	clientServer, clientPeer := net.Pipe()
	defer implantPeer.Close()
	defer clientPeer.Close()
	if err := broker.AttachImplant(secondID, implantServer); err != nil {
		t.Fatal(err)
	}
	if err := broker.AttachClient(secondID, clientServer); err != nil {
		t.Fatal(err)
	}

	payload := []byte("stream-two")
	go func() { _, _ = clientPeer.Write(payload) }()
	_ = implantPeer.SetReadDeadline(time.Now().Add(time.Second))
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(implantPeer, received); err != nil {
		t.Fatal(err)
	}
	if string(received) != string(payload) {
		t.Fatalf("received %q", received)
	}

	firstImplantServer, firstImplantPeer := net.Pipe()
	defer firstImplantPeer.Close()
	if err := broker.AttachImplant(firstID, firstImplantServer); err != nil {
		t.Fatalf("first stream was consumed by second: %v", err)
	}
	broker.Close(firstID)
	broker.Close(secondID)
}
