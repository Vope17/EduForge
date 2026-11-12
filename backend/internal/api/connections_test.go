package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eduforge/backend/internal/llm"
)

type registrationStub struct{}

func (registrationStub) ModelRegistration(context.Context, string) (string, string, string, error) {
	return "urn:uuid:fixture", "", "", nil
}
func (registrationStub) SaveModelRegistration(context.Context, string, string, string) error {
	return nil
}

func TestConnectionSettingsNeverReturnAPIKeyAndRejectCrossOrigin(t *testing.T) {
	manager, err := llm.NewConnections("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	oauth, err := llm.NewOAuth(registrationStub{}, manager, "http://127.0.0.1:8081")
	if err != nil {
		t.Fatal(err)
	}
	connections := NewModelConnections(manager, oauth, nil, 30)
	handler := NewHandler(nil, connections)
	body := []byte(`{"endpoint":"https://provider.example/v1/chat/completions","model":"fixture","api_key":"private-test-key","version":0}`)

	call := func(method, token, origin string, body []byte) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, "http://127.0.0.1:8081/api/v1/model-connection", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-EduForge-CSRF", token)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	if response := call(http.MethodPut, "", "http://127.0.0.1:8081", body); response.Code != http.StatusForbidden {
		t.Fatal("accepted settings without CSRF verification")
	}
	if response := call(http.MethodPut, connections.csrf, "https://attacker.example", body); response.Code != http.StatusForbidden {
		t.Fatal("accepted cross-origin settings")
	}
	response := call(http.MethodPut, connections.csrf, "http://127.0.0.1:8081", body)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-test-key") ||
		strings.Contains(response.Body.String(), "api_key\":") {
		t.Fatalf("unsafe or unsuccessful settings response: status %d", response.Code)
	}
	if response := call(http.MethodPut, connections.csrf, "http://127.0.0.1:8081", body); response.Code != http.StatusConflict {
		t.Fatal("accepted a stale configuration version")
	}
	response = call(http.MethodGet, "", "", nil)
	var result connectionDTO
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Connected || result.Model != "fixture" || result.Verified || strings.Contains(response.Body.String(), "private-test-key") {
		t.Fatal("connection metadata was incorrect or exposed credentials")
	}
	response = call(http.MethodDelete, connections.csrf, "http://127.0.0.1:8081", []byte(`{"version":1}`))
	if response.Code != http.StatusOK || manager.Ready() {
		t.Fatal("connection was not cleared")
	}
}
