package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	implantwire "purpcmd/implant"
	"purpcmd/internal"
	"purpcmd/internal/encrypt"
	"purpcmd/internal/protocol"
)

func TestBindServerUsesListenerProtocolFrames(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPublicKeyDER(publicDER); err != nil {
		t.Fatal(err)
	}

	bind, err := NewBindServer(BindConfig{Path: "/bind"})
	if err != nil {
		t.Fatal(err)
	}
	var executions atomic.Int32
	underlying := bind.execute
	bind.execute = func(ctx *CommandContext, code uint16, payload []byte, taskID [8]byte) (string, bool, error) {
		executions.Add(1)
		return underlying(ctx, code, payload, taskID)
	}
	httpServer := httptest.NewServer(bind.httpServer.Handler)
	defer httpServer.Close()

	registration := bindExchange(t, httpServer.URL+"/bind", protocol.ExchangeRegistration, nil)
	rsaEncryption := encrypt.Encrypt{RSAPrivate: privateKey}
	registrationDecoded, err := base64.StdEncoding.Strict().DecodeString(string(registration))
	if err != nil {
		t.Fatal(err)
	}
	registrationPlaintext, err := rsaEncryption.RSADecode(registrationDecoded)
	if err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader(registrationPlaintext)
	var messageType uint16
	if err := binary.Read(reader, binary.BigEndian, &messageType); err != nil {
		t.Fatal(err)
	}
	if messageType != internal.REG {
		t.Fatalf("registration message type = %d", messageType)
	}
	metadata := new(implantwire.ImplantMetadata)
	if err := protocol.ReadMetadata(reader, metadata); err != nil {
		t.Fatal(err)
	}
	var key, iv [16]byte
	if err := binary.Read(reader, binary.BigEndian, &key); err != nil {
		t.Fatal(err)
	}
	if err := binary.Read(reader, binary.BigEndian, &iv); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(registrationPlaintext, PackRegistration(bind.metadata, key, iv)) {
		t.Fatal("bind registration differs from reverse-listener registration packet")
	}
	sessionEncryption := encrypt.EncryptImport(key, iv)

	health := bindExchange(t, httpServer.URL+"/bind", protocol.ExchangeHealthcheck, nil)
	healthPlaintext := decodeBindPacket(t, &sessionEncryption, health)
	if !bytes.Equal(healthPlaintext, PackCheck(bind.metadata)) {
		t.Fatal("bind healthcheck differs from reverse-listener healthcheck packet")
	}

	taskID := [8]byte{'t', 'a', 's', 'k', '0', '0', '0', '1'}
	taskPacket := protocol.EncodeTask(internal.PING, taskID, []byte("hello"))
	taskBody := encodeBindPacket(&sessionEncryption, taskPacket)
	response := bindExchange(t, httpServer.URL+"/bind", protocol.ExchangeTask, taskBody)
	responsePlaintext := decodeBindPacket(t, &sessionEncryption, response)
	expected := PackResponse(bind.metadata, []byte("hello pong"), taskID)
	if !bytes.Equal(responsePlaintext, expected) {
		t.Fatal("bind response differs from reverse-listener response packet")
	}

	retried := bindExchange(t, httpServer.URL+"/bind", protocol.ExchangeTask, taskBody)
	if !bytes.Equal(response, retried) {
		t.Fatal("retried task did not return the cached response")
	}
	if executions.Load() != 1 {
		t.Fatalf("retried task executed %d times", executions.Load())
	}

	conflictingPacket := protocol.EncodeTask(internal.PING, taskID, []byte("changed"))
	conflictingBody := encodeBindPacket(&sessionEncryption, conflictingPacket)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, httpServer.URL+"/bind", bytes.NewReader(conflictingBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(protocol.ExchangeHeader, protocol.ExchangeTask)
	conflict, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer conflict.Body.Close()
	if conflict.StatusCode != http.StatusConflict {
		t.Fatalf("conflicting task status = %d", conflict.StatusCode)
	}
}

func TestBindServerRejectsUnauthenticatedAndOversizedRequests(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPublicKeyDER(publicDER); err != nil {
		t.Fatal(err)
	}
	bind, err := NewBindServer(BindConfig{Path: "/bind", MaxRequestBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(bind.httpServer.Handler)
	defer httpServer.Close()

	tests := []struct {
		name     string
		exchange string
		body     []byte
		status   int
	}{
		{name: "unknown operation", exchange: "other", status: http.StatusBadRequest},
		{name: "registration body", exchange: protocol.ExchangeRegistration, body: []byte("x"), status: http.StatusBadRequest},
		{name: "bad task authentication", exchange: protocol.ExchangeTask, body: []byte("YWJjZA=="), status: http.StatusBadRequest},
		{name: "oversized", exchange: protocol.ExchangeTask, body: []byte("123456789"), status: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, httpServer.URL+"/bind", bytes.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set(protocol.ExchangeHeader, test.exchange)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			_, _ = io.Copy(io.Discard, response.Body)
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
			}
		})
	}
}

func bindExchange(t *testing.T, endpoint, exchange string, body []byte) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(protocol.ExchangeHeader, exchange)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("exchange %s returned %d: %s", exchange, response.StatusCode, result)
	}
	return result
}

func encodeBindPacket(encryption *encrypt.Encrypt, plaintext []byte) []byte {
	ciphertext := encryption.AESCbcEncrypt(plaintext)
	encryption.HMACPackAddHmac(&ciphertext)
	return []byte(base64.StdEncoding.EncodeToString(ciphertext))
}

func decodeBindPacket(t *testing.T, encryption *encrypt.Encrypt, encoded []byte) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.Strict().DecodeString(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if !encryption.HMACVerifyHash(decoded) {
		t.Fatal("packet HMAC was invalid")
	}
	plaintext, err := encryption.AESCbcDecrypt(decoded[:len(decoded)-16])
	if err != nil {
		t.Fatal(err)
	}
	return plaintext
}
