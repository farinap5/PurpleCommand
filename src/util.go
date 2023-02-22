package src

import "os"

func Err(err error) {
	if err != nil {
		println(err.Error())
		os.Exit(1)
	}
}