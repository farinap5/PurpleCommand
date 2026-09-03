package core

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestHTTPHelpersUseDefaultImplantProtocol(t *testing.T) {
	requests := 0
	issues := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodPost || request.URL.Path != "/" || request.URL.RawQuery != "" {
				issues <- fmt.Sprintf("registration request = %s %s", request.Method, request.URL.String())
			}
		case 2:
			if request.Method != http.MethodPost || request.URL.Query().Get("a") != "12345" {
				issues <- fmt.Sprintf("response request = %s %s", request.Method, request.URL.String())
			}
		case 3:
			cookie, err := request.Cookie("a")
			if request.Method != http.MethodGet || request.URL.Query().Get("a") != "12345" || err != nil || cookie.Value != "encoded-check" {
				issues <- fmt.Sprintf("check request = %s %s cookie=%#v err=%v", request.Method, request.URL.String(), cookie, err)
			}
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte("Hi!"))
		}
	}))
	defer server.Close()

	request := HTTPNew(12345)
	request.URL = server.URL + "/"
	if err := request.PostRegistering([]byte("registration")); err != nil {
		t.Fatal(err)
	}
	if err := request.Post([]byte("response")); err != nil {
		t.Fatal(err)
	}
	body, err := request.Get([]byte("encoded-check"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || string(data) != "Hi!" {
		t.Fatalf("check body = %q, %v", data, err)
	}
	close(issues)
	for issue := range issues {
		t.Error(issue)
	}
}

func TestHTTPHelpersReturnTransportAndStatusErrors(t *testing.T) {
	request := HTTPNew(12345)
	request.URL = "http://listener.invalid/"
	request.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})
	if err := request.PostRegistering(nil); err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("registration transport error = %v", err)
	}
	if err := request.Post(nil); err == nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("response transport error = %v", err)
	}
	if body, err := request.Get(nil); err == nil || body != nil || !strings.Contains(err.Error(), "network unavailable") {
		t.Fatalf("check transport result = body:%v err:%v", body, err)
	}

	request.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader("rejected")),
			Header:     make(http.Header),
		}, nil
	})
	if err := request.PostRegistering(nil); err == nil {
		t.Fatal("registration HTTP error was ignored")
	}
	if err := request.Post(nil); err == nil {
		t.Fatal("response HTTP error was ignored")
	}
}
