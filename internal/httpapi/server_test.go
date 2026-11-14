package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/internal/app"
	"eduforge/internal/store"
)

func handler(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a, err := app.New(s, filepath.Join(root, "secrets"))
	if err != nil {
		t.Fatal(err)
	}
	return New(a, filepath.Join(root, "web"), []string{"http://localhost:8080"})
}
func TestHostOriginAndWriteToken(t *testing.T) {
	s := handler(t)
	cases := []struct {
		host, origin, token string
		want                int
	}{{"attacker.invalid", "", "", 403}, {"localhost:8080", "https://attacker.invalid", s.Token, 403}, {"localhost:8080", "http://localhost:8080", "", 403}, {"localhost:8080", "http://localhost:8080", s.Token, 200}}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/api/projects", strings.NewReader(`{"name":"Research","description":"","style":{}}`))
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-EduForge-Token", tc.token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("got %d want %d: %s", rec.Code, tc.want, rec.Body.String())
		}
	}
}
func TestClientCannotAssignAnOwner(t *testing.T) {
	s := handler(t)
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/projects", strings.NewReader(`{"name":"Research","owner_user_id":"someone-else"}`))
	req.Header.Set("X-EduForge-Token", s.Token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("client ownership accepted: %d", rec.Code)
	}
}
