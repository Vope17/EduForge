package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	NewHandler(nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	var body healthResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if body.Status != "ok" || body.Service != "eduforge" {
		t.Fatalf("unexpected health: %+v", body)
	}
}

func TestErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		path   string
		status int
		code   string
	}{
		{"unknown API", http.MethodGet, "/api/v1/missing", http.StatusNotFound, "not_found"},
		{"health suffix", http.MethodGet, "/api/v1/health/extra", http.StatusNotFound, "not_found"},
		{"unsupported method", http.MethodPost, "/api/v1/health", http.StatusMethodNotAllowed, "method_not_allowed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewHandler(nil, nil).ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			var body errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Error.Code != test.code || body.Error.Message == "" {
				t.Fatalf("unexpected error: %+v", body.Error)
			}
			if test.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("missing Allow header")
			}
		})
	}
}

func TestHeadHasNoBody(t *testing.T) {
	for _, path := range []string{"/api/v1/health", "/api/v1/missing"} {
		response := httptest.NewRecorder()
		NewHandler(nil, nil).ServeHTTP(response, httptest.NewRequest(http.MethodHead, path, nil))
		if response.Body.Len() != 0 {
			t.Fatalf("HEAD %s has a body", path)
		}
	}
}
