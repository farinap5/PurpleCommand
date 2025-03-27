package implant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"purpcmd/server"
	"time"
)

const (
	NIL = iota // Nothing
	REG // Register - Used by the implant to register itself
	CHK // Check - Used by the implant to check for new tasks
)

func (i *Implant)TaskGet() (*Task, error) {
	tasks := i.Task
	if tasks == nil {
		return nil, errors.New("no task")
	}

	var t *Task
	if len(tasks) == 1 {
		t = tasks[0]
	} else {
		t = tasks[len(tasks)-1]
	}


	return t, nil
}

func (t Task)TaskMarshal() []byte {
	b := new(bytes.Buffer)

	binary.Write(b, binary.BigEndian, t.Code)
	binary.Write(b, binary.BigEndian, t.ID)
	binary.Write(b, binary.BigEndian, len(t.Payload))
	binary.Write(b, binary.BigEndian, t.Payload)

	return b.Bytes()
}

func TaskEncode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func TaskNew(code uint16, payload []byte) *Task {
	return &Task{
		ID: string(server.RandomString(16)),
		Code: code,
		Registered: time.Now(),
		Payload: payload,
	}
}