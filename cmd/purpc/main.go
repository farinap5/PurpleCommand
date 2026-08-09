package main

import (
	"fmt"
	"os"

	"purpcmd/client/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "purpc:", err)
		os.Exit(1)
	}
}
