package implant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"

	"purpcmd/server"
)

func (i *Implant) TaskGet() (*Task, error) {
	for _, t := range i.Task {
		if !t.Sent && !t.Done {
			return t, nil
		}
	}
	return nil, errors.New("no pending task")
}

func TaskGetPtrById(ImplantName string, TaskID [8]byte) *Task {
	return ImplantMAP[ImplantName].TaskMap[TaskID]
}

func (t Task) TaskMarshal() []byte {
	b := new(bytes.Buffer)

	binary.Write(b, binary.BigEndian, t.Code)
	binary.Write(b, binary.BigEndian, t.ID)
	binary.Write(b, binary.BigEndian, uint32(len(t.Payload)))
	binary.Write(b, binary.BigEndian, t.Payload)

	return b.Bytes()
}

func TaskEncode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func (t *Task) TaskSetResponsePayload(payload []byte) {
	t.ResponseTime = time.Now()
	t.Done = true
	t.Response = payload
}

func TaskNew(code uint16, payload []byte) *Task {
	return &Task{
		ID:         server.RandomAlphanumericID8(),
		Code:       code,
		Registered: time.Now(),
		Payload:    payload,
	}
}
