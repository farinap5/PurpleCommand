package src

import "golang.org/x/crypto/ssh"

// Session struct define session properties
type Session struct {
	AuthKeys 	map[string]bool // keep the fingerprint of allowed keys
	PubKey		ssh.PublicKey
	Pty			Pty
	SockName	string
}

type Window struct {
	Width  int
	Height int
}

type WindowsConf struct {
	Height uint16
	Width  uint16
	x      uint16
	y      uint16
}

type Pty struct {
	Term string
	Window Window
}