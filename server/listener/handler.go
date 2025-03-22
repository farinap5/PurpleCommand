package listener

import "net/http"

func root(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(404)
}