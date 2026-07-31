package listener

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootRejectsMalformedCallbackWithoutAssociation(t *testing.T) {
	listener := newListener("test", "11111111-1111-1111-1111-111111111111", "127.0.0.1", "0", false)
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("%%%"))
	response := httptest.NewRecorder()

	listener.root(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if listener.Association != 0 {
		t.Fatalf("malformed registration changed association to %d", listener.Association)
	}
}

func TestRootRequiresNamedCallbackCookie(t *testing.T) {
	listener := newListener("test", "11111111-1111-1111-1111-111111111111", "127.0.0.1", "0", false)
	request := httptest.NewRequest(http.MethodGet, "/?a=12345", nil)
	request.AddCookie(&http.Cookie{Name: "unrelated", Value: "AA=="})
	response := httptest.NewRecorder()

	listener.root(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestRootRejectsUnsupportedCallbackMethod(t *testing.T) {
	listener := newListener("test", "11111111-1111-1111-1111-111111111111", "127.0.0.1", "0", false)
	request := httptest.NewRequest(http.MethodPut, "/", nil)
	response := httptest.NewRecorder()

	listener.root(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if response.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("Allow header = %q", response.Header().Get("Allow"))
	}
}
