package implantbuilder

import (
	"bytes"
	"sync"
)

// MaxBuildOutputBytes bounds the output retained and persisted for one build
// command. The full command output is still written to the teamserver console.
const MaxBuildOutputBytes = 256 << 10

// BuildOutputCapture retains a concurrency-safe, bounded copy of command
// output. os/exec may write stdout and stderr from separate goroutines.
type BuildOutputCapture struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	truncated bool
}

func (capture *BuildOutputCapture) Write(value []byte) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	originalLength := len(value)
	remaining := MaxBuildOutputBytes - capture.buffer.Len()
	if remaining <= 0 {
		capture.truncated = capture.truncated || originalLength > 0
		return originalLength, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		capture.truncated = true
	}
	_, _ = capture.buffer.Write(value)
	return originalLength, nil
}

func (capture *BuildOutputCapture) String() string {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	result := capture.buffer.String()
	if capture.truncated {
		result += "\n[build output truncated]\n"
	}
	return result
}
