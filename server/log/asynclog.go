package log

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	_ "unsafe" // Required for go:linkname

	"github.com/c-bata/go-prompt"
)

// Map the local variable "consoleWriter" to the one of go-prompt
//
//go:linkname consoleWriter github.com/c-bata/go-prompt.consoleWriter
var consoleWriter prompt.ConsoleWriter

var asyncOutputMu sync.Mutex

func AsyncWriteStdout(a ...any) {
	asyncOutputMu.Lock()
	defer asyncOutputMu.Unlock()

	message := fmt.Sprint(a...)
	if consoleWriter == nil {
		fmt.Print(message)
		if !strings.HasSuffix(message, "\n") {
			fmt.Print("\n")
		}
		return
	}

	consoleWriter.EraseLine()                            // Erase current line
	consoleWriter.EraseDown()                            // Required to remove the completions menu
	consoleWriter.WriteRawStr("\r" + message)            // '\r' to go back to the start of line
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH) // Required to force the re-render of the prompt
}

func AsyncWriteStdoutSuccs(a ...any) {
	AsyncWriteStdout("[\u001B[1;32mOK\u001B[0;0m]- " + fmt.Sprint(a...))
}

func AsyncWriteStdoutErr(a ...any) {
	AsyncWriteStdout("[\u001B[1;31m!\u001B[0;0m]- " + fmt.Sprint(a...))
}

func AsyncWriteStdoutAlert(a ...any) {
	AsyncWriteStdout("[\u001B[1;31m!\u001B[0;0m]- " + fmt.Sprint(a...))
}

func AsyncWriteStdoutInfo(a ...any) {
	AsyncWriteStdout("[\u001B[1;34mi\u001B[0;0m]- " + fmt.Sprint(a...))
}
