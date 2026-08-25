package cli

import (
	"strings"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/log"
)

func eventHandler(cli *CLI, event teamapi.Envelope) {
	switch event.Type {
	case "evt.task.created":
	case teamapi.EventTaskCompleted:
		showTask(event)
	case teamapi.EventSessionOutput:
		showSessionOutput(cli, event)
	case teamapi.EventUserMessage:
		showUserMessage(event)
	default:
	}
}

func showSessionOutput(cli *CLI, event teamapi.Envelope) {
	var output teamapi.SessionOutput
	if err := teamapi.DecodeData(event, &output); err != nil {
		log.AsyncWriteStdoutErr(err.Error())
		return
	}
	if !cli.shouldShowSessionOutput(output.Session) {
		return
	}
	message := output.Message
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	log.AsyncWriteStdoutInfo(message)
}

func (cli *CLI) shouldShowSessionOutput(sessionID string) bool {
	cli.mu.RLock()
	defer cli.mu.RUnlock()
	return cli.mode == modeSession && cli.selectedSession == sessionID
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
