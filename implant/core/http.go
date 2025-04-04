package core

import (
	"bytes"
	"io"
	"net/http"
)

type Request struct {
	Socket 	string
	URL 	string
	Client 	*http.Client
}

func HTTPNew() *Request {
	return &Request{
		Client: &http.Client{},
	}
}

func (r *Request) HTTPSetSocket(s string) {
	r.Socket = s
}

func (r *Request) HTTPSetURL(secureconn bool, path string) {
	scm := "http://"
	if secureconn {scm = "https://"}

	r.URL = scm + r.Socket + path
}

func (r *Request) Post(data []byte) error {
	req, err := http.NewRequest("POST", r.URL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	res, err := r.Client.Do(req)
	if err != nil {
		panic(err)
	}

	defer res.Body.Close()
	return nil
}

func (r *Request) Get(data []byte) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", r.URL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Cookie", "a=" + string(data))

	res, err := r.Client.Do(req)
	if err != nil {
		panic(err)
	}

	return res.Body, nil
}