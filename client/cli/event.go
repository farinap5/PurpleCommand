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
	case teamapi.EventBuildQueued, teamapi.EventBuildStarted, teamapi.EventBuildCompleted,
		teamapi.EventBuildFailed, teamapi.EventBuildDeleted:
		showBuildLifecycle(cli, event)
	case teamapi.EventBuildOutput:
		showBuildOutput(cli, event)
	case teamapi.EventPayloadBuilderRegistered, teamapi.EventPayloadBuilderUnregistered:
		showPayloadBuilderLifecycle(cli, event)
	default:
	}
}

func showBuildLifecycle(cli *CLI, event teamapi.Envelope) {
	if !cli.inMode(modeProfile) {
		return
	}
	var build teamapi.Build
	if err := teamapi.DecodeData(event, &build); err != nil {
		log.AsyncWriteStdoutErr(err.Error())
		return
	}
	message := event.Type + " " + build.ID + " " + build.Profile
	if build.Builder != "" {
		message += " " + build.Builder
	}
	message += " " + build.Status
	if build.Error != "" {
		message += ": " + build.Error
	}
	log.AsyncWriteStdoutInfo(message + "\n")
}

func showBuildOutput(cli *CLI, event teamapi.Envelope) {
	var output teamapi.BuildOutput
	if err := teamapi.DecodeData(event, &output); err != nil {
		log.AsyncWriteStdoutErr(err.Error())
		return
	}
	if !cli.shouldShowBuildOutput(output.Profile) {
		return
	}
	message := output.Message
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	log.AsyncWriteStdoutInfo("[build " + output.BuildID + "] " + message)
}

func showPayloadBuilderLifecycle(cli *CLI, event teamapi.Envelope) {
	if !cli.inMode(modeProfile) {
		return
	}
	var builder teamapi.PayloadBuilder
	if err := teamapi.DecodeData(event, &builder); err != nil {
		log.AsyncWriteStdoutErr(err.Error())
		return
	}
	log.AsyncWriteStdoutInfo(event.Type + " " + builder.Name + "\n")
}

func (cli *CLI) inMode(expected mode) bool {
	cli.mu.RLock()
	defer cli.mu.RUnlock()
	return cli.mode == expected
}

func (cli *CLI) shouldShowBuildOutput(profile string) bool {
	cli.mu.RLock()
	defer cli.mu.RUnlock()
	return cli.mode == modeProfile && (cli.selectedProfile == "" || cli.selectedProfile == profile)
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
