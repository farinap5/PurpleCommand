package cli

import (
	"purpcmd/server/log"
	"purpcmd/pkg/teamapi"
)

func eventHandler(event teamapi.Envelope) {
	switch event.Type {
	case "evt.task.created":
	case teamapi.EventTaskCompleted:
		showTask(event)
	default:
	}
}

func showTask(event teamapi.Envelope) {
	var task teamapi.Task
	if err := teamapi.DecodeData(event, &task); err != nil {
		log.AsyncWriteStdoutErr(err.Error())
	}
	log.AsyncWriteStdoutInfo(string(task.Response))
}