package implant

import (
	"encoding/binary"
	"io"
)

const (
	NIL = iota
	REG
)

func ParseCallback(r io.ReadCloser) int {
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