package server

import "crypto/rand"

func RandomString(length int) []byte {
	b := make([]byte, length)
	rand.Read(b)
	return b
}

func RandomBytes8() [8]byte {
	var b [8]byte
	rand.Read(b[:])
	return b
}
