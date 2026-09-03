package core

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Request struct {
	Socket string
	URL    string
	Client *http.Client

	sessionName uint32
}

func HTTPNew(name uint32) *Request {
	return &Request{
		Client:      &http.Client{Timeout: 45 * time.Second},
		sessionName: name,
	}
}

func (r *Request) HTTPSetSocket(s string) {
	r.Socket = s
}

func (r *Request) HTTPSetURL(secureconn bool, path string) {
	scm := "http://"
	if secureconn {
		scm = "https://"
	}

	r.URL = scm + r.Socket + path
}

func (r *Request) PostRegistering(data []byte) error {
	req, err := http.NewRequest("POST", r.URL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	res, err := r.Client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, res.Body)
		return fmt.Errorf("registration returned HTTP status %d", res.StatusCode)
	}
	return nil
}

func (r *Request) Post(data []byte) error {
	req, err := http.NewRequest("POST", r.URL+"?a="+fmt.Sprintf("%d", r.sessionName), bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	res, err := r.Client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, res.Body)
		return fmt.Errorf("response returned HTTP status %d", res.StatusCode)
	}
	return nil
}

func (r *Request) Get(data []byte) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", r.URL+"?a="+fmt.Sprintf("%d", r.sessionName), nil)

	//req.URL.Query().Add("a",fmt.Sprintf("%d", r.sessionName))
	if err != nil {
		return nil, err
	}

	req.Header.Add("Cookie", "a="+string(data))

	res, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}

	return res.Body, nil
}
