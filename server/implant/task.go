package implant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"purpcmd/server"
	"time"
)

const (
	NIL = iota
	REG
)

func ParseCallback(d io.ReadCloser) int {
	r := base64.NewDecoder(base64.StdEncoding, d)
	var messageType uint16

	err := binary.Read(r, binary.BigEndian, &messageType)
	if err != nil {
		if err == io.EOF {
			return NIL
		}
	}


	if messageType == REG {
		println("registration request")
		i := new(ImplantMetadata)

		binary.Read(r, binary.BigEndian, &i.PID)
		binary.Read(r, binary.BigEndian, &i.SessionID)
		binary.Read(r, binary.BigEndian, &i.IP)
		binary.Read(r, binary.BigEndian, &i.Port)

		println("Pid: ", i.PID)
		return REG
	}

	return NIL
}

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