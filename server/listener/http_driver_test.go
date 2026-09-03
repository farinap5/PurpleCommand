package listener

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	implantwire "purpcmd/implant"
	implantcore "purpcmd/implant/core"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/pkg/teamapi"
	serverimplant "purpcmd/server/implant"
	"purpcmd/server/interactive"

	"github.com/gorilla/websocket"
)

func newHTTPDriverTest(t *testing.T) (*Registry, *HTTPDriver) {
	t.Helper()
	registry := NewRegistry()
	if err := RegisterHTTPBuiltins(registry); err != nil {
		t.Fatal(err)
	}
	driver, found := registry.Driver("http")
	if !found {
		t.Fatal("HTTP driver was not registered")
	}
	return registry, driver.(*HTTPDriver)
}

func TestHTTPBuiltinsAndImageCarrierRoundTrip(t *testing.T) {
	registry, _ := newHTTPDriverTest(t)
	definitions := registry.CarrierDefinitions()
	if len(definitions) != 5 {
		t.Fatalf("carrier definitions = %#v", definitions)
	}
	carrier, found := registry.Carrier("image")
	if !found {
		t.Fatal("image carrier was not registered")
	}
	encoded, err := carrier.Encode(CarriedMessage{Payload: []byte("encrypted-task")}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encoded.Body, defaultPNGTemplate[:8]) || encoded.Fields["header:content-type"][0] != "image/png" {
		t.Fatalf("encoded image envelope = %#v", encoded)
	}
	decoded, err := carrier.Decode(encoded, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded.Payload) != "encrypted-task" {
		t.Fatalf("decoded payload = %q", decoded.Payload)
	}
	encoded.Body[len(encoded.Body)-1] ^= 0xff
	decoded, err = carrier.Decode(encoded, json.RawMessage(`{}`))
	if err != nil || string(decoded.Payload) == "encrypted-task" {
		t.Fatalf("carrier did not preserve the changed payload: %q, %v", decoded.Payload, err)
	}
}

func TestHTTPFieldCarriersRoundTrip(t *testing.T) {
	registry, _ := newHTTPDriverTest(t)
	for _, carrierID := range []string{"header", "cookie", "query"} {
		t.Run(carrierID, func(t *testing.T) {
			carrier, found := registry.Carrier(carrierID)
			if !found {
				t.Fatalf("carrier %q was not registered", carrierID)
			}
			options := json.RawMessage(`{"name":"X-Data"}`)
			envelope, err := carrier.Encode(CarriedMessage{Payload: []byte("encoded")}, options)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := carrier.Decode(envelope, options)
			if err != nil || string(decoded.Payload) != "encoded" {
				t.Fatalf("round trip = %q, %v", decoded.Payload, err)
			}
		})
	}
}

func TestHTTPDriverCustomRouteHeadersAndImageResponse(t *testing.T) {
	registry, driver := newHTTPDriverTest(t)
	routes := []Route{{
		ID: "custom", Purpose: "callback",
		Match: json.RawMessage(`{"path":"/pixel.png","methods":["POST"]}`),
		Options: json.RawMessage(`{
			"session":{"source":"header","name":"X-Session"},
			"response":{"status":202,"headers":{"X-Route":"yes"}}
		}`),
		Inbound:  CarrierSpec{Type: "header", Options: json.RawMessage(`{"name":"X-Callback"}`)},
		Outbound: CarrierSpec{Type: "image", Options: json.RawMessage(`{}`)},
	}}
	compiled, err := driver.compileRoutes(routes)
	if err != nil {
		t.Fatal(err)
	}
	var received Exchange
	handler := &httpListenerHandler{
		configuration: RuntimeConfig{Name: "custom", UUID: "custom-id", Driver: "http"},
		options: resolvedHTTPOptions{httpListenerOptions: httpListenerOptions{
			ResponseHeaders: map[string]string{"X-Global": "yes"}, MaxBodyBytes: 1024,
		}},
		routes: compiled,
		exchange: func(_ context.Context, exchange Exchange) (ExchangeResult, error) {
			received = exchange
			return ExchangeResult{MessageType: 2, Payload: []byte("task")}, nil
		},
	}
	request := httptest.NewRequest(http.MethodPost, "http://listener/pixel.png", stringsReader("ignored"))
	request.Header.Set("X-Callback", "encoded-callback")
	request.Header.Set("X-Session", "session-7")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || response.Header().Get("X-Global") != "yes" || response.Header().Get("X-Route") != "yes" {
		t.Fatalf("response status/headers = %d %#v", response.Code, response.Header())
	}
	if string(received.Payload) != "encoded-callback" || received.AuthenticatedSession != "session-7" || received.ListenerUUID != "custom-id" {
		t.Fatalf("exchange = %#v", received)
	}
	carrier, _ := registry.Carrier("image")
	decoded, err := carrier.Decode(CarrierEnvelope{Body: response.Body.Bytes()}, json.RawMessage(`{}`))
	if err != nil || string(decoded.Payload) != "task" {
		t.Fatalf("decode response = %q, %v", decoded.Payload, err)
	}
}

func TestHTTPDriverValidationRejectsUnsafeAndAmbiguousConfiguration(t *testing.T) {
	_, driver := newHTTPDriverTest(t)
	tests := []struct {
		name    string
		options json.RawMessage
		routes  []Route
	}{
		{name: "unsafe header", options: json.RawMessage(`{"response_headers":{"Content-Length":"12"}}`)},
		{name: "invalid port", options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"70000"}}`)},
		{name: "partial TLS", options: json.RawMessage(`{"tls":{"enabled":true,"cert_file":"cert.pem"}}`)},
		{name: "unknown option", options: json.RawMessage(`{"mystery":true}`)},
		{name: "invalid carrier header", options: json.RawMessage(`{}`), routes: []Route{{
			ID: "bad-header", Purpose: "callback", Match: json.RawMessage(`{"path":"/"}`),
			Inbound:  CarrierSpec{Type: "header", Options: json.RawMessage(`{"name":"Bad Header"}`)},
			Outbound: CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
		}}},
		{name: "query response carrier", options: json.RawMessage(`{}`), routes: []Route{{
			ID: "bad", Purpose: "callback", Match: json.RawMessage(`{"path":"/"}`),
			Inbound:  CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
			Outbound: CarrierSpec{Type: "query", Options: json.RawMessage(`{"name":"x"}`)},
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := driver.Validate(test.options, test.routes); err == nil {
				t.Fatal("validation unexpectedly succeeded")
			}
		})
	}
}

func TestHTTPDriverRuntimeServesAndStops(t *testing.T) {
	_, driver := newHTTPDriverTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime, err := driver.Start(ctx, RuntimeConfig{
		Name: "runtime", UUID: "runtime-id", Driver: "http",
		Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"},"response_headers":{"X-Listener":"runtime"}}`),
		Routes: []Route{{
			ID: "callback", Purpose: "callback", Match: json.RawMessage(`{"path":"/callback","methods":["POST"]}`),
			Inbound:  CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
			Outbound: CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
		}},
	}, func(_ context.Context, exchange Exchange) (ExchangeResult, error) {
		if string(exchange.Payload) != "request" {
			return ExchangeResult{}, errors.New("unexpected request")
		}
		return ExchangeResult{MessageType: 2, Payload: []byte("response")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post("http://"+runtime.Address()+"/callback", "application/octet-stream", stringsReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Listener") != "runtime" || string(body) != "response" {
		t.Fatalf("response = %d %q %#v", response.StatusCode, body, response.Header)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-runtime.Done(); err != nil {
		t.Fatalf("runtime terminal error = %v", err)
	}
}

func TestHTTPDriverTLSRuntime(t *testing.T) {
	_, driver := newHTTPDriverTest(t)
	certificatePath, keyPath := writeTestCertificate(t)
	options, err := json.Marshal(map[string]any{
		"bind": map[string]string{"host": "127.0.0.1", "port": "0"},
		"tls":  map[string]any{"enabled": true, "cert_file": certificatePath, "key_file": keyPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := driver.Start(context.Background(), RuntimeConfig{
		Name: "tls", UUID: "tls-id", Driver: "http", Options: options,
		Routes: []Route{{
			ID: "callback", Purpose: "callback", Match: json.RawMessage(`{"path":"/","methods":["POST"]}`),
			Inbound:  CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
			Outbound: CarrierSpec{Type: "body", Options: json.RawMessage(`{}`)},
		}},
	}, func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{MessageType: 2}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: testTLSConfig()}}
	response, err := client.Post("https://"+runtime.Address()+"/", "application/octet-stream", stringsReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("TLS response status = %d", response.StatusCode)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-runtime.Done(); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPDefaultCompatibilityAndErrorRedaction(t *testing.T) {
	_, driver := newHTTPDriverTest(t)
	runtime, err := driver.Start(context.Background(), RuntimeConfig{
		Name: "compatibility", UUID: "compatibility-id", Driver: "http",
		Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"}}`),
	}, func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{}, errors.New("sensitive parser detail")
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = runtime.Stop(ctx)
	})

	request, err := http.NewRequest(http.MethodPut, "http://"+runtime.Address()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != "GET, POST" {
		t.Fatalf("unsupported method response = %d %q %#v", response.StatusCode, body, response.Header)
	}

	response, err = http.Post("http://"+runtime.Address()+"/", "application/octet-stream", stringsReader("callback"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || bytes.Contains(body, []byte("sensitive parser detail")) {
		t.Fatalf("callback error response = %d %q", response.StatusCode, body)
	}
}

func TestHTTPRuntimeRejectsCrossOriginAndClosesInteractiveConnections(t *testing.T) {
	_, driver := newHTTPDriverTest(t)
	runtime, err := driver.Start(context.Background(), RuntimeConfig{
		Name: "interactive", UUID: "interactive-id", Driver: "http",
		Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"}}`),
	}, func(context.Context, Exchange) (ExchangeResult, error) {
		return ExchangeResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	streamID, _ := interactive.Default.Open("test-session")
	t.Cleanup(func() { interactive.Default.Close(streamID) })
	endpoint := "ws://" + runtime.Address() + "/cover.png?stream=" + streamID

	crossOriginHeader := http.Header{"Origin": {"https://example.invalid"}}
	connection, response, err := websocket.DefaultDialer.Dial(endpoint, crossOriginHeader)
	if connection != nil {
		_ = connection.Close()
	}
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("cross-origin interactive WebSocket was accepted")
	}

	connection, _, err = websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("interactive WebSocket remained open after listener stop")
	}
	_ = connection.Close()
}

func TestHTTPDefaultEncryptedImplantLifecycle(t *testing.T) {
	previousImplants := serverimplant.ImplantMAP
	serverimplant.ImplantMAP = make(map[string]*serverimplant.Implant)
	t.Cleanup(func() { serverimplant.ImplantMAP = previousImplants })

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if err := encrypt.LoadServerRSAKeyBytes(x509.MarshalPKCS1PrivateKey(privateKey)); err != nil {
		t.Fatal(err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	rsaEncryption := encrypt.Encrypt{}
	if err := rsaEncryption.SetPublicKeyDER(publicKeyDER); err != nil {
		t.Fatal(err)
	}
	sessionEncryption := encrypt.EncryptInit()
	key, iv := sessionEncryption.EncryptGetKeys()
	metadata := &implantwire.ImplantMetadata{
		PID: 1, SessionID: 24680, IP: 0x7f000001, Port: 8080, Sleep: 10,
		Arch: internal.AMD64, Proc: "implant", Hostname: "host", User: "user", Type: "impl",
	}

	manager, err := NewHTTPManager(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(ManagedListenerConfig{
		Name: "default-e2e", UUID: "default-e2e-id", Driver: "http",
		Options: json.RawMessage(`{"bind":{"host":"127.0.0.1","port":"0"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	started, err := manager.Start("default-e2e")
	if err != nil {
		t.Fatal(err)
	}
	implantHTTP := implantcore.HTTPNew(metadata.SessionID)
	implantHTTP.HTTPSetSocket(started.Status.Address)
	implantHTTP.HTTPSetURL(false, "/")

	registration, err := rsaEncryption.RSAEncode(implantcore.PackRegistration(metadata, key, iv))
	if err != nil {
		t.Fatal(err)
	}
	if err := implantHTTP.PostRegistering([]byte(base64.StdEncoding.EncodeToString(registration))); err != nil {
		t.Fatal(err)
	}
	session, err := serverimplant.APIGetSession("24680")
	if err != nil {
		t.Fatal(err)
	}
	if session.Listener != "default-e2e" || session.ListenerUUID != "default-e2e-id" {
		t.Fatalf("registered session = %#v", session)
	}
	if associations := ListenerSnapshotToAPI(started).Associations; associations != 1 {
		t.Fatalf("listener associations = %d", associations)
	}

	task, err := serverimplant.APICreateTask(teamapi.TaskCreateRequest{
		Session: "24680", Code: uint16(internal.PING), Payload: []byte("request"),
	})
	if err != nil {
		t.Fatal(err)
	}
	check := encodeAuthenticatedPacket(&sessionEncryption, implantcore.PackCheck(metadata))
	responseBody, err := implantHTTP.Get([]byte(check))
	if err != nil {
		t.Fatal(err)
	}
	taskBody, err := io.ReadAll(responseBody)
	_ = responseBody.Close()
	if err != nil {
		t.Fatalf("read check response: %v", err)
	}
	framedTask, err := base64.StdEncoding.Strict().DecodeString(string(taskBody))
	if err != nil {
		t.Fatal(err)
	}
	if !sessionEncryption.HMACVerifyHash(framedTask) {
		t.Fatal("task HMAC verification failed")
	}
	plainTask, err := sessionEncryption.AESCbcDecrypt(framedTask[:len(framedTask)-16])
	if err != nil {
		t.Fatal(err)
	}
	taskID, taskCode, payload := implantcore.PackParseTask(bytes.NewReader(plainTask))
	if string(taskID[:]) != task.ID || taskCode != uint16(internal.PING) || string(payload) != "request" {
		t.Fatalf("task = id:%q code:%d payload:%q", taskID, taskCode, payload)
	}

	encodedResponse := encodeAuthenticatedPacket(&sessionEncryption, implantcore.PackResponse(metadata, []byte("response"), taskID))
	if err := implantHTTP.Post([]byte(encodedResponse)); err != nil {
		t.Fatal(err)
	}
	completed, err := serverimplant.APIGetTask("24680", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || string(completed.Response) != "response" {
		t.Fatalf("completed task = %#v", completed)
	}

	responseBody, err = implantHTTP.Get([]byte(encodeAuthenticatedPacket(&sessionEncryption, implantcore.PackCheck(metadata))))
	if err != nil {
		t.Fatal(err)
	}
	noTaskBody, err := io.ReadAll(responseBody)
	_ = responseBody.Close()
	if err != nil || string(noTaskBody) != "Hi!" {
		t.Fatalf("no-task response = %q %v", noTaskBody, err)
	}

	if _, err := manager.Delete("default-e2e"); err != nil {
		t.Fatal(err)
	}
	if err := implantHTTP.PostRegistering([]byte("callback")); err == nil {
		t.Fatal("deleted listener still accepts connections")
	}
}

func encodeAuthenticatedPacket(encryption *encrypt.Encrypt, plaintext []byte) string {
	packet := encryption.AESCbcEncrypt(plaintext)
	encryption.HMACPackAddHmac(&packet)
	return base64.StdEncoding.EncodeToString(packet)
}

func writeTestCertificate(t *testing.T) (string, string) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "listener.crt")
	keyPath := filepath.Join(directory, "listener.key")
	if err := os.WriteFile(certificatePath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certificatePath, keyPath
}

func testTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} // test-only self-signed certificate
}

func stringsReader(value string) io.Reader { return bytes.NewBufferString(value) }
