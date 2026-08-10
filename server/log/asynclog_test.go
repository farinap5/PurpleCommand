package log

import "testing"

func TestAsyncWriteStdoutWithoutPrompt(t *testing.T) {
	previous := consoleWriter
	consoleWriter = nil
	t.Cleanup(func() { consoleWriter = previous })

	AsyncWriteStdoutInfo("headless teamserver")
}
