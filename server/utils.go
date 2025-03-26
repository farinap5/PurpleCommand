package server

import "math/rand"


func RandomString(length int) []byte {
	var letterBytes = ""

	b := make([]byte, length)
	letterBytes = letterBytes+"1234567890abcdef"

	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}

	return b
}