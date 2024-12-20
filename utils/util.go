package utils

import (
	"io"
	"log"
	"net"
	"os"
)

func Err(err error, i int) {
	if err != nil {
		log.Println(err.Error(), i)
		os.Exit(1)
	}
}

// sync io from those connectios
func CopyIO(src, dest net.Conn) {
	defer src.Close()
	defer dest.Close()
	io.Copy(src, dest)
}
