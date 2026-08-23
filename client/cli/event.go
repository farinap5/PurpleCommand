package cli

import (
	"purpcmd/pkg/teamapi"
	"purpcmd/server/log"
)

func eventHandler(event teamapi.Envelope) {
	switch event.Type {
	case "evt.task.created":
	case teamapi.EventTaskCompleted:
		showTask(event)
	case teamapi.EventUserMessage:
		showUserMessage(event)
	default:
	}
}

func showUserMessage(event teamapi.Envelope) {
	var message teamapi.UserMessage
	if err := teamapi.DecodeData(event, &message); err != nil {
		log.AsyncWriteStdoutErr(err.Error())
		return
	}
	log.AsyncWriteStdoutInfo(message.User + ": " + message.Message + "\n")
}

func showTask(event teamapi.Envelope) {
	var task teamapi.Task
	if err := teamapi.DecodeData(event, &task); err != nil {
		log.AsyncWriteStdoutErr(err.Error())
	}
	log.AsyncWriteStdoutInfo(string(task.Response))
}
