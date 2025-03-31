package server

import "math/rand"


func RandomString(length int) []byte {
	b := make([]byte, length)
	letterBytes := "1234567890abcdef"

	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}

	return b
}

func RandomBytes8() [8]byte {
	var b [8]byte
	letterBytes := "1234567890abcdef"

	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return b
}